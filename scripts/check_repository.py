#!/usr/bin/env python3
"""Check first-party Go file sizes and repository-local documentation links."""

from __future__ import annotations

from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parent.parent
GO_ROOTS = {"cmd", "internal", "example", "examples"}
MARKDOWN_ROOTS = {"docs", "examples", "scripts"}
ROOT_DOCS = {"README.md", "README_CN.md", "CONTRIBUTING.md", "CONTRIBUTING_CN.md", "CHANGELOG.md"}


def files() -> list[Path]:
    result = subprocess.run(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
        cwd=ROOT, check=True, stdout=subprocess.PIPE,
    )
    return sorted({ROOT / name.decode("utf-8") for name in result.stdout.split(b"\0") if name})


def headings(path: Path) -> set[str]:
    anchors: set[str] = set()
    counts: dict[str, int] = {}
    fenced = False
    for line in path.read_text(encoding="utf-8-sig").splitlines():
        if line.lstrip().startswith(("```", "~~~")):
            fenced = not fenced
        if fenced:
            continue
        match = re.match(r"^#{1,6}\s+(.+?)\s*#*\s*$", line)
        if match:
            slug = re.sub(r"[^\w\s-]", "", match.group(1).lower()).replace(" ", "-")
            count = counts.get(slug, 0)
            counts[slug] = count + 1
            anchors.add(slug if count == 0 else f"{slug}-{count}")
    return anchors


def main() -> int:
    errors: list[str] = []
    go_count = doc_count = 0
    for path in files():
        relative = path.relative_to(ROOT)
        if not path.is_file() or "vendor" in relative.parts:
            continue
        if path.suffix == ".go" and (len(relative.parts) == 1 or relative.parts[0] in GO_ROOTS):
            go_count += 1
            lines = len(path.read_text(encoding="utf-8-sig").splitlines())
            if lines >= 2000:
                errors.append(f"{relative}: {lines} lines; first-party Go files must have fewer than 2000")
        if path.suffix != ".md" or not (relative.parts[0] in MARKDOWN_ROOTS or str(relative) in ROOT_DOCS):
            continue
        doc_count += 1
        for raw in re.findall(r"\]\(([^)]+)\)", path.read_text(encoding="utf-8-sig")):
            raw = raw.strip().removeprefix("<").removesuffix(">")
            link = urlsplit(raw)
            if link.scheme or link.netloc:
                continue
            target = (path.parent / unquote(link.path)).resolve() if link.path else path
            if not target.exists():
                errors.append(f"{relative}: missing link target {raw}")
            elif link.fragment and target.is_file() and target.suffix == ".md":
                if unquote(link.fragment) not in headings(target):
                    errors.append(f"{relative}: missing heading {raw}")
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print(f"PASS repository: {go_count} Go file sizes, {doc_count} Markdown documents")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
