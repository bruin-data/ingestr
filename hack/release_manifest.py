"""Release contract v1: hash final archives, validate strictly, smoke-test natively."""

import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess
import tarfile
import tempfile
import zipfile

REPOSITORY = "bruin-data/ingestr"
PLATFORMS = {
    ("darwin", "amd64"): "ingestr_Darwin_x86_64.tar.gz",
    ("darwin", "arm64"): "ingestr_Darwin_arm64.tar.gz",
    ("linux", "amd64"): "ingestr_Linux_x86_64.tar.gz",
    ("linux", "arm64"): "ingestr_Linux_arm64.tar.gz",
    ("windows", "amd64"): "ingestr_Windows_x86_64.zip",
}
MANIFEST = "ingestr-manifest.v1.json"


def require(condition, message):
    if not condition:
        raise ValueError(message)


def identity(tag, commit):
    require(re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag),
            "tag must be canonical stable vMAJOR.MINOR.PATCH")
    require(re.fullmatch(r"[0-9a-f]{40}", commit), "source_commit must be a full lowercase SHA")


def digest(data):
    return {"size": len(data), "sha256": hashlib.sha256(data).hexdigest()}


def executable(archive, member):
    seen = set()

    def check(name):
        require(name and "\\" not in name and not name.startswith("/"), "unsafe member")
        require(all(p not in ("", ".", "..") for p in name.rstrip("/").split("/")), "unsafe member")
        require(not PurePosixPath(name).is_absolute() and ":" not in name, "unsafe member")
        require(name not in seen, "duplicate archive member")
        seen.add(name)

    found = None
    if archive.name.endswith(".zip"):
        with zipfile.ZipFile(archive) as src:
            for item in src.infolist():
                check(item.filename)
                mode = item.external_attr >> 16
                require(stat.S_IFMT(mode) in (0, stat.S_IFREG, stat.S_IFDIR), "special ZIP member")
                if item.filename == member:
                    require(not item.is_dir() and stat.S_IFMT(mode) in (0, stat.S_IFREG), "executable is not a file")
                    found = src.read(item)
    else:
        with tarfile.open(archive, "r:gz") as src:
            for item in src:
                check(item.name)
                require(item.isfile() or item.isdir(), "special TAR member")
                if item.name == member:
                    require(item.isfile(), "executable is not a file")
                    found = src.extractfile(item).read()
    require(found, "missing or empty executable")
    return found


def build(directory, tag, commit):
    identity(tag, commit)
    archives = {p.name for p in directory.iterdir() if p.name.endswith((".tar.gz", ".zip"))}
    require(archives == set(PLATFORMS.values()), "expected exactly all five platform archives")
    entries = []
    for (goos, goarch), name in PLATFORMS.items():
        path = directory / name
        require(path.is_file() and not path.is_symlink(), "archive must be a regular file")
        member = "ingestr.exe" if goos == "windows" else "ingestr"
        entries.append({
            "goos": goos, "goarch": goarch,
            "archive": {"name": name, "format": "zip" if goos == "windows" else "tar.gz",
                        **digest(path.read_bytes())},
            "executable": {"path": member, **digest(executable(path, member))},
        })
    return {"schema_version": 1, "repository": REPOSITORY, "tag": tag,
            "version": tag[1:], "source_commit": commit, "platforms": entries}


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON field: " + key)
        result[key] = value
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["identity", "build", "check", "smoke"])
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--directory", type=Path, default=Path("release-assets"))
    parser.add_argument("--goos")
    parser.add_argument("--goarch")
    args = parser.parse_args()
    identity(args.tag, args.commit)
    if args.command == "identity":
        return
    if args.command == "smoke":
        member = "ingestr.exe" if args.goos == "windows" else "ingestr"
        payload = executable(args.directory / PLATFORMS[(args.goos, args.goarch)], member)
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / member
            path.write_bytes(payload)
            path.chmod(0o755)
            result = subprocess.check_output([str(path), "--version"], text=True).strip()
            require(result == "ingestr version " + args.tag, "unexpected version output: " + result)
        return
    expected = build(args.directory, args.tag, args.commit)
    manifest = args.directory / MANIFEST
    checksums = "".join(f'{p["archive"]["sha256"]}  {p["archive"]["name"]}\n' for p in expected["platforms"])
    checksum_path = args.directory / f"ingestr_{args.tag[1:]}_checksums.txt"
    if args.command == "build":
        manifest.write_text(json.dumps(expected, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        checksum_path.write_text(checksums, encoding="utf-8")
    else:
        actual = json.loads(manifest.read_bytes(), object_pairs_hook=unique_object)
        # Compare JSON, not Python values (where True == 1), to enforce field types too.
        require(json.dumps(actual, sort_keys=True) == json.dumps(expected, sort_keys=True),
                "manifest does not match actual artifacts")
        require(checksum_path.read_text(encoding="utf-8") == checksums, "checksums do not match actual artifacts")


if __name__ == "__main__":
    main()
