"""Regressions for PATH precedence, macOS SDK propagation, and native job guards."""

from __future__ import annotations

import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import check_toolchain
import configure_cgo


class CompilerConfigurationTests(unittest.TestCase):
    def test_unix_keeps_setup_go_path_and_exports_sdk_to_probe_and_job(self) -> None:
        for system in ("Linux", "Darwin"):
            with self.subTest(system=system), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                compiler = directory / "system-bin" / ("clang" if system == "Darwin" else "gcc")
                sdk = directory / "MacOSX.sdk"
                sdk.mkdir()
                environment_file, path_file = directory / "env", directory / "path"
                environment_file.write_text("EXISTING=value\n", encoding="utf-8")
                path_file.write_text("existing-helper-directory\n", encoding="utf-8")
                original_path = os.pathsep.join([str(directory / "setup-go" / "bin"), str(compiler.parent)])
                environment = {"PATH": original_path, "GITHUB_ENV": str(environment_file), "GITHUB_PATH": str(path_file)}
                with patch.dict(os.environ, environment, clear=True), \
                     patch("configure_cgo.platform.system", return_value=system), \
                     patch("configure_cgo.shutil.which", return_value=str(compiler)), \
                     patch("configure_cgo.subprocess.check_output", side_effect=[str(compiler), str(sdk)]), \
                     patch("configure_cgo.subprocess.run"), \
                     patch("configure_cgo.probe_compiler") as probe:
                    configure_cgo.main()
                self.assertEqual(path_file.read_text(encoding="utf-8"), "existing-helper-directory\n")
                self.assertEqual(probe.call_args.args[2]["PATH"], original_path)
                self.assertEqual(probe.call_args.args[2]["CGO_ENABLED"], "1")
                saved_environment = environment_file.read_text(encoding="utf-8")
                self.assertIn("CGO_ENABLED=1\n", saved_environment)
                if system == "Darwin":
                    self.assertEqual(probe.call_args.args[2]["SDKROOT"], str(sdk))
                    self.assertIn(f"SDKROOT={sdk}\n", saved_environment)

    def test_windows_adds_gcc_dll_directory_but_keeps_setup_go_first(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            compiler = directory / "gcc" / "bin" / "gcc.exe"
            go = directory / "setup-go" / "bin" / "go.exe"
            environment_file, path_file = directory / "env", directory / "path"
            original_path = str(go.parent) + os.pathsep + "existing-path"
            environment = {"PATH": original_path, "GITHUB_ENV": str(environment_file), "GITHUB_PATH": str(path_file)}
            with patch.dict(os.environ, environment, clear=True), \
                 patch("configure_cgo.platform.system", return_value="Windows"), \
                 patch("configure_cgo.shutil.which", side_effect=lambda name, **_: str(compiler if name == "gcc" else go)), \
                 patch("configure_cgo.subprocess.run"), \
                 patch("configure_cgo.probe_compiler") as probe:
                configure_cgo.main()
            # Actions prepends the last line first; this preserves setup-go.
            self.assertEqual(path_file.read_text(encoding="utf-8").splitlines(), [str(compiler.resolve().parent), str(go.resolve().parent)])
            probe_path = probe.call_args.args[2]["PATH"].split(os.pathsep)
            self.assertEqual(probe_path[:2], [str(go.resolve().parent), str(compiler.resolve().parent)])

    def test_failed_c_probe_does_not_export_a_partial_configuration(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            environment_file, path_file = directory / "env", directory / "path"
            environment_file.write_text("EXISTING=value\n", encoding="utf-8")
            path_file.write_text("existing-path\n", encoding="utf-8")
            environment = {"PATH": "unchanged", "GITHUB_ENV": str(environment_file), "GITHUB_PATH": str(path_file)}
            with patch.dict(os.environ, environment, clear=True), \
                 patch("configure_cgo.platform.system", return_value="Linux"), \
                 patch("configure_cgo.shutil.which", return_value=str(directory / "gcc")), \
                 patch("configure_cgo.subprocess.run"), \
                 patch("configure_cgo.probe_compiler", side_effect=subprocess.CalledProcessError(1, "gcc")):
                with self.assertRaises(subprocess.CalledProcessError):
                    configure_cgo.main()
            self.assertEqual(environment_file.read_text(encoding="utf-8"), "EXISTING=value\n")
            self.assertEqual(path_file.read_text(encoding="utf-8"), "existing-path\n")

    def test_probe_compiles_headers_links_pthreads_and_runs_with_sdk_environment(self) -> None:
        environment = {"PATH": "setup-go-first", "SDKROOT": "/selected/MacOSX.sdk"}
        commands: list[list[str]] = []

        def run(command: list[str], **kwargs: object) -> None:
            self.assertEqual(kwargs["env"], environment)
            self.assertTrue(kwargs["check"])
            commands.append(command)
            if len(commands) == 1:
                source = Path(command[2]).read_text(encoding="utf-8")
                for text in ("stdlib.h", "errno.h", "pthread.h", "pthread_create", "pthread_join"):
                    self.assertIn(text, source)

        with patch("configure_cgo.subprocess.run", side_effect=run):
            configure_cgo.probe_compiler("/selected/clang", "Darwin", environment)
        self.assertEqual(commands[0][:2], ["/selected/clang", "-pthread"])
        self.assertEqual(commands[1], [commands[0][-1]])

    def test_missing_macos_sdk_fails_instead_of_disabling_cgo(self) -> None:
        with patch("configure_cgo.subprocess.check_output", side_effect=["/selected/clang", "not-an-absolute-sdk"]):
            with self.assertRaisesRegex(SystemExit, "unavailable macOS SDK"):
                configure_cgo.compiler_configuration("Darwin", {"PATH": "original"})


class GoToolchainGuardTests(unittest.TestCase):
    def test_rejects_old_path_go_cross_target_and_automatic_toolchain(self) -> None:
        actual = {
            "GOVERSION": "go1.27.1", "GOHOSTOS": "linux", "GOHOSTARCH": "arm64",
            "GOOS": "linux", "GOARCH": "arm64", "GOTOOLCHAIN": "local", "CGO_ENABLED": "1",
        }
        check_toolchain.validate(actual, "1.27.1", "linux", "arm64", "1")
        for key, value in (("GOVERSION", "go1.24.13"), ("GOARCH", "amd64"), ("GOHOSTARCH", "amd64"), ("GOTOOLCHAIN", "auto"), ("CGO_ENABLED", "0")):
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, key):
                check_toolchain.validate({**actual, key: value}, "1.27.1", "linux", "arm64", "1")


if __name__ == "__main__":
    unittest.main()
