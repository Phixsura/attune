#!/usr/bin/env python3
"""Check that relative markdown links resolve to real files.

The commonest doc rot is a moved/renamed file leaving a dangling [..](path) link.
This walks every *.md, strips fenced + inline code, and verifies each *relative*
inline link target exists.

Scope (intentionally narrow — a cheap gate, not a comprehensive checker):
  - checks: inline `[text](relative/path)` targets
  - skips:  external (http/mailto/tel), pure #anchors, templated `{...}`, IMAGE
            links `![..](..)`, reference-style `[x]: url`, and #anchor validity.
Run in CI (.github/workflows/ci.yml: doc-links). Stdlib only.
"""
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SKIP_DIRS = {".git", "node_modules", "dist", "build", ".cache", "out"}
FENCE = re.compile(r"```.*?```", re.S)            # drop fenced code blocks
INLINE = re.compile(r"`[^`]*`")                   # drop inline code spans
LINK = re.compile(r"(!?)\[[^\]]*\]\(\s*<?([^)>\s]+)")  # (image-marker, target)


def is_external(target: str) -> bool:
    return target.startswith(("http://", "https://", "mailto:", "tel:", "#")) or "{" in target


def main() -> int:
    broken = []
    for dirpath, dirs, files in os.walk(ROOT):
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS]
        for name in files:
            if not name.endswith(".md"):
                continue
            path = os.path.join(dirpath, name)
            with open(path, encoding="utf-8") as fh:
                text = INLINE.sub("", FENCE.sub("", fh.read()))
            for is_image, target in LINK.findall(text):
                if is_image == "!" or is_external(target):
                    continue
                rel = target.split("#", 1)[0].split("?", 1)[0]
                if not rel:
                    continue
                resolved = os.path.normpath(os.path.join(dirpath, rel))
                if not os.path.exists(resolved):
                    broken.append(f"{os.path.relpath(path, ROOT)}  ->  {target}")

    if broken:
        print("❌ Broken relative markdown links:")
        for b in sorted(set(broken)):
            print("   " + b)
        return 1
    print("✅ All relative markdown links resolve.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
