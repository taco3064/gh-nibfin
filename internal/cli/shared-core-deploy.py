"""Install one verified Core archive without importing any of its code as root."""

import fcntl
import hashlib
import io
import os
import pathlib
import re
import shutil
import stat
import struct
import sys
import tarfile
import tempfile


def trusted(path):
    for node in (path, *path.parents):
        info = node.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise ValueError("Untrusted installation directory")


def extract_core(archive, stage):
    with tarfile.open(fileobj=io.BytesIO(archive), mode="r:gz") as package:
        count, length = 0, 0
        for entry in package:
            count += 1
            length += entry.size
            parts = entry.name.split("/")
            if (count > 1000 or length > 40 * 1024 * 1024 or not entry.isfile()
                    or not re.fullmatch(r"[A-Za-z0-9._/-]+", entry.name)
                    or any(part in ("", ".", "..", ".git") for part in parts)
                    or entry.mode not in (0o644, 0o755)):
                raise ValueError("Invalid Core archive entry")
            path = stage.joinpath(*parts)
            path.parent.mkdir(parents=True, exist_ok=True)
            with package.extractfile(entry) as source, path.open("xb") as output:
                shutil.copyfileobj(source, output)
                os.fchmod(output.fileno(), entry.mode)
                output.flush()
                os.fsync(output.fileno())


def install(archive, expected, parent):
    if not re.fullmatch(r"[0-9a-f]{64}", expected):
        raise ValueError("Invalid Core digest")
    if len(archive) > 32 * 1024 * 1024 or hashlib.sha256(archive).hexdigest() != expected:
        raise ValueError("Core archive digest differs")
    trusted(parent.parent)
    parent.mkdir(mode=0o755, exist_ok=True)
    trusted(parent)
    lock = os.open(parent / ".core-install.lock",
                   os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(lock, "rb+") as handle:
        info = os.fstat(handle.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o077:
            raise ValueError("Untrusted Core installation lock")
        fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        target = parent / "core"
        if os.path.lexists(target):
            trusted(target)
            return "existing"
        stage = pathlib.Path(tempfile.mkdtemp(prefix=".core-install-", dir=parent))
        try:
            extract_core(archive, stage)
            stage.chmod(0o755)
            os.rename(stage, target)
            directory = os.open(parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
            return "installed"
        finally:
            if stage.exists():
                shutil.rmtree(stage)


def verify_node(path, digest, machine):
    flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
    with os.fdopen(os.open(path, flags), "rb") as source:
        info = os.fstat(source.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1
                or not 0 < info.st_size <= 256 * 1024 * 1024
                or not info.st_mode & 0o111 or info.st_uid != 0
                or info.st_mode & 0o022):
            raise ValueError("Invalid Node executable")
        header = source.read(64)
        if (len(header) != 64 or header[:7] != b"\x7fELF\x02\x01\x01"
                or struct.unpack_from("<H", header, 16)[0] not in (2, 3)
                or struct.unpack_from("<I", header, 20)[0] != 1
                or struct.unpack_from("<H", header, 18)[0] != machine):
            raise ValueError("Node executable architecture differs")
        source.seek(0)
        if hashlib.file_digest(source, "sha256").hexdigest() != digest:
            raise ValueError("Node executable digest differs")


def extract_node(archive, stage, name, binary_hash, machine):
    (stage / "bin").mkdir()
    path = stage / "bin/node"
    matches, count, length = 0, 0, 0
    with tarfile.open(fileobj=io.BytesIO(archive), mode="r|xz") as package:
        for entry in package:
            count += 1
            length += entry.size
            if count > 30000 or entry.size < 0 or length > 1024 * 1024 * 1024:
                raise ValueError("Node archive size limit")
            if entry.name != name + "/bin/node":
                continue
            matches += 1
            if matches != 1 or not entry.isfile() or entry.size > 256 * 1024 * 1024:
                raise ValueError("Invalid Node executable member")
            with package.extractfile(entry) as source, path.open("xb") as output:
                shutil.copyfileobj(source, output)
                os.fchmod(output.fileno(), 0o755)
                output.flush()
                os.fsync(output.fileno())
    if matches != 1:
        raise ValueError("Node executable member missing")
    verify_node(path, binary_hash, machine)


def install_runtime(archive, expected, binary_hash, name, arch):
    if (not all(re.fullmatch(r"[0-9a-f]{64}", value)
                for value in (expected, binary_hash))
            or arch not in ("amd64", "arm64")):
        raise ValueError("Invalid Node identity")
    artifact = "x64" if arch == "amd64" else "arm64"
    if not re.fullmatch(r"node-v[0-9]+\.[0-9]+\.[0-9]+-linux-" + artifact, name):
        raise ValueError("Invalid Node target")
    machine = 62 if arch == "amd64" else 183
    if len(archive) > 64 * 1024 * 1024 or hashlib.sha256(archive).hexdigest() != expected:
        raise ValueError("Node archive digest differs")
    parent = pathlib.Path("/opt/nibfin/tools")
    trusted(parent.parent.parent)
    parent.parent.mkdir(mode=0o755, exist_ok=True)
    trusted(parent.parent)
    parent.mkdir(mode=0o755, exist_ok=True)
    trusted(parent)
    lock = os.open(parent / ".node-install.lock",
                   os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    with os.fdopen(lock, "rb+") as handle:
        info = os.fstat(handle.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0
                or info.st_mode & 0o077 or info.st_nlink != 1):
            raise ValueError("Untrusted Node installation lock")
        fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        target = parent / name
        if os.path.lexists(target):
            trusted(target / "bin")
            verify_node(target / "bin/node", binary_hash, machine)
            return "existing"
        stage = pathlib.Path(tempfile.mkdtemp(prefix=".node-install-", dir=parent))
        try:
            extract_node(archive, stage, name, binary_hash, machine)
            stage.chmod(0o755)
            os.rename(stage, target)
            directory = os.open(parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
            return "installed"
        finally:
            if stage.exists():
                shutil.rmtree(stage)


# CORE_MAINTENANCE_EXTENSION

if __name__ == "__main__":
    if os.geteuid() != 0:
        raise SystemExit("Host administrator required")
    os.umask(0o022)
    if len(sys.argv) == 3 and sys.argv[1] == "bootstrap":
        bootstrap_maintenance(sys.stdin.buffer.read(32 * 1024 * 1024 + 1), sys.argv[2])
    elif len(sys.argv) == 6 and sys.argv[1] == "node":
        data = sys.stdin.buffer.read(64 * 1024 * 1024 + 1)
        print(install_runtime(data, sys.argv[2], sys.argv[3],
                              sys.argv[4], sys.argv[5]))
    elif len(sys.argv) == 2:
        data = sys.stdin.buffer.read(32 * 1024 * 1024 + 1)
        print(install(data, sys.argv[1], pathlib.Path("/opt/nibfin")))
    else:
        raise SystemExit("Invalid installation request")
