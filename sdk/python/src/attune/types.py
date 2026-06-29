"""Wire types for the attune API."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass
class IngestRequest:
    """Request body for POST /v1/feedback/ingest."""

    content: str
    source: str = "api"
    type: str = ""
    user_id: str = ""
    page_url: str = ""
    source_meta: dict[str, Any] = field(default_factory=dict)


@dataclass
class IngestResponse:
    """Response from POST /v1/feedback/ingest."""

    id: str = ""


@dataclass
class Feedback:
    """Summary feedback item from list responses."""

    id: str = ""
    content: str = ""
    source: str = ""
    type: str = ""
    user_id: str = ""
    page_url: str = ""
    enriched_title: str | None = None
    enriched_attrs: dict[str, Any] = field(default_factory=dict)
    is_urgent: bool = False
    enrichment_status: str = ""
    created_at: str = ""
    language: str | None = None
    tags: list[dict[str, Any]] = field(default_factory=list)


@dataclass
class FeedbackDetail(Feedback):
    """Detailed feedback item with extra fields."""

    source_meta: dict[str, Any] = field(default_factory=dict)
    enrichment_error: str | None = None
    reply_draft: str | None = None
    reply_draft_generated_at: str | None = None
