"""Tests for attune client."""

from __future__ import annotations

import http.server
import json
import threading
from typing import Any

import pytest

from attune.client import AttuneClient, _strip_empty
from attune.errors import AttuneAPIError


class _MockHandler(http.server.BaseHTTPRequestHandler):
    responses: dict[str, tuple[int, Any]] = {}

    def do_POST(self) -> None:
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        _MockHandler.last_body = json.loads(body) if body else {}
        _MockHandler.last_headers = dict(self.headers)
        status, data = self.responses.get(self.path, (404, {"code": "NOT_FOUND", "message": "nope"}))
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(data).encode())

    def do_GET(self) -> None:
        path = self.path.split("?")[0]
        status, data = self.responses.get(path, (404, {"code": "NOT_FOUND", "message": "nope"}))
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(data).encode())

    def log_message(self, format: str, *args: Any) -> None:
        pass


@pytest.fixture()
def mock_server():
    server = http.server.HTTPServer(("127.0.0.1", 0), _MockHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    port = server.server_address[1]
    yield f"http://127.0.0.1:{port}"
    server.shutdown()


def test_ingest(mock_server: str) -> None:
    _MockHandler.responses = {"/v1/feedback/ingest": (200, {"id": "42"})}
    client = AttuneClient(base_url=mock_server, api_key="test-key")
    resp = client.ingest("login is broken", source="widget")
    assert resp.id == "42"
    assert _MockHandler.last_body["content"] == "login is broken"
    assert _MockHandler.last_body["source"] == "widget"
    assert _MockHandler.last_headers["Authorization"] == "Bearer test-key"


def test_ingest_idempotency_key(mock_server: str) -> None:
    _MockHandler.responses = {"/v1/feedback/ingest": (200, {"id": "42"})}
    client = AttuneClient(base_url=mock_server, api_key="k")
    client.ingest("test", idempotency_key="idem-123")
    assert _MockHandler.last_headers["Idempotency-Key"] == "idem-123"


def test_list_feedback(mock_server: str) -> None:
    _MockHandler.responses = {
        "/fb/v1/console/feedback": (
            200,
            {
                "items": [{"id": "1", "content": "hello", "isUrgent": True}],
                "nextCursor": "abc",
            },
        ),
    }
    client = AttuneClient(base_url=mock_server, api_key="k")
    items, cursor = client.list_feedback()
    assert len(items) == 1
    assert items[0].is_urgent is True
    assert cursor == "abc"


def test_get_feedback(mock_server: str) -> None:
    _MockHandler.responses = {
        "/fb/v1/console/feedback/7": (
            200,
            {"id": "7", "content": "detail", "replyDraft": "hi"},
        ),
    }
    client = AttuneClient(base_url=mock_server, api_key="k")
    detail = client.get_feedback("7")
    assert detail.id == "7"
    assert detail.reply_draft == "hi"


def test_api_error(mock_server: str) -> None:
    _MockHandler.responses = {
        "/v1/feedback/ingest": (429, {"code": "RATE_LIMITED", "message": "slow down"}),
    }
    client = AttuneClient(base_url=mock_server, api_key="k")
    with pytest.raises(AttuneAPIError) as exc_info:
        client.ingest("x")
    assert exc_info.value.status == 429
    assert exc_info.value.code == "RATE_LIMITED"


def test_strip_empty() -> None:
    assert _strip_empty({"a": 1, "b": "", "c": 0, "d": None}) == {"a": 1}
