# attune — developer task runner.
#
# Proto codegen (issue #19): edit proto/**, then run `make proto` to regenerate
# the Go (internal/proto/), TS (console/src/proto/) and OpenAPI (docs/openapi/)
# artifacts. Generated files are committed; CI's proto-sync job fails on drift.
#
# Requires `buf` (https://buf.build/docs/installation). Codegen uses buf *remote*
# plugins, so no local protoc-gen-* installs are needed — only network access to
# the Buf Schema Registry. To change a proto dependency, run `make proto-deps`.

.PHONY: help proto proto-lint proto-breaking proto-deps observability-dashboards observability-rules observability-load-e2e test test-live test-live-list ci-check

help: ## List targets.
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-16s %s\n", $$1, $$2}'

proto: ## Regenerate Go + TS + OpenAPI from proto/, then lint.
	buf generate
	go run ./internal/tools/openapipatch
	$(MAKE) proto-sdk-go
	buf lint

# Second Go target for the published SDK (#36): a different go_package_prefix
# than internal/proto, so it needs its own template. Scoped to the public SDK
# surface (ingest + selected management APIs) to keep the SDK module focused.
proto-sdk-go: ## Regenerate the Go SDK's proto types (sdk/go/internal).
	buf generate --template buf.gen.sdk-go.yaml \
		--path proto/attune/v1/ingest.proto \
		--path proto/attune/v1/audit.proto \
		--path proto/attune/v1/gdpr.proto \
		--path proto/attune/v1/outbox.proto \
		--path proto/attune/v1/mcp_client.proto \
		--path proto/attune/v1/tag.proto \
		--path proto/attune/v1/workflow.proto \
		--path proto/attune/v1/common.proto

proto-lint: ## Lint the proto definitions only.
	buf lint

proto-breaking: ## Check proto compatibility against main.
	buf breaking --against '.git#branch=main'

proto-deps: ## Refresh buf.lock (after changing deps in buf.yaml).
	buf dep update

observability-dashboards: ## Regenerate Grafana dashboards and Helm copies.
	go run ./internal/tools/observabilitydash

observability-rules: ## Validate Prometheus scrape config, recording rules, and alert rules.
	docker run --rm \
		--entrypoint promtool \
		-v "$(CURDIR)/deploy/prometheus.yml:/etc/prometheus/prometheus.yml:ro" \
		-v "$(CURDIR)/observability/rules:/etc/prometheus/rules:ro" \
		prom/prometheus:v3.12.0 \
		check config /etc/prometheus/prometheus.yml
	docker run --rm \
		--entrypoint promtool \
		-v "$(CURDIR)/observability/rules:/etc/prometheus/rules:ro" \
		-v "$(CURDIR)/observability/rule-tests:/etc/prometheus/rule-tests:ro" \
		prom/prometheus:v3.12.0 \
		test rules /etc/prometheus/rule-tests/attune-rules.test.yml

observability-load-e2e: ## Send load and verify metrics via Prometheus/Grafana. Requires API_KEY.
	bash scripts/observability-load-e2e.sh

# ── Test tiers (docs/testing.md) ─────────────────────────────────────────
#
# `test` is the default unit tier — runs on every contributor's machine
# and in CI with no external dependencies. `test-live` is opt-in and
# hits real LLM endpoints; the `live` build tag ensures `go test ./...`
# cannot accidentally enter those files.

test: ## Unit tier — no external services, no API keys needed.
	go test -short ./...

test-live: ## Live tier — runs test/live/... against real LLM endpoints. See docs/testing.md.
	go test -tags=live -count=1 -timeout=10m -run '^TestLive_' ./test/live/...

test-live-list: ## Show which live backends would run given current env.
	@for v in E2E_OPENAI_COMPAT_KEY E2E_OPENAI_RESPONSES_KEY E2E_ANTHROPIC_KEY E2E_GEMINI_KEY; do \
		if [ -n "$${!v}" ]; then echo "  ✓ $$v set"; else echo "  ✗ $$v unset (test would skip)"; fi; \
	done

# IO integration tier — real Postgres smoke suites under test/integration.
# Requires Docker locally; CI runs this against a Postgres service container.
.PHONY: test-integration

test-integration: ## IO tier — real Postgres. Needs Docker or ATTUNE_TEST_DATABASE_URL + PostgreSQL 17 client tools (pg_dump/psql/pg_basebackup/pg_verifybackup) for the restore-drill tests.
	go test -tags=integration -count=1 -p 1 -timeout=10m ./test/integration/postgres/...

# ── CI pre-flight (docs/ci-troubleshooting.md) ───────────────────────────
#
# `ci-check` mirrors the full CI gate locally. Run before pushing to catch
# issues early. Requires: go, golangci-lint, lizard, pnpm, trufflehog (optional).

ci-check: ## Run all CI checks locally before push.
	@echo "══════════════════════════════════════════════════════════════"
	@echo "  ci-check — local CI pre-flight"
	@echo "══════════════════════════════════════════════════════════════"
	@echo
	@echo "▸ go vet"
	@go vet ./...
	@echo "✓ go vet"
	@echo
	@echo "▸ go build"
	@go build ./...
	@echo "✓ go build"
	@echo
	@echo "▸ go test (unit)"
	@go test -race -short ./...
	@echo "✓ go test"
	@echo
	@echo "▸ golangci-lint"
	@golangci-lint run
	@echo "✓ golangci-lint"
	@echo
	@echo "▸ lizard (CCN ≤15, NLOC ≤100)"
	@lizard . -l go -C 15 -T nloc=100 --warnings_only
	@echo "✓ lizard"
	@echo
	@echo "▸ scripts/lint-slog.sh"
	@bash scripts/lint-slog.sh --strict
	@echo "✓ lint-slog"
	@echo
	@echo "▸ scripts/lint-artifacts.sh"
	@bash scripts/lint-artifacts.sh --strict
	@echo "✓ lint-artifacts"
	@echo
	@echo "▸ scripts/lint-rawptr.sh"
	@bash scripts/lint-rawptr.sh
	@echo "✓ lint-rawptr"
	@echo
	@echo "▸ scripts/lint-errorcode.sh"
	@bash scripts/lint-errorcode.sh
	@echo "✓ lint-errorcode"
	@echo
	@echo "▸ scripts/lint-integration-layout.sh"
	@bash scripts/lint-integration-layout.sh
	@echo "✓ lint-integration-layout"
	@echo
	@echo "▸ scripts/lint-outbound-conformance.sh"
	@bash scripts/lint-outbound-conformance.sh
	@echo "✓ lint-outbound-conformance"
	@echo
	@echo "▸ jscpd (duplication < 5%, test files excluded)"
	@npx -y jscpd . --silent
	@echo "✓ jscpd"
	@echo
	@echo "▸ console: pnpm tsc"
	@cd console && pnpm tsc -b --noEmit
	@echo "✓ console tsc"
	@echo
	@echo "▸ console: biome check"
	@cd console && pnpm biome check
	@echo "✓ console biome"
	@echo
	@echo "▸ console: vitest"
	@cd console && pnpm vitest run
	@echo "✓ console vitest"
	@echo
	@echo "▸ trufflehog (if installed)"
	@command -v trufflehog >/dev/null 2>&1 && trufflehog git file://. --only-verified --fail || echo "⚠ trufflehog not installed, skipping"
	@echo
	@echo "══════════════════════════════════════════════════════════════"
	@echo "  ✓ ci-check passed — ready to push"
	@echo "══════════════════════════════════════════════════════════════"
