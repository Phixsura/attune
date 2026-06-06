# attune — developer task runner.
#
# Proto codegen (issue #19): edit proto/**, then run `make proto` to regenerate
# the Go (internal/proto/), TS (console/src/proto/) and OpenAPI (docs/openapi/)
# artifacts. Generated files are committed; CI's proto-sync job fails on drift.
#
# Requires `buf` (https://buf.build/docs/installation). Codegen uses buf *remote*
# plugins, so no local protoc-gen-* installs are needed — only network access to
# the Buf Schema Registry. To change a proto dependency, run `make proto-deps`.

.PHONY: proto proto-lint proto-deps help

help: ## List targets.
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

proto: ## Regenerate Go + TS + OpenAPI from proto/, then lint.
	buf generate
	buf lint

proto-lint: ## Lint the proto definitions only.
	buf lint

proto-deps: ## Refresh buf.lock (after changing deps in buf.yaml).
	buf dep update
