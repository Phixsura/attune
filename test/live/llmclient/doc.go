// Package llmclient_live holds the live-call test suite for the four
// LLM client backends (OpenAI Chat Completions, OpenAI Responses,
// Anthropic Messages, Gemini). The tests themselves live in
// live_test.go behind the `live` build tag; this file exists so that
// `go vet ./...` always sees a valid package here regardless of build
// tags. See docs/testing.md for the operational surface.
package llmclient_live
