#!/usr/bin/env python3
"""Minimal OpenAI-compatible /v1/chat/completions mock for the deploy smoke test.

Returns a fixed classification so the documented ingest -> enrich path can be
exercised in CI with no real LLM key. Stdlib only; listens on :18080.
Used by scripts/smoke-deploy.sh (and handy for local cold-path verification)."""
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

# The string attune's enricher_parse expects as choices[0].message.content.
CLASSIFICATION = json.dumps({
    "title": "Export button broken on Safari",
    "kind": "bug",           # must be one of attune's ValidKinds
    "modules": ["export"],
    "severity": "P2",        # must be one of P0..P3
    "rationale": "Browser-specific UI failure",
}, ensure_ascii=False)


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", 0)))
        body = json.dumps({
            "id": "mock-1",
            "object": "chat.completion",
            "model": "gpt-4o-mini",
            "choices": [{
                "index": 0,
                "message": {"role": "assistant", "content": CLASSIFICATION},
                "finish_reason": "stop",
            }],
        }).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_):  # quiet
        pass


if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 18080), Handler).serve_forever()
