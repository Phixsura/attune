# attune — developer task runner.
#
# Proto codegen (issue #19): edit proto/**, then run `make proto` to regenerate
# the Go (internal/proto/), TS (console/src/proto/) and OpenAPI (docs/openapi/)
# artifacts. Generated files are committed; CI's proto-sync job fails on drift.
#
# Requires `buf` (https://buf.build/docs/installation). Codegen first uses buf
# remote plugins; when the Buf Schema Registry is unavailable, `make proto`
# falls back to fixed-version local plugins. To change a proto dependency, run
# `make proto-deps`.

.PHONY: help proto proto-lint proto-breaking proto-deps observability-dashboards observability-rules observability-load-e2e search-quality maturity-contract demo-seed demo-reset demo-bootstrap test fast-check adversarial-check test-live test-live-list test-integration runtime-smoke release-smoke ci-check

PNPM ?= corepack pnpm
FUZZTIME ?= 10s

help: ## List targets.
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-16s %s\n", $$1, $$2}'

proto: ## Regenerate Go + TS + OpenAPI from proto/, then lint.
	bash scripts/proto-generate.sh

# Second Go target for the published SDK (#36): a different go_package_prefix
# than internal/proto, so it needs its own template. Scoped to the public SDK
# surface (ingest + selected management APIs) to keep the SDK module focused.
proto-sdk-go: ## Regenerate the Go SDK's proto types (sdk/go/internal).
	bash scripts/proto-generate.sh sdk-go

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

search-quality: ## Verify the committed semantic-search relevance baseline.
	go run ./internal/tools/searchquality

maturity-contract: ## Verify the platform maturity proposal graph and verification links.
	bash scripts/lint-maturity-contract.sh

demo-seed: ## Seed or refresh the local demo workspace.
	go run ./cmd/attune demo seed

demo-reset: ## Clear demo-seeded rows for the local demo workspace.
	go run ./cmd/attune demo reset

demo-bootstrap: ## Rebuild the demo workspace from a clean baseline.
	go run ./cmd/attune demo bootstrap

# ── Test tiers (docs/testing.md) ─────────────────────────────────────────
#
# `test` is the default unit tier — runs on every contributor's machine
# and in CI with no external dependencies. `test-live` is opt-in and
# hits real external endpoints; the `live` build tag ensures `go test ./...`
# cannot accidentally enter those files.

test: ## Unit tier — no external services, no API keys needed.
	go test -short ./...

fast-check: ## Fast local unit/type/browser-contract sweep.
	go test -short ./...
	cd console && $(PNPM) --ignore-workspace tsc -b --noEmit
	cd console && $(PNPM) --ignore-workspace vitest run

adversarial-check: ## Bug-hunting tier — focused adversarial tests plus short Go fuzz runs.
	go test -count=1 ./internal/repo/feedback ./internal/handlers/console/feedback
	go test ./internal/repo/feedback -run '^$$' -fuzz=FuzzNormalizeQualityValue -fuzztime=$(FUZZTIME)
	go test ./internal/repo/feedback -run '^$$' -fuzz=FuzzQualityAccumulatorMalformedPayloads -fuzztime=$(FUZZTIME)

test-live: ## Live tier — runs test/live/... against real external endpoints. See docs/testing.md.
	go test -tags=live -count=1 -timeout=10m -run '^TestLive_' ./test/live/...

test-live-list: ## Show which live backends would run given current env.
	@echo "LLM:"
	@for v in E2E_OPENAI_COMPAT_KEY E2E_OPENAI_RESPONSES_KEY E2E_ANTHROPIC_KEY E2E_GEMINI_KEY; do \
		if [ -n "$${!v}" ]; then echo "  ✓ $$v set"; else echo "  ✗ $$v unset (test would skip)"; fi; \
	done
	@echo "Outbound:"
	@for v in E2E_OUTBOUND_RAW_WEBHOOK_URL E2E_OUTBOUND_SLACK_WEBHOOK_URL E2E_OUTBOUND_DISCORD_WEBHOOK_URL E2E_OUTBOUND_LARK_WEBHOOK_URL E2E_OUTBOUND_GITHUB_REPO_URL E2E_OUTBOUND_GITHUB_TOKEN; do \
		if [ -n "$${!v}" ]; then echo "  ✓ $$v set"; else echo "  ✗ $$v unset (test would skip)"; fi; \
	done
	@if [ -n "$$E2E_OUTBOUND_GITHUB_CREATE_ISSUE" ]; then echo "  ✓ E2E_OUTBOUND_GITHUB_CREATE_ISSUE set"; else echo "  ✗ E2E_OUTBOUND_GITHUB_CREATE_ISSUE unset (GitHub issue test would skip)"; fi

# IO integration tier — real Postgres smoke suites under test/integration.
# Requires Docker locally; CI runs this against a Postgres service container.
.PHONY: test-integration

test-integration: ## IO tier — real Postgres. Needs Docker or ATTUNE_TEST_DATABASE_URL + PostgreSQL 17 client tools (pg_dump/psql/pg_basebackup/pg_verifybackup) for the restore-drill tests.
	go test -tags=integration -count=1 -p 1 -timeout=10m ./test/integration/postgres/...

runtime-smoke: ## Build the production image and boot it against throwaway pgvector Postgres.
	docker build -t attune:runtime-smoke .
	ATTUNE_RUNTIME_SMOKE_IMAGE=attune:runtime-smoke bash scripts/runtime-smoke.sh

release-smoke: ## Heavy pre-release sweep: CI checks, contracts, integration, deploy, observability, image runtime.
	$(MAKE) ci-check
	$(MAKE) test-integration
	$(MAKE) proto-lint
	$(MAKE) proto-breaking
	$(MAKE) observability-dashboards
	$(MAKE) observability-rules
	docker compose -f deploy/docker-compose.yml config -q
	docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.obs.yml config -q
	docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.oidc-test.yml config -q
	git diff --check
	$(MAKE) runtime-smoke

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
	@echo "▸ go.mod / go.sum tidy"
	@go mod tidy
	@git diff --exit-code go.mod go.sum
	@echo "✓ go mod tidy"
	@echo
	@echo "▸ search quality baseline"
	@go run ./internal/tools/searchquality
	@echo "✓ search quality"
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
	@echo "▸ scripts/lint-maturity-contract.sh"
	@bash scripts/lint-maturity-contract.sh
	@echo "✓ lint-maturity-contract"
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
	@cd console && $(PNPM) --ignore-workspace tsc -b --noEmit
	@echo "✓ console tsc"
	@echo
	@echo "▸ console: biome check"
	@cd console && $(PNPM) --ignore-workspace biome check
	@echo "✓ console biome"
	@echo
	@echo "▸ console: vite build"
	@cd console && $(PNPM) --ignore-workspace exec vite build
	@echo "✓ console vite build"
	@echo
	@echo "▸ console: vitest"
	@cd console && $(PNPM) --ignore-workspace vitest run
	@echo "✓ console vitest"
	@echo
	@echo "▸ console: dependency-cruiser"
	@cd console && $(PNPM) --ignore-workspace arch
	@echo "✓ console arch"
	@echo
	@echo "▸ trufflehog (if installed)"
	@command -v trufflehog >/dev/null 2>&1 && trufflehog git file://. --only-verified --fail || echo "⚠ trufflehog not installed, skipping"
	@echo
	@echo "══════════════════════════════════════════════════════════════"
	@echo "  ✓ ci-check passed — ready to push"
	@echo "══════════════════════════════════════════════════════════════"
