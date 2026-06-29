"""Tests for attune types."""

from attune.types import IngestRequest, IngestResponse, Feedback, FeedbackDetail


def test_ingest_request_defaults() -> None:
    req = IngestRequest(content="login is broken")
    assert req.content == "login is broken"
    assert req.source == "api"
    assert req.type == ""
    assert req.user_id == ""
    assert req.source_meta == {}


def test_ingest_response() -> None:
    resp = IngestResponse(id="42")
    assert resp.id == "42"


def test_feedback_defaults() -> None:
    fb = Feedback(id="1", content="test")
    assert fb.is_urgent is False
    assert fb.enriched_title is None
    assert fb.tags == []


def test_feedback_detail_inherits() -> None:
    fd = FeedbackDetail(id="1", content="test", reply_draft="hello")
    assert fd.content == "test"
    assert fd.reply_draft == "hello"
    assert fd.source_meta == {}
