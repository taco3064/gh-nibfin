# Credential-free WSL bootstrap. No shell, login, installation or private input.
import json
import os
from pathlib import Path
import stat


def trusted(path):
    path = path.resolve(strict=True)
    for directory in ('/tmp', '/var/tmp', os.environ.get('TMPDIR') or '/tmp'):
        temporary = Path(directory).resolve()
        if path == temporary or temporary in path.parents:
            raise ValueError('temporary entry')
    for item in (path, *path.parents):
        info = item.stat()
        if info.st_uid not in (0, os.getuid()) or info.st_mode & 0o022:
            raise ValueError('unsafe ownership or permissions')
        if item.is_dir():
            try:
                (item / '.git').lstat()
            except FileNotFoundError:
                pass
            else:
                raise ValueError('repository entry')
    info = path.stat()
    if not stat.S_ISREG(info.st_mode) or not info.st_mode & 0o111:
        raise ValueError('not executable')
    return path


def main():
    # This is gh's Linux extension data location, not PATH or the current repo.
    data = os.environ.get('XDG_DATA_HOME')
    root = Path(data) if data else Path.home() / '.local/share'
    if not root.is_absolute():
        raise ValueError('relative data directory')
    entry = trusted(root / 'gh/extensions/gh-nibfin/gh-nibfin')
    print(json.dumps({'entry': str(entry)}))


if __name__ == '__main__':
    try:
        main()
    except Exception:
        raise SystemExit(1)
