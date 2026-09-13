#!/usr/bin/env python3
"""Select a native C compiler and export it for subsequent GitHub Actions steps."""

from __future__ import annotations

import os
from pathlib import Path
import platform
import shutil
import subprocess


def main() -> None:
    system = platform.system()
    if system == "Darwin":
        compiler = subprocess.check_output(["xcrun", "--find", "clang"], text=True).strip()
    elif system in ("Linux", "Windows"):
        compiler = shutil.which("gcc")
        if compiler is None and system == "Windows":
            candidates = (
                Path("C:/msys64/ucrt64/bin/gcc.exe"),
                Path("C:/msys64/mingw64/bin/gcc.exe"),
                Path("C:/mingw64/bin/gcc.exe"),
            )
            compiler = next((str(path) for path in candidates if path.is_file()), None)
        if compiler is None:
            raise SystemExit("Native GCC was not found; this CI job requires a C toolchain")
    else:
        raise SystemExit(f"Unsupported C toolchain platform: {system}")
    compiler = str(Path(compiler).resolve())
    subprocess.run([compiler, "--version"], check=True)
    print(f"CC={compiler}\nCGO_ENABLED=1", flush=True)
    cc_value = f'"{compiler}"' if " " in compiler else compiler
    with open(os.environ["GITHUB_ENV"], "a", encoding="utf-8") as output:
        output.write(f"CC={cc_value}\nCGO_ENABLED=1\n")
    with open(os.environ["GITHUB_PATH"], "a", encoding="utf-8") as output:
        output.write(str(Path(compiler).parent) + "\n")


if __name__ == "__main__":
    main()
