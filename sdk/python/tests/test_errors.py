"""Tests for attune errors."""

from attune.errors import AttuneAPIError, AttuneError, AttuneTimeoutError


def test_api_error_message() -> None:
    err = AttuneAPIError(status=429, code="RATE_LIMITED", message="slow down")
    assert err.status == 429
    assert err.code == "RATE_LIMITED"
    assert "429" in str(err)
    assert "slow down" in str(err)


def test_api_error_inherits_base() -> None:
    err = AttuneAPIError(status=500, code="INTERNAL", message="oops")
    assert isinstance(err, AttuneError)


def test_timeout_error_inherits_base() -> None:
    err = AttuneTimeoutError("timed out")
    assert isinstance(err, AttuneError)
