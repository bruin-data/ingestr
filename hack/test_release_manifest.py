import copy
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
import zipfile

import release_manifest as contract

TAG = "v1.2.3"
SHA = "a" * 40
SCRIPT = Path(__file__).with_name("release_manifest.py")


class ReleaseContractTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        for (goos, _), name in contract.PLATFORMS.items():
            self.archive(name, "ingestr.exe" if goos == "windows" else "ingestr")

    def archive(self, name, member, payload=b"packaged executable", duplicate=False, link=False):
        path = self.directory / name
        if name.endswith(".zip"):
            with zipfile.ZipFile(path, "w") as out:
                out.writestr(member, payload)
        else:
            with tarfile.open(path, "w:gz") as out:
                item = tarfile.TarInfo(member)
                item.size = len(payload)
                if link:
                    item.type = tarfile.SYMTYPE
                    item.linkname = "elsewhere"
                out.addfile(item, io.BytesIO(payload))
                if duplicate:
                    out.addfile(item, io.BytesIO(payload))
        return path

    def run_cli(self, command, tag=TAG):
        return subprocess.run([sys.executable, str(SCRIPT), command, "--directory", str(self.directory),
                               "--tag", tag, "--commit", SHA], capture_output=True)

    def test_all_platforms_and_determinism(self):
        data = contract.build(self.directory, TAG, SHA)
        self.assertEqual(len(data["platforms"]), 5)
        for entry in data["platforms"]:
            self.assertEqual(entry["executable"]["sha256"], contract.digest(b"packaged executable")["sha256"])
        self.assertEqual(self.run_cli("build").returncode, 0)
        first = (self.directory / contract.MANIFEST).read_bytes()
        self.assertEqual(self.run_cli("build").returncode, 0)
        self.assertEqual(first, (self.directory / contract.MANIFEST).read_bytes())
        self.assertEqual(self.run_cli("check").returncode, 0)
        checksums = (self.directory / "ingestr_1.2.3_checksums.txt").read_text()
        self.assertEqual(len(checksums.splitlines()), 5)
        self.assertIn("ingestr_Windows_x86_64.zip", checksums)

    def test_missing_windows_and_extra_archive(self):
        windows = self.directory / contract.PLATFORMS[("windows", "amd64")]
        windows.unlink()
        with self.assertRaises(ValueError):
            contract.build(self.directory, TAG, SHA)
        self.archive(windows.name, "ingestr.exe")
        self.archive("unexpected.zip", "ingestr.exe")
        with self.assertRaises(ValueError):
            contract.build(self.directory, TAG, SHA)

    def test_invalid_identity(self):
        for tag in ("1.2.3", "v01.2.3", "v1.2", "v1.2.3-rc.1", "v1.2.3+build", "v1.2.3\n"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                contract.identity(tag, SHA)
        for sha in ("abc123", "A" * 40, "g" * 40):
            with self.subTest(sha=sha), self.assertRaises(ValueError):
                contract.identity(TAG, sha)

    def test_invalid_fields(self):
        self.assertEqual(self.run_cli("build").returncode, 0)
        valid = contract.build(self.directory, TAG, SHA)
        mutations = [
            lambda d: d.update(repository="attacker/ingestr"),
            lambda d: d.update(version="1.2.4"),
            lambda d: d.update(tag="v1.2.4"),
            lambda d: d.update(source_commit="b" * 40),
            lambda d: d.update(schema_version=True),
            lambda d: d.update(schema_version=1.0),
            lambda d: d.update(extra=1),
            lambda d: d["platforms"].pop(),
            lambda d: d["platforms"].__setitem__(4, d["platforms"][0]),
            lambda d: d["platforms"][0]["archive"].update(sha256="bad"),
            lambda d: d["platforms"][0]["archive"].update(size=True),
            lambda d: d["platforms"][0]["executable"].update(size=0),
            lambda d: d["platforms"][0]["executable"].update(path="../ingestr"),
            lambda d: d["platforms"][0]["archive"].update(format="zip"),
            lambda d: d["platforms"][0]["archive"].update(name="other.tar.gz"),
        ]
        for mutation in mutations:
            data = copy.deepcopy(valid)
            mutation(data)
            (self.directory / contract.MANIFEST).write_text(json.dumps(data))
            with self.subTest(data=data):
                self.assertNotEqual(self.run_cli("check").returncode, 0)

    def test_duplicate_json_fields(self):
        for text in ('{"tag":"v1.2.3","tag":"v1.2.3"}', '{"archive":{"size":1,"size":2}}'):
            with self.assertRaises(ValueError):
                json.loads(text, object_pairs_hook=contract.unique_object)

    def test_tampering_and_same_commit_different_tag(self):
        self.assertEqual(self.run_cli("build").returncode, 0)
        self.assertNotEqual(self.run_cli("check", tag="v1.2.4").returncode, 0)
        path = self.directory / contract.MANIFEST
        data = json.loads(path.read_text())
        data["platforms"][0]["executable"]["sha256"] = "0" * 64
        path.write_text(json.dumps(data))
        self.assertNotEqual(self.run_cli("check").returncode, 0)
        self.assertEqual(self.run_cli("build").returncode, 0)
        (self.directory / "ingestr_1.2.3_checksums.txt").write_text("wrong checksums")
        self.assertNotEqual(self.run_cli("check").returncode, 0)
        self.assertEqual(self.run_cli("build").returncode, 0)
        self.archive(contract.PLATFORMS[("windows", "amd64")], "ingestr.exe", b"tampered")
        self.assertNotEqual(self.run_cli("check").returncode, 0)

    def test_archive_member_safety(self):
        name = contract.PLATFORMS[("linux", "amd64")]
        for member, duplicate, link in [("../ingestr", False, False), ("ingestr", True, False),
                                        ("ingestr", False, True), ("other", False, False)]:
            self.archive(name, member, duplicate=duplicate, link=link)
            with self.subTest(member=member, duplicate=duplicate, link=link), self.assertRaises(ValueError):
                contract.build(self.directory, TAG, SHA)

    def test_zip_executable_must_be_regular(self):
        for mode in (0o120777, 0o040755):
            path = self.directory / contract.PLATFORMS[("windows", "amd64")]
            with zipfile.ZipFile(path, "w") as out:
                item = zipfile.ZipInfo("ingestr.exe")
                item.create_system = 3
                item.external_attr = mode << 16
                out.writestr(item, b"not a regular executable")
            with self.subTest(mode=mode), self.assertRaises(ValueError):
                contract.build(self.directory, TAG, SHA)

    @unittest.skipIf(sys.platform == "win32", "shell fixture requires Unix")
    def test_smoke_requires_exact_version(self):
        name = contract.PLATFORMS[("linux", "amd64")]
        for version, success in [(TAG, True), ("v1.2.4", False)]:
            self.archive(name, "ingestr", f"#!/bin/sh\necho 'ingestr version {version}'\n".encode())
            result = subprocess.run([sys.executable, str(SCRIPT), "smoke", "--directory", str(self.directory),
                                     "--tag", TAG, "--commit", SHA, "--goos", "linux", "--goarch", "amd64"],
                                    capture_output=True)
            self.assertEqual(result.returncode == 0, success)


if __name__ == "__main__":
    unittest.main()
