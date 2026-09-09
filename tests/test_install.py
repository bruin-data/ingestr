"""Offline installer regressions. Run explicitly with python3 tests/test_install.py."""

import hashlib
import io
import os
from pathlib import Path
import pty
import shutil
import subprocess
import tarfile
import tempfile
import time
import unittest
import zipfile


INSTALLER = Path(__file__).resolve().parents[1] / "install.sh"
BINARY = b"#!/bin/sh\necho executed > \"$ROOT/executed\"\n"


class InstallerTest(unittest.TestCase):
    def run_install(self, *, platform="Linux", tty=False, digest="correct",
                    tags=("v1.2.3",), tool="sha256sum", failure="", downloader="curl",
                    machine="x86_64", cancel_install=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tools = root / "tools"
            tools.mkdir()
            dest = root / "destination"
            dest.mkdir()
            tmp = root / "tmp"
            tmp.mkdir()
            windows = platform == "MINGW64_NT"
            binary = "ingestr.exe" if windows else "ingestr"
            (dest / binary).write_bytes(b"existing installation")
            archive = root / "archive"
            if windows:
                with zipfile.ZipFile(archive, "w") as output:
                    output.writestr(binary, BINARY)
            else:
                with tarfile.open(archive, "w:gz") as output:
                    entry = tarfile.TarInfo(binary)
                    entry.size = len(BINARY)
                    entry.mode = 0o755
                    output.addfile(entry, io.BytesIO(BINARY))
            expected = hashlib.sha256(archive.read_bytes()).hexdigest()

            def stub(name, body):
                path = tools / name
                path.write_text("#!/bin/sh\nset -e\n" + body + "\n")
                path.chmod(0o755)

            for name in ("cat", "tr", "grep", "sed", "cut", "mktemp", "rm", "cp",
                         "install", "basename", "tail", "awk", "wc", "sleep", "gzip"):
                path = shutil.which(name)
                self.assertIsNotNone(path, name)
                (tools / name).symlink_to(path)
            stub("uname", 'if [ "$1" = -s ]; then echo "$PLATFORM"; else echo "$MACHINE"; fi')
            stub("tput", "echo 0")
            stub(downloader, '''
echo "$*" >> "$ROOT/requests"
case "$*" in *-sLI*) echo 'content-length: 0'; exit 0 ;; esac
while [ "$#" -gt 0 ]; do
  case "$1" in -o|-O) output=$2; shift ;; esac
  url=$1
  shift
done
case "$url" in
  */releases/download/*) cp "$ROOT/archive" "$output" ;;
  *) printf '{"tag_name":"v1.2.3"}' > "$output" ;;
esac
if [ "$FAILURE" = download ]; then exit 22; fi
''')
            extractor = "unzip" if windows else "tar"
            real_extractor = shutil.which(extractor)
            self.assertIsNotNone(real_extractor)
            stub(extractor, f'echo extracted > "$ROOT/extracted"\nexec "{real_extractor}" "$@"')
            if cancel_install:
                (tools / "install").unlink()
                stub("install", f'''
echo started > "$ROOT/install-started"
sleep 1
"{shutil.which('install')}" "$@"
echo finished > "$ROOT/install-finished"
''')
            if tool != "missing":
                if failure == "tool":
                    stub(tool, f'echo "{expected}  archive"; exit 1')
                elif failure == "output":
                    stub(tool, "echo invalid-digest")
                else:
                    real_tool = shutil.which("sha256sum" if tool == "gsha256sum" else tool)
                    self.assertIsNotNone(real_tool, tool)
                    (tools / tool).symlink_to(real_tool)

            args = ["-b", str(dest)]
            if digest is not None:
                value = expected.upper() if digest == "correct" else digest
                args += ["-s", value]
            args += list(tags)
            env = dict(os.environ, PATH=str(tools), ROOT=str(root), TMPDIR=str(tmp),
                       HOME=str(root), SHELL="bruin-installer", PLATFORM=platform,
                       FAILURE=failure, TERM="xterm", MACHINE=machine)
            master = slave = None
            try:
                if tty:
                    master, slave = pty.openpty()
                with tempfile.TemporaryFile() as output:
                    with subprocess.Popen(
                        ["/bin/sh", "-s", "--", *args], stdin=subprocess.PIPE,
                        stdout=slave if tty else output, stderr=output, env=env,
                    ) as process:
                        if cancel_install:
                            process.stdin.write(INSTALLER.read_bytes())
                            process.stdin.close()
                            process.stdin = None
                            deadline = time.monotonic() + 10
                            while not (root / "install-started").exists():
                                if process.poll() is not None or time.monotonic() > deadline:
                                    self.fail("installer did not reach destination write")
                                time.sleep(0.01)
                            process.terminate()
                            process.communicate(timeout=15)
                            self.assertTrue((root / "install-finished").exists(),
                                            "installer exited before its child finished writing")
                        else:
                            process.communicate(INSTALLER.read_bytes(), timeout=15)
                        returncode = process.returncode
                    output.seek(0)
                    diagnostics = output.read().decode(errors="replace")
            finally:
                if slave is not None:
                    os.close(slave)
                    os.close(master)
            success = digest in ("correct", None) and not failure and tool != "missing"
            success = success and (digest is None or tags == ("v1.2.3",))
            self.assertEqual(returncode == 0, success and not cancel_install, diagnostics)
            self.assertEqual((dest / binary).read_bytes(), BINARY if success else b"existing installation")
            self.assertEqual((root / "extracted").exists(), success, diagnostics)
            self.assertFalse((root / "executed").exists())
            self.assertEqual(list(tmp.iterdir()), [], diagnostics)
            requests = (root / "requests").read_text() if (root / "requests").exists() else ""
            if digest is not None:
                archive_os = "Windows" if windows else platform
                archive_arch = {"x86_64": "x86_64", "aarch64": "arm64", "i686": "i386"}[machine]
                archive_suffix = ".zip" if windows else ".tar.gz"
                for request in requests.splitlines():
                    self.assertIn(f"/releases/download/v1.2.3/ingestr_{archive_os}_{archive_arch}{archive_suffix}", request)

    def test_verification_before_extraction_in_both_output_paths(self):
        for platform, tty in (("Linux", False), ("Linux", True), ("Darwin", False),
                              ("Darwin", True), ("MINGW64_NT", False)):
            for failure in ("", "download", "tool", "output"):
                with self.subTest(platform=platform, tty=tty, failure=failure):
                    self.run_install(platform=platform, tty=tty, failure=failure)
            for digest, tool in (("0" * 64, "sha256sum"), ("correct", "missing"),
                                 (hashlib.sha256(BINARY).hexdigest(), "sha256sum")):
                with self.subTest(platform=platform, tty=tty, digest=digest, tool=tool):
                    self.run_install(platform=platform, tty=tty, digest=digest, tool=tool)

    def test_strict_inputs(self):
        for digest in ("", "a" * 63, "a" * 65, "g" * 64, " " + "a" * 63,
                       "a" * 63 + "\n", "sha256:" + "a" * 64):
            with self.subTest(digest=digest):
                self.run_install(digest=digest)
        for tags in ((), ("latest",), ("1.2.3",), ("v1.2",), ("v1.2.3-rc1",),
                     ("v1.2.3", "extra"), ("v1.2.3\nlatest",)):
            with self.subTest(tags=tags):
                self.run_install(tags=tags)

    def test_system_hash_fallbacks_and_wget(self):
        for tool in ("gsha256sum", "sha256sum", "shasum", "openssl"):
            for downloader in ("curl", "wget"):
                with self.subTest(tool=tool, downloader=downloader):
                    self.run_install(tool=tool, downloader=downloader)
        self.run_install(downloader="wget", failure="download")

    def test_other_supported_architectures(self):
        for platform, machine in (("Linux", "aarch64"), ("Darwin", "aarch64")):
            with self.subTest(platform=platform, machine=machine):
                self.run_install(platform=platform, machine=machine)

    def test_cancellation_waits_for_destination_write(self):
        self.run_install(tty=True, cancel_install=True)

    def test_legacy_without_hash(self):
        for tags in ((), ("v1.2.3",)):
            with self.subTest(tags=tags):
                self.run_install(digest=None, tags=tags)


if __name__ == "__main__":
    unittest.main()
