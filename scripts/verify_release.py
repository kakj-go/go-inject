#!/usr/bin/env python3
"""Verify a downloaded archive and run its exact binary with the active Go version."""

from __future__ import annotations

import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile
import zipfile

from package_release import DOCUMENTS, GO_VERSION, TARGETS, VERSION_PATTERN, archive_name


ROOT = Path(__file__).resolve().parent.parent


def checksums(directory: Path) -> dict[str, str]:
    result: dict[str, str] = {}
    for line in (directory / "SHA256SUMS").read_text(encoding="utf-8").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([^/\\]+)", line)
        if match is None or match[2] in result:
            raise ValueError(f"Invalid or duplicate checksum entry: {line!r}")
        result[match[2]] = match[1]
    return result


def extract_archive(archive: Path, directory: Path, system: str) -> Path:
    binary_name = "go-inject.exe" if system == "windows" else "go-inject"
    expected = {binary_name, *DOCUMENTS}
    contents: dict[str, bytes] = {}
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as source:
            for entry in source.infolist():
                if entry.filename not in expected or entry.filename in contents or entry.is_dir():
                    raise ValueError(f"Unexpected archive member: {entry.filename}")
                if ((entry.external_attr >> 16) & 0o170000) not in (0, 0o100000):
                    raise ValueError(f"Archive member is not a regular file: {entry.filename}")
                contents[entry.filename] = source.read(entry)
    else:
        with tarfile.open(archive, "r:gz") as source:
            for entry in source.getmembers():
                if entry.name not in expected or entry.name in contents or not entry.isfile():
                    raise ValueError(f"Unexpected archive member: {entry.name}")
                stream = source.extractfile(entry)
                if stream is None:
                    raise ValueError(f"Unreadable archive member: {entry.name}")
                contents[entry.name] = stream.read()
    if set(contents) != expected:
        raise ValueError(f"Archive members differ from {sorted(expected)}")
    directory.mkdir(parents=True, exist_ok=True)
    for name, content in contents.items():
        with (directory / name).open("xb") as result:
            result.write(content)
    binary = directory / binary_name
    binary.chmod(0o755)
    return binary


def output(*args: str) -> str:
    return subprocess.check_output(args, cwd=ROOT, text=True, encoding="utf-8", errors="replace").strip()


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--goos", choices=("linux", "windows", "darwin"), required=True)
    parser.add_argument("--goarch", choices=("amd64", "arm64"), required=True)
    parser.add_argument("--go-version", choices=("1.26.8", "1.27.1"), required=True)
    args = parser.parse_args()
    if not VERSION_PATTERN.fullmatch(args.version) or re.fullmatch(r"[0-9a-f]{40}", args.commit) is None:
        parser.error("invalid release version or commit")
    actual = output("go", "env", "GOVERSION", "GOHOSTOS", "GOHOSTARCH").splitlines()
    expected = ["go" + args.go_version, args.goos, args.goarch]
    if actual != expected:
        parser.error(f"native validation requires {expected}, got {actual}")
    directory = args.directory.resolve()
    manifest = checksums(directory)
    expected_names = {archive_name(args.version, system, arch) for system, arch in TARGETS}
    if set(manifest) != expected_names:
        parser.error("SHA256SUMS must contain exactly the six release archives")
    name = archive_name(args.version, args.goos, args.goarch)
    archive = directory / name
    if hashlib.sha256(archive.read_bytes()).hexdigest() != manifest[name]:
        parser.error(f"checksum mismatch: {name}")
    with tempfile.TemporaryDirectory(prefix="go-inject-release-check-") as workspace:
        binary = extract_archive(archive, Path(workspace) / "unpacked", args.goos)
        version = output(str(binary), "version")
        if args.version not in version or args.commit not in version:
            raise SystemExit(f"unexpected embedded version/commit: {version}")
        build = output("go", "version", "-m", str(binary))
        if GO_VERSION not in build.splitlines()[0] or "CGO_ENABLED=0" not in build:
            raise SystemExit(f"unexpected release build metadata:\n{build}")
        subprocess.run(
            [sys.executable, str(ROOT / "examples" / "check.py"), "basic", "http",
             "--no-vendor", "--tool", str(binary)],
            cwd=ROOT, env={**os.environ, "GOTOOLCHAIN": "local", "CGO_ENABLED": "0"}, check=True,
        )
    print(f"PASS release: {name} with Go {args.go_version}")


if __name__ == "__main__":
    main()
