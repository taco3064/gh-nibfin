"""Install one verified Core archive without importing any of its code as root."""

import fcntl
import hashlib
import io
import os
import pathlib
import re
import shutil
import stat
import sys
import tarfile
import tempfile


def trusted(path):
    for node in (path, *path.parents):
        info = node.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise ValueError("Untrusted installation directory")


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


def install_runtime(archive, expected, binary_hash):
    if len(archive) > 64 * 1024 * 1024 or hashlib.sha256(archive).hexdigest() != expected:
        raise ValueError("Node archive digest differs")
    parent = pathlib.Path("/opt/nibfin/tools")
    trusted(parent.parent.parent)
    parent.parent.mkdir(mode=0o755, exist_ok=True)
    trusted(parent.parent)
    parent.mkdir(mode=0o755, exist_ok=True)
    trusted(parent)
    name = "node-v24.22.1-linux-x64"
    target = parent / name
    if os.path.lexists(target):
        trusted(target / "bin")
        return "existing"
    stage = pathlib.Path(tempfile.mkdtemp(prefix=".node-install-", dir=parent))
    try:
        (stage / "bin").mkdir()
        with tarfile.open(fileobj=io.BytesIO(archive), mode="r:gz") as package:
            entry = package.getmember(name + "/bin/node")
            if not entry.isfile() or entry.size > 144 * 1024 * 1024:
                raise ValueError("Invalid Node executable")
            path = stage / "bin/node"
            with package.extractfile(entry) as source, path.open("xb+") as output:
                shutil.copyfileobj(source, output)
                output.flush()
                output.seek(0)
                if hashlib.file_digest(output, "sha256").hexdigest() != binary_hash:
                    raise ValueError("Node executable digest differs")
                os.fchmod(output.fileno(), 0o755)
                os.fsync(output.fileno())
        stage.chmod(0o755)
        os.rename(stage, target)
        return "installed"
    finally:
        if stage.exists():
            shutil.rmtree(stage)


if __name__ == "__main__":
    if os.geteuid() != 0:
        raise SystemExit("Host administrator required")
    os.umask(0o022)
    if len(sys.argv) == 4 and sys.argv[1] == "node":
        data = sys.stdin.buffer.read(64 * 1024 * 1024 + 1)
        print(install_runtime(data, sys.argv[2], sys.argv[3]))
    elif len(sys.argv) == 2:
        data = sys.stdin.buffer.read(32 * 1024 * 1024 + 1)
        print(install(data, sys.argv[1], pathlib.Path("/opt/nibfin")))
    else:
        raise SystemExit("Invalid installation request")
