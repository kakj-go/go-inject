#!/usr/bin/env python3
"""Run the examples in disposable copies with a real go-inject executable."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile


EXAMPLES = ("basic", "http", "gin", "external")


def run(args: list[str], cwd: Path, *, success: bool = True) -> str:
    temporary = str(cwd.parent.parent / "tmp")
    result = subprocess.run(
        args,
        cwd=cwd,
        env={**os.environ, "GOWORK": "off", "TMPDIR": temporary, "TMP": temporary, "TEMP": temporary},
        text=True,
        encoding="utf-8",
        errors="replace",
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        timeout=600,
    )
    if (result.returncode == 0) != success:
        expected = "success" if success else "failure"
        raise RuntimeError(
            f"Expected {expected}: {args!r}\n"
            f"Working directory: {cwd}\nExit: {result.returncode}\n{result.stdout}"
        )
    return result.stdout


def digest(directory: Path) -> dict[str, str]:
    if not directory.exists():
        return {}
    return {
        str(path.relative_to(directory)): hashlib.sha256(path.read_bytes()).hexdigest()
        for path in sorted(directory.rglob("*"))
        if path.is_file()
    }


def assert_program(tool: str, directory: Path, output: Path, name: str) -> None:
    build_output = run([tool, "build", "-work", "-o", str(output), "."], directory)
    marker = re.search(r"(?m)^GOINJECT_WORK=(.+)$", build_output)
    if marker is None:
        raise RuntimeError(f"{name}: -work did not report GOINJECT_WORK=<directory>:\n{build_output}")
    session = marker.group(1).strip().strip('"')
    inspection = run([tool, "inspect", "--json", session], directory)
    report = json.loads(inspection)
    if not any(rule.get("State") == "selected" for rule in report.get("Rules", [])):
        raise RuntimeError(f"{name}: inspection did not contain a selected rule")
    if not any(package.get("Result", {}).get("Matches") for package in report.get("Packages", [])):
        raise RuntimeError(f"{name}: inspection did not contain an applied source match")
    actual = run([str(output)], directory)
    expected = "quote=35 early=0" if name == "basic" else f"PASS {name}:"
    if expected not in actual:
        raise RuntimeError(f"{name}: expected {expected!r}, got:\n{actual}")
    if name == "basic" and "injection ready" not in actual:
        raise RuntimeError("basic: the added main initializer did not execute")


def check_vendor(tool: str, directory: Path, output: Path) -> None:
    run(["go", "mod", "vendor"], directory)
    baseline = digest(directory / "vendor")
    module_files = {name: (directory / name).read_bytes() for name in ("go.mod", "go.sum")}
    run([tool, "vendor", "."], directory)
    once = digest(directory / "vendor")
    if once == baseline:
        raise RuntimeError("gin: vendor did not change any source")
    run([tool, "vendor", "."], directory)
    if digest(directory / "vendor") != once:
        raise RuntimeError("gin: repeated vendor injection changed the result")
    run(["go", "test", "-mod=vendor", "."], directory)
    run(["go", "build", "-mod=vendor", "-o", str(output), "."], directory)
    actual = run([str(output)], directory)
    if "PASS gin:" not in actual:
        raise RuntimeError(f"gin: native vendor binary failed its self-check:\n{actual}")
    run([tool, "vendor", "--restore"], directory)
    if digest(directory / "vendor") != baseline:
        raise RuntimeError("gin: vendor --restore did not restore the original files")
    for name, content in module_files.items():
        if (directory / name).read_bytes() != content:
            raise RuntimeError(f"gin: vendor --restore changed {name}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("examples", nargs="*", choices=EXAMPLES)
    parser.add_argument("--tool", default=os.environ.get("GOINJECT_BINARY", "go-inject"))
    parser.add_argument("--gin-version", choices=("v1.11.0", "v1.12.0"))
    parser.add_argument("--no-vendor", action="store_true")
    parser.add_argument("--keep-work", action="store_true")
    args = parser.parse_args()
    tool = shutil.which(args.tool)
    if tool is None:
        parser.error("go-inject was not found; pass --tool or set GOINJECT_BINARY")
    tool = str(Path(tool).resolve())
    work = Path(tempfile.mkdtemp(prefix="go-inject-examples-"))
    try:
        source = Path(__file__).resolve().parent
        copied = work / "examples"
        shutil.copytree(
            source,
            copied,
            ignore=shutil.ignore_patterns("vendor", "__pycache__", ".goinject", ".go-inject", "*.exe"),
        )
        binary_dir = work / "bin"
        binary_dir.mkdir()
        (work / "tmp").mkdir()
        for name in args.examples or EXAMPLES:
            directory = copied / name
            if args.gin_version and name in ("gin", "external"):
                run(["go", "get", "github.com/gin-gonic/gin@" + args.gin_version], directory)
            run(["go", "mod", "tidy"], directory)
            run([tool, "test", "."], directory)
            binary = binary_dir / (name + (".exe" if os.name == "nt" else ""))
            assert_program(tool, directory, binary, name)
            if name == "gin" and not args.no_vendor:
                check_vendor(tool, directory, binary)
            if name in ("http", "external") and not args.no_vendor:
                before = digest(directory)
                run([tool, "vendor", "."], directory, success=False)
                if digest(directory) != before:
                    raise RuntimeError(f"{name}: rejected standard-library vendor request changed project files")
            print(f"PASS {name}", flush=True)
        return 0
    except (OSError, RuntimeError, subprocess.TimeoutExpired, json.JSONDecodeError) as exc:
        print(str(exc), file=sys.stderr)
        print(f"Example workspace: {work}", file=sys.stderr)
        args.keep_work = True
        return 1
    finally:
        if args.keep_work:
            print(f"Kept example workspace: {work}", file=sys.stderr)
        else:
            shutil.rmtree(work)


if __name__ == "__main__":
    raise SystemExit(main())
