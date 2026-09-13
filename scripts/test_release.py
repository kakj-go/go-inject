"""Regression checks for archive determinism, contents, and verification boundaries."""

from __future__ import annotations

import hashlib
import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import package_release
import verify_release


class ReleaseArchiveTests(unittest.TestCase):
    def test_archive_bytes_and_contents_are_reproducible(self) -> None:
        for system, suffix in (("linux", ".tar.gz"), ("windows", ".zip")):
            with self.subTest(system=system), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                for name in package_release.DOCUMENTS:
                    (directory / name).write_text(f"document: {name}\n", encoding="utf-8")
                binary = directory / ("go-inject.exe" if system == "windows" else "go-inject")
                binary.write_bytes(b"example executable\x00\xff")
                first, second = directory / ("first" + suffix), directory / ("second" + suffix)
                with patch.object(package_release, "ROOT", directory):
                    package_release.write_archive(first, binary, 1789257600)
                    package_release.write_archive(second, binary, 1789257600)
                self.assertEqual(first.read_bytes(), second.read_bytes())
                extracted = verify_release.extract_archive(first, directory / "unpacked", system)
                self.assertEqual(extracted.read_bytes(), binary.read_bytes())
                for name in package_release.DOCUMENTS:
                    self.assertEqual((extracted.parent / name).read_bytes(), (directory / name).read_bytes())

    def test_tar_rejects_links_and_paths_outside_archive_root(self) -> None:
        for name, member_type in (("../escape", tarfile.REGTYPE), ("go-inject", tarfile.SYMTYPE)):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                archive = directory / "invalid.tar.gz"
                with tarfile.open(archive, "w:gz") as output:
                    entry = tarfile.TarInfo(name)
                    entry.type = member_type
                    entry.linkname = "../escape" if member_type == tarfile.SYMTYPE else ""
                    output.addfile(entry, io.BytesIO())
                with self.assertRaises(ValueError):
                    verify_release.extract_archive(archive, directory / "unpacked", "linux")
                self.assertFalse((directory / "escape").exists())

    def test_zip_requires_complete_members(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            archive = directory / "incomplete.zip"
            with zipfile.ZipFile(archive, "w") as output:
                output.writestr("go-inject.exe", b"binary")
            with self.assertRaises(ValueError):
                verify_release.extract_archive(archive, directory / "unpacked", "windows")

    def test_checksum_manifest_rejects_duplicates_and_paths(self) -> None:
        checksum = hashlib.sha256(b"archive").hexdigest()
        for text in (f"{checksum}  archive.zip\n{checksum}  archive.zip\n", f"{checksum}  ../archive.zip\n", "invalid\n"):
            with self.subTest(text=text), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                (directory / "SHA256SUMS").write_text(text, encoding="utf-8")
                with self.assertRaises(ValueError):
                    verify_release.checksums(directory)


if __name__ == "__main__":
    unittest.main()
