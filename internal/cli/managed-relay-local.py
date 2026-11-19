"""Fixed local relay configuration operations; never open Controller credentials."""
import json
import os
import stat
import sys

ROOT = "/etc/nibfin"
LOCK = ROOT + "/notification-relay.cli-lock"
CONFIG = ROOT + "/notification-relay.json"
HOST = ROOT + "/host.json"
LIMIT = 262144
STATE = "/var/lib/nibfin/notification-relay"
UNITS = "/etc/systemd/system"


def trusted(path):
    current = path
    while True:
        info = os.lstat(current)
        if (info.st_uid != 0 or stat.S_ISLNK(info.st_mode)
                or info.st_mode & 0o022):
            raise ValueError("untrusted")
        if current == "/":
            return
        current = os.path.dirname(current)


def read(path):
    trusted(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise ValueError("unsafe file")
        data = stream.read(LIMIT + 1)
        if len(data) > LIMIT:
            raise ValueError("too large")
        return json.loads(data)


def bindings():
    host = read(HOST)
    paths = host.get("wakePeerFiles", [])
    if not isinstance(paths, list) or len(paths) > 10:
        raise ValueError("peers")
    import re
    if any(not isinstance(p, str) or not re.fullmatch(
            r"/etc/nibfin/host\.[A-Za-z0-9_-]+\.json", p) for p in paths):
        raise ValueError("peer path")
    records = [host] + [read(path) for path in paths]
    ids = host["sharedRunner"]["repositories"]
    result = []
    for identity in ids:
        matches = [r for r in records if
                   r["repository"]["approval"]["repositoryId"] == identity]
        if len(matches) != 1:
            raise ValueError("adoption")
        record = matches[0]
        if (record["memberId"] != host["memberId"]
                or record["installation"] != host["installation"]):
            raise ValueError("binding")
        approval = record["repository"]["approval"]
        event = record["repository"]["eventRunner"]
        selected = {key: event[key] for key in (
            "repository", "repositoryId", "memberId", "workflowId",
            "workflowPath", "sourceCommit", "workflowBlob")}
        selected["reusable"] = {key: event["reusable"][key] for key in (
            "repository", "path", "commit")}
        result.append({"repository": approval["repository"],
                       "repositoryId": identity, "event": selected})
    return {"memberId": host["memberId"],
            "pin": host["installation"]["pin"], "origins": result}


def snapshot():
    try:
        config = read(CONFIG)
    except FileNotFoundError:
        config = None
        residual = [STATE, UNITS + "/nibfin-notification-relay.service",
                    UNITS + "/nibfin-notification-relay.timer",
                    UNITS + "/timers.target.wants/nibfin-notification-relay.timer"]
        if any(os.path.lexists(path) for path in residual):
            raise ValueError("relay requires inspection before new configuration")
    return {"bindings": bindings(), "config": config}


def execute(request):
    trusted(ROOT)
    operation = request["operation"]
    if operation == "bindings":
        return {"bindings": bindings()}
    if operation == "lock":
        os.mkdir(LOCK, 0o700)
        return {"status": "locked"}
    trusted(LOCK)
    if not stat.S_ISDIR(os.lstat(LOCK).st_mode):
        raise ValueError("lock")
    if operation == "read":
        return snapshot()
    if operation == "unlock":
        os.rmdir(LOCK)
        return {"status": "unlocked"}
    if operation != "write" or snapshot() != request["expected"]:
        raise ValueError("changed")
    data = (json.dumps(request["config"], separators=(",", ":")) + "\n").encode()
    if len(data) > LIMIT:
        raise ValueError("size")
    temp = LOCK + "/config"
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        if snapshot() != request["expected"]:
            raise ValueError("changed")
        os.replace(temp, CONFIG)
        directory = os.open(ROOT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        if read(CONFIG) != request["config"]:
            raise ValueError("readback")
        return {"status": "written"}
    finally:
        if os.path.exists(temp):
            os.unlink(temp)


try:
    raw = sys.stdin.buffer.read(LIMIT + 1)
    if len(raw) > LIMIT:
        raise ValueError("input")
    print(json.dumps(execute(json.loads(raw))))
except Exception:
    print('{"status":"pending","code":"MANAGED_RELAY_LOCAL_READBACK_REQUIRED"}')
    sys.exit(1)
