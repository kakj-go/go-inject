#!/usr/bin/env python3
"""Build six CGO-free release archives with fixed Go and deterministic metadata."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import zipfile


ROOT = Path(__file__).resolve().parent.parent
GO_VERSION = "go1.27.1"
TARGETS = tuple((system, arch) for system in ("linux", "windows", "darwin") for arch in ("amd64", "arm64"))
DOCUMENTS = ("README.md", "README_CN.md", "LICENSE")
VERSION_PATTERN = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$")


def command(*args: str) -> str:
    return subprocess.check_output(args, cwd=ROOT, text=True).strip()


def archive_name(version: str, system: str, arch: str) -> str:
    suffix = "zip" if system == "windows" else "tar.gz"
    return f"go-inject_{version}_{system}_{arch}.{suffix}"


def write_archive(destination: Path, binary: Path, epoch: int) -> None:
    members = [(binary.name, binary.read_bytes(), 0o755)]
    members.extend((name, (ROOT / name).read_bytes(), 0o644) for name in DOCUMENTS)
    members.sort(key=lambda item: item[0])
    if destination.suffix == ".zip":
        stamp = datetime.fromtimestamp(max(epoch, 315532800), timezone.utc)
        date_time = (stamp.year, stamp.month, stamp.day, stamp.hour, stamp.minute, stamp.second // 2 * 2)
        with zipfile.ZipFile(destination, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as output:
            for name, data, mode in members:
                entry = zipfile.ZipInfo(name, date_time=date_time)
                entry.create_system = 3
                entry.external_attr = (0o100000 | mode) << 16
                output.writestr(entry, data, compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
    else:
        with destination.open("wb") as raw:
            with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=epoch, compresslevel=9) as compressed:
                with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as output:
                    for name, data, mode in members:
                        entry = tarfile.TarInfo(name)
                        entry.size = len(data)
                        entry.mode = mode
                        entry.mtime = epoch
                        output.addfile(entry, io.BytesIO(data))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="v0.1.0-beta.1")
    parser.add_argument("--commit")
    parser.add_argument("--output", type=Path, default=ROOT / "dist")
    args = parser.parse_args()
    if not VERSION_PATTERN.fullmatch(args.version):
        parser.error("version must be a Go semantic release version such as v0.1.0-beta.1")
    commit = args.commit or command("git", "rev-parse", "HEAD")
    if re.fullmatch(r"[0-9a-f]{40}", commit) is None:
        parser.error("commit must be the full lowercase Git commit ID")
    active_go = subprocess.check_output(
        ["go", "env", "GOVERSION"], cwd=ROOT,
        env={**os.environ, "GOTOOLCHAIN": "local"}, text=True,
    ).strip()
    if active_go != GO_VERSION:
        parser.error(f"release packaging requires the active Go toolchain to be {GO_VERSION}")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    names = [archive_name(args.version, system, arch) for system, arch in TARGETS]
    for name in [*names, "SHA256SUMS"]:
        if (output / name).exists():
            parser.error(f"refusing to replace an existing artifact: {output / name}")
    epoch = int(os.environ.get("SOURCE_DATE_EPOCH", command("git", "show", "-s", "--format=%ct", commit)))
    with tempfile.TemporaryDirectory(prefix="go-inject-package-") as directory:
        stage = Path(directory)
        for system, arch in TARGETS:
            binary = stage / ("go-inject.exe" if system == "windows" else "go-inject")
            environment = {
                **os.environ, "GOOS": system, "GOARCH": arch,
                "CGO_ENABLED": "0", "GOTOOLCHAIN": "local", "GOWORK": "off",
                "GOFLAGS": "", "GOEXPERIMENT": "", "GOAMD64": "v1", "GOARM64": "v8.0",
            }
            subprocess.run(
                ["go", "build", "-trimpath", "-buildvcs=false", "-ldflags",
                 f"-s -w -X main.version={args.version} -X main.commit={commit}",
                 "-o", str(binary), "./cmd/go-inject"],
                cwd=ROOT, env=environment, check=True,
            )
            destination = stage / archive_name(args.version, system, arch)
            write_archive(destination, binary, epoch)
            print(f"Built {destination.name}", flush=True)
        checksums = "".join(
            f"{hashlib.sha256((stage / name).read_bytes()).hexdigest()}  {name}\n"
            for name in sorted(names)
        )
        for name in names:
            with (output / name).open("xb") as result:
                result.write((stage / name).read_bytes())
        with (output / "SHA256SUMS").open("x", encoding="utf-8", newline="\n") as result:
            result.write(checksums)
    print(f"Release artifacts: {output}")


if __name__ == "__main__":
    main()
