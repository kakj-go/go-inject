#!/usr/bin/env python3
"""Select a native C compiler and export it for subsequent GitHub Actions steps."""

from __future__ import annotations

import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile


def compiler_configuration(system: str, environment: dict[str, str]) -> tuple[str, dict[str, str], list[str]]:
    exports = {"CGO_ENABLED": "1"}
    paths: list[str] = []
    if system == "Darwin":
        compiler = subprocess.check_output(["xcrun", "--sdk", "macosx", "--find", "clang"], text=True).strip()
        sdk = subprocess.check_output(["xcrun", "--sdk", "macosx", "--show-sdk-path"], text=True).strip()
        if not Path(sdk).is_absolute() or not Path(sdk).is_dir():
            raise SystemExit(f"xcrun returned an unavailable macOS SDK: {sdk!r}")
        exports["SDKROOT"] = sdk
    elif system in ("Linux", "Windows"):
        compiler = shutil.which("gcc", path=environment.get("PATH"))
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
    exports["CC"] = f'"{compiler}"' if " " in compiler else compiler
    if system == "Windows":
        # GCC's runtime DLLs must be discoverable. Preserve setup-go ahead of
        # this directory: GitHub Actions prepends the last path written first.
        go = shutil.which("go", path=environment.get("PATH"))
        if go is None:
            raise SystemExit("Go was not found; configure the Go toolchain before CGO")
        paths.append(str(Path(compiler).parent))
        go_directory = str(Path(go).resolve().parent)
        if go_directory not in paths:
            paths.append(go_directory)
    # Unix compilers are invoked by absolute path. Prepending /usr/bin (or an
    # Xcode toolchain directory) can replace the Go/Python selected by setup-*.
    return compiler, exports, paths


def probe_compiler(compiler: str, system: str, environment: dict[str, str]) -> None:
    source = r"""
#include <stdlib.h>
#include <errno.h>
#include <stdio.h>
#ifndef _WIN32
#include <pthread.h>
static void *worker(void *value) { return value; }
#endif
int main(void) {
    char *end = NULL;
    errno = 0;
    if (strtol("42", &end, 10) != 42 || errno != 0 || *end != '\0') return 1;
#ifndef _WIN32
    pthread_t thread;
    if (pthread_create(&thread, NULL, worker, NULL) != 0) return 2;
    if (pthread_join(thread, NULL) != 0) return 3;
#endif
    puts("PASS native C compiler probe");
    return 0;
}
"""
    with tempfile.TemporaryDirectory(prefix="go-inject-cgo-probe-") as temporary:
        directory = Path(temporary)
        input_file = directory / "probe.c"
        binary = directory / ("probe.exe" if system == "Windows" else "probe")
        input_file.write_text(source, encoding="utf-8")
        flags = [] if system == "Windows" else ["-pthread"]
        subprocess.run(
            [compiler, *flags, str(input_file), "-o", str(binary)],
            env=environment, check=True, timeout=60,
        )
        subprocess.run([str(binary)], env=environment, check=True, timeout=60)


def main() -> None:
    system = platform.system()
    environment = dict(os.environ)
    compiler, exports, paths = compiler_configuration(system, environment)
    environment.update(exports)
    if paths:
        environment["PATH"] = os.pathsep.join([*reversed(paths), environment.get("PATH", "")])
    subprocess.run([compiler, "--version"], env=environment, check=True)
    # Probe with exactly the SDK/PATH settings exported to subsequent Go jobs.
    # A missing SDK or a non-native compiler fails here, without disabling CGO.
    probe_compiler(compiler, system, environment)
    with open(os.environ["GITHUB_ENV"], "a", encoding="utf-8") as output:
        for name, value in exports.items():
            output.write(f"{name}={value}\n")
            print(f"{name}={value}", flush=True)
    if paths:
        with open(os.environ["GITHUB_PATH"], "a", encoding="utf-8") as output:
            output.write("\n".join(paths) + "\n")


if __name__ == "__main__":
    main()
