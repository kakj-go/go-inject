#!/usr/bin/env python3
"""Fail when PATH or environment changes replace the matrix's native Go toolchain."""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess


FIELDS = ("GOVERSION", "GOHOSTOS", "GOHOSTARCH", "GOOS", "GOARCH", "GOTOOLCHAIN", "CGO_ENABLED", "CC")


def validate(actual: dict[str, str], version: str, system: str, arch: str, cgo: str) -> None:
    expected = {
        "GOVERSION": "go" + version, "GOHOSTOS": system, "GOHOSTARCH": arch,
        "GOOS": system, "GOARCH": arch, "GOTOOLCHAIN": "local", "CGO_ENABLED": cgo,
    }
    errors = [f"{name}: expected {want!r}, got {actual.get(name)!r}" for name, want in expected.items() if actual.get(name) != want]
    if errors:
        raise ValueError("Go toolchain does not match the native job:\n" + "\n".join(errors))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go-version", required=True)
    parser.add_argument("--goos", choices=("linux", "windows", "darwin"), required=True)
    parser.add_argument("--goarch", choices=("amd64", "arm64"), required=True)
    parser.add_argument("--cgo", choices=("0", "1"), required=True)
    args = parser.parse_args()
    print(f"Go executable: {shutil.which('go')}", flush=True)
    actual = json.loads(subprocess.check_output(["go", "env", "-json", *FIELDS], text=True))
    print(json.dumps(actual, indent=2), flush=True)
    try:
        validate(actual, args.go_version, args.goos, args.goarch, args.cgo)
    except ValueError as error:
        raise SystemExit(str(error)) from error
    print("PASS native Go toolchain")


if __name__ == "__main__":
    main()
