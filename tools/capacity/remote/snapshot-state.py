#!/usr/bin/env python3
"""Read-only handle/WAL check for the single task-owned stopped database."""
import json
import os
import stat
import sys
from pathlib import Path

if len(sys.argv) != 2 or not Path(sys.argv[1]).is_absolute():
    raise SystemExit('usage: snapshot-state.py ABSOLUTE_TASK_ROOT')
root = Path(sys.argv[1]).resolve()
db = root/'data'/'ranking.sqlite'
files = {}
for path in (db, Path(str(db)+'-wal'), Path(str(db)+'-shm')):
    try:
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode):
            raise SystemExit('task DB and sidecars must be regular files: '+str(path))
        files[str(path)] = {'size': info.st_size, 'mtime_ns': info.st_mtime_ns, 'inode': info.st_ino}
    except FileNotFoundError:
        files[str(path)] = None
if not files[str(db)]:
    raise SystemExit('task database missing')
if files[str(db)+'-wal'] and files[str(db)+'-wal']['size'] != 0:
    raise SystemExit('task database has a nonempty WAL')
opened = []
for proc in Path('/proc').iterdir():
    if not proc.name.isdigit():
        continue
    try:
        fds = list((proc/'fd').iterdir())
    except (FileNotFoundError, ProcessLookupError):
        continue
    except PermissionError as exc:
        raise SystemExit('cannot prove task database handles are closed: '+str(exc))
    for fd in fds:
        try:
            target = os.readlink(fd)
        except (FileNotFoundError, ProcessLookupError):
            continue
        except PermissionError as exc:
            raise SystemExit('cannot inspect a process handle: '+str(exc))
        if target == str(db) or target.startswith(str(db)+'-') or target.startswith(str(db)+' (deleted)'):
            opened.append([proc.name, fd.name, target])
if opened:
    raise SystemExit('task database handles still open: '+json.dumps(opened))
print(json.dumps({'root': str(root), 'database': str(db), 'files': files, 'open_handles': []}, sort_keys=True))
