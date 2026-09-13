#!/usr/bin/env python3
"""Keep complete README Go snippets identical to executable example sources."""
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parent.parent


def formatted(source: str) -> bytes:
    return subprocess.check_output(["gofmt"], input=source.encode("utf-8"))


def main() -> None:
    expected = [
        formatted((ROOT / "examples/basic/inject/quote/quote.go").read_text(encoding="utf-8")),
        formatted((ROOT / "examples/gin/inject.go").read_text(encoding="utf-8")),
    ]
    for name in ("README.md", "README_CN.md"):
        text = (ROOT / name).read_text(encoding="utf-8")
        actual = [formatted(code) for code in re.findall(r"```go\n(.*?)\n```", text, re.S)]
        if actual != expected:
            raise SystemExit(f"{name}: complete Go snippets differ from the executable examples")
        for command in ('go build -a -toolexec="go-inject" .', 'go generate .', 'go build -mod=vendor .'):
            if command not in text:
                raise SystemExit(f"{name}: missing native command {command}")
    print("PASS README snippets: both languages match executable Go examples")


if __name__ == "__main__":
    main()
