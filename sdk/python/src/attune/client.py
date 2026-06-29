"""attune API client."""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from dataclasses import asdict
from typing import Any

from attune.errors import AttuneAPIError, AttuneError, AttuneTimeoutError
from attune.types import Feedback, FeedbackDetail, IngestRequest, IngestResponse

_DEFAULT_TIMEOUT = 30


class AttuneClient:
    """Synchronous client for the attune API.

    Uses only stdlib (urllib) — no third-party dependencies.
    """

    def __init__(
        self,
        *,
        base_url: str,
        api_key: str,
        timeout: int = _DEFAULT_TIMEOUT,
        user_agent: str = "attune-python/0.1.0",
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._api_key = api_key
        self._timeout = timeout
        self._user_agent = user_agent

    def ingest(
        self,
        content: str,
        *,
        source: str = "api",
        type: str = "",
        user_id: str = "",
        page_url: str = "",
        source_meta: dict[str, Any] | None = None,
        idempotency_key: str | None = None,
    ) -> IngestResponse:
        """Submit feedback via POST /v1/feedback/ingest."""
        req = IngestRequest(
            content=content,
            source=source,
            type=type,
            user_id=user_id,
            page_url=page_url,
            source_meta=source_meta or {},
        )
        body = _strip_empty(asdict(req))
        headers: dict[str, str] = {}
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key
        data = self._post("/v1/feedback/ingest", body, extra_headers=headers)
        return IngestResponse(id=str(data.get("id", "")))

    def list_feedback(
        self,
        *,
        limit: int = 50,
        cursor: str = "",
        q: str = "",
    ) -> tuple[list[Feedback], str]:
        """List feedback via GET /fb/v1/console/feedback."""
        params: dict[str, str] = {"limit": str(limit)}
        if cursor:
            params["cursor"] = cursor
        if q:
            params["q"] = q
        data = self._get("/fb/v1/console/feedback", params)
        items = [_dict_to_feedback(f) for f in data.get("items", [])]
        next_cursor = data.get("nextCursor", "")
        return items, next_cursor

    def get_feedback(self, feedback_id: str) -> FeedbackDetail:
        """Get feedback detail via GET /fb/v1/console/feedback/{id}."""
        data = self._get(f"/fb/v1/console/feedback/{feedback_id}")
        return _dict_to_feedback_detail(data)

    def _request(
        self,
        method: str,
        path: str,
        body: dict[str, Any] | None = None,
        params: dict[str, str] | None = None,
        extra_headers: dict[str, str] | None = None,
    ) -> dict[str, Any]:
        url = self._base_url + path
        if params:
            qs = "&".join(f"{k}={v}" for k, v in params.items() if v)
            if qs:
                url = f"{url}?{qs}"

        data_bytes = json.dumps(body).encode() if body else None
        req = urllib.request.Request(url, data=data_bytes, method=method)
        req.add_header("Authorization", f"Bearer {self._api_key}")
        req.add_header("Content-Type", "application/json")
        req.add_header("User-Agent", self._user_agent)
        for k, v in (extra_headers or {}).items():
            req.add_header(k, v)

        try:
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                resp_body = resp.read().decode()
                if not resp_body:
                    return {}
                return json.loads(resp_body)
        except urllib.error.HTTPError as e:
            resp_body = e.read().decode() if e.fp else ""
            try:
                err_data = json.loads(resp_body)
                code = err_data.get("code", "UNKNOWN")
                message = err_data.get("message", resp_body)
            except (json.JSONDecodeError, ValueError):
                code = "UNKNOWN"
                message = resp_body
            raise AttuneAPIError(e.code, code, message) from e
        except TimeoutError as e:
            raise AttuneTimeoutError(f"Request to {path} timed out") from e
        except Exception as e:
            raise AttuneError(f"Request failed: {e}") from e

    def _get(
        self, path: str, params: dict[str, str] | None = None
    ) -> dict[str, Any]:
        return self._request("GET", path, params=params)

    def _post(
        self,
        path: str,
        body: dict[str, Any],
        extra_headers: dict[str, str] | None = None,
    ) -> dict[str, Any]:
        return self._request("POST", path, body=body, extra_headers=extra_headers)


def _strip_empty(d: dict[str, Any]) -> dict[str, Any]:
    return {k: v for k, v in d.items() if v}


def _dict_to_feedback(d: dict[str, Any]) -> Feedback:
    return Feedback(
        id=str(d.get("id", "")),
        content=d.get("content", ""),
        source=d.get("source", ""),
        type=d.get("type", ""),
        user_id=d.get("userId", ""),
        page_url=d.get("pageUrl", ""),
        enriched_title=d.get("enrichedTitle"),
        enriched_attrs=d.get("enrichedAttrs", {}),
        is_urgent=d.get("isUrgent", False),
        enrichment_status=d.get("enrichmentStatus", ""),
        created_at=d.get("createdAt", ""),
        language=d.get("language"),
        tags=d.get("tags", []),
    )


def _dict_to_feedback_detail(d: dict[str, Any]) -> FeedbackDetail:
    fb = _dict_to_feedback(d)
    return FeedbackDetail(
        **{k: v for k, v in fb.__dict__.items()},
        source_meta=d.get("sourceMeta", {}),
        enrichment_error=d.get("enrichmentError"),
        reply_draft=d.get("replyDraft"),
        reply_draft_generated_at=d.get("replyDraftGeneratedAt"),
    )
