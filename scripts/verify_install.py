#!/usr/bin/env python3
"""Install a remote CLI/module revision into isolated caches and run aggregate rules."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parent.parent
MODULE = "github.com/kakj-go/go-inject"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ref", required=True, help="immutable full commit SHA or release version")
    args = parser.parse_args()
    if not re.fullmatch(r"(?:[0-9a-f]{40}|v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?)", args.ref):
        parser.error("ref must be a full commit SHA or a semantic release version")
    go = shutil.which("go")
    if go is None:
        parser.error("Go must be configured before running installation checks")
    with tempfile.TemporaryDirectory(prefix="go-inject-install-") as temporary:
        stage = Path(temporary).resolve()
        environment = {
            **os.environ, "GOTOOLCHAIN": "local", "GOWORK": "off", "GOFLAGS": "", "CGO_ENABLED": "0",
            "GOPATH": str(stage / "gopath"), "GOMODCACHE": str(stage / "modules"),
            "GOBIN": str(stage / "bin"), "GOCACHE": str(stage / "build-cache"),
            "GOPROXY": "https://proxy.golang.org,direct", "GOSUMDB": "sum.golang.org",
            "GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "",
        }

        def run(command: list[str | Path], directory: Path = stage) -> str:
            actual = [str(part) for part in command]
            if actual[0] == "go":
                actual[0] = go
            print("RUN " + " ".join(actual), flush=True)
            process = subprocess.run(actual, cwd=directory, env=environment, text=True,
                                     encoding="utf-8", errors="replace", stdout=subprocess.PIPE,
                                     stderr=subprocess.STDOUT, timeout=1200)
            print(process.stdout, end="", flush=True)
            if process.returncode:
                raise SystemExit(process.returncode)
            return process.stdout

        suffix = ".exe" if os.name == "nt" else ""
        run(["go", "install", MODULE + "/cmd/go-inject@" + args.ref])
        tool = stage / "bin" / ("go-inject" + suffix)
        version_output = run([tool, "version"])
        run(["go", "version", "-m", tool])
        expected = args.ref if args.ref.startswith("v") else args.ref[:12]
        if expected not in version_output:
            raise SystemExit("installed CLI reports an unexpected revision")
        app = stage / "application"
        app.mkdir()
        for name in ("main.go", "main_test.go", "inject.go"):
            shutil.copyfile(ROOT / "examples" / "external" / name, app / name)
        (app / "go.mod").write_text(
            "module example.test/releasecheck\n\ngo 1.26.0\n\nrequire github.com/gin-gonic/gin v1.12.0\n",
            encoding="utf-8",
        )
        run(["go", "get", MODULE + "/examples/rules@" + args.ref], app)
        run(["go", "mod", "tidy"], app)
        module = json.loads(run(["go", "list", "-m", "-json", MODULE + "/examples/rules"], app))
        if module.get("Replace") or not module["Version"].endswith(expected):
            raise SystemExit("external rules did not resolve the requested remote revision")
        run(["go", "test", '-toolexec="' + str(tool) + '"', "."], app)
        binary = stage / ("release-check" + suffix)
        run(["go", "build", '-toolexec="' + str(tool) + '"', "-o", binary, "."], app)
        if "PASS external:" not in run([binary], app):
            raise SystemExit("external aggregate rule behavior is missing")
    print("PASS clean remote CLI installation and aggregate rules: " + args.ref)


if __name__ == "__main__":
    main()
