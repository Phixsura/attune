# attune — developer task runner.
#
# Proto codegen (issue #19): edit proto/**, then run `make proto` to regenerate
# the Go (internal/proto/), TS (console/src/proto/) and OpenAPI (docs/openapi/)
# artifacts. Generated files are committed; CI's proto-sync job fails on drift.
#
# Requires `buf` (https://buf.build/docs/installation). Codegen uses buf *remote*
# plugins, so no local protoc-gen-* installs are needed — only network access to
# the Buf Schema Registry. To change a proto dependency, run `make proto-deps`.

.PHONY: help proto proto-lint proto-breaking proto-deps test test-live test-live-list

help: ## List targets.
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-16s %s\n", $$1, $$2}'

proto: ## Regenerate Go + TS + OpenAPI from proto/, then lint.
	buf generate
	buf lint

proto-lint: ## Lint the proto definitions only.
	buf lint

proto-breaking: ## Check proto compatibility against main.
	buf breaking --against '.git#branch=main'

proto-deps: ## Refresh buf.lock (after changing deps in buf.yaml).
	buf dep update

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

# IO integration tier — spins up real Postgres via testcontainers-go.
# Requires a running Docker daemon; CI runs this in a separate job.
.PHONY: test-integration

test-integration: ## IO tier — real Postgres in a testcontainer. Needs Docker running.
	go test -tags=integration -count=1 -timeout=10m ./...
