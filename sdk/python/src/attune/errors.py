"""Error types for the attune SDK."""

from __future__ import annotations


class AttuneError(Exception):
    """Base exception for all attune SDK errors."""


class AttuneAPIError(AttuneError):
    """Raised when the attune API returns an error response."""

    def __init__(self, status: int, code: str, message: str) -> None:
        self.status = status
        self.code = code
        super().__init__(f"[{status}] {code}: {message}")


class AttuneTimeoutError(AttuneError):
    """Raised when a request times out."""
