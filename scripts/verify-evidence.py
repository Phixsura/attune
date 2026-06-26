#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0

"""
Reference verifier for attune audit evidence exports (Python).

Verifies per-file SHA-256 hashes and SHA-256 hash chain integrity.
Ed25519 signature verification requires PyNaCl: pip install pynacl

Usage:
  python3 scripts/verify-evidence.py archive.zip [--public-key HEX]
"""

import argparse
import hashlib
import json
import sys
import zipfile


def sha256_hex(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def canonical_json(obj) -> bytes:
    """Minimal RFC 8785: sorted keys, compact separators, no trailing space."""
    return json.dumps(
        obj, sort_keys=True, separators=(",", ":"), ensure_ascii=False
    ).encode("utf-8")


def verify(archive_path: str, public_key_hex: str | None = None) -> bool:
    ok = True

    with zipfile.ZipFile(archive_path) as zf:
        manifest_data = zf.read("manifest.json")
        manifest = json.loads(manifest_data)

        print(f"Archive:      {archive_path}")
        print(f"Format:       {manifest.get('format', 'unknown')}")
        print(f"Export ID:    {manifest.get('export_id', 'unknown')}")
        print(f"Tenant:       {manifest.get('tenant_id', 'unknown')}")
        total = manifest.get("stats", {}).get("total_events", 0)
        print(f"Events:       {total}")
        print()

        for f in manifest.get("files", []):
            content = zf.read(f["name"])
            actual = sha256_hex(content)
            if actual != f["sha256"]:
                print(f"FAIL  {f['name']} hash mismatch")
                ok = False
            else:
                print(f"PASS  {f['name']} hash verified (SHA-256)")

        events_data = zf.read("events.jsonl")
        trimmed = events_data.decode("utf-8").strip()
        lines = trimmed.split("\n") if trimmed else []

        integrity = manifest.get("integrity", {})
        event_count = integrity.get("event_count", 0)

        if len(lines) != event_count:
            print(f"FAIL  event count mismatch: manifest {event_count}, found {len(lines)}")
            ok = False
        elif event_count == 0:
            expected = sha256_hex(b"empty")
            if integrity.get("chain_hash") != expected:
                print("FAIL  empty chain hash mismatch")
                ok = False
            else:
                print("PASS  hash chain verified (0 events)")
        else:
            prev_hash = sha256_hex(b"genesis")
            for i, line in enumerate(lines):
                entry = json.loads(line)
                event_canonical = canonical_json(entry["event"])
                event_hash = sha256_hex(event_canonical)
                if event_hash != entry["event_hash"]:
                    print(f"FAIL  event {i + 1}: event_hash mismatch")
                    ok = False
                    break
                chain_input = event_canonical + prev_hash.encode("utf-8")
                chain_hash = sha256_hex(chain_input)
                if chain_hash != entry["chain_hash"]:
                    print(f"FAIL  event {i + 1}: chain_hash mismatch")
                    ok = False
                    break
                prev_hash = chain_hash
            else:
                if prev_hash != integrity.get("chain_hash"):
                    print(f"FAIL  final chain hash mismatch")
                    ok = False
                else:
                    print(f"PASS  hash chain integrity verified ({event_count} events)")

        if "manifest.sig" in zf.namelist():
            if public_key_hex:
                try:
                    from nacl.signing import VerifyKey

                    verify_key = VerifyKey(bytes.fromhex(public_key_hex))
                    sig = zf.read("manifest.sig")
                    verify_key.verify(manifest_data, sig)
                    signing = manifest.get("signing", {})
                    fp = signing.get("public_key_fingerprint", "")
                    suffix = f" (key: sha256:{fp[:12]}...)" if fp else ""
                    print(f"PASS  Ed25519 signature verified{suffix}")
                except ImportError:
                    print("WARN  PyNaCl not installed; skipping signature verification")
                    print("      Install with: pip install pynacl")
                except Exception as e:
                    print(f"FAIL  signature verification: {e}")
                    ok = False
            else:
                print("WARN  manifest.sig present but no --public-key provided")
        else:
            if public_key_hex:
                print("FAIL  --public-key provided but no manifest.sig in archive")
                ok = False
            else:
                print("SKIP  no manifest.sig (unsigned)")

    return ok


def main():
    parser = argparse.ArgumentParser(
        description="Verify attune audit evidence export archives"
    )
    parser.add_argument("archive", help="Path to the evidence ZIP archive")
    parser.add_argument(
        "--public-key",
        help="Hex-encoded Ed25519 public key for signature verification",
    )
    args = parser.parse_args()

    success = verify(args.archive, args.public_key)
    print()
    if success:
        print("Result: ALL CHECKS PASSED")
    else:
        print("Result: VERIFICATION FAILED")
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
