#!/usr/bin/env python3
"""Identify only the parent chain and SSH transport of this multiplexed probe."""
import json
import os
from pathlib import Path

client = os.environ.get('SSH_CLIENT', '').split()
if len(client) < 2:
    raise SystemExit('SSH_CLIENT unavailable; cannot identify transport')
peer_port = int(client[1])
ssh_inodes = set()
for path in ('/proc/net/tcp', '/proc/net/tcp6'):
    for line in Path(path).read_text().splitlines()[1:]:
        fields = line.split()
        if fields[3] == '01' and int(fields[2].split(':')[1], 16) == peer_port:
            ssh_inodes.add(fields[9])

chain = []
pid = os.getpid()
while pid > 1:
    p = Path('/proc') / str(pid)
    try:
        stat = (p / 'stat').read_text().rsplit(')', 1)[1].split()
        comm = (p / 'comm').read_text().strip()
        status = (p / 'status').read_text().splitlines()
        uid = next(x for x in status if x.startswith('Uid:')).split()[1]
        limit = next(x for x in (p / 'limits').read_text().splitlines() if x.startswith('Max open files'))
        entries = list((p / 'fd').iterdir())
        sockets = set()
        for fd in entries:
            try:
                s = os.readlink(fd)
                if s.startswith('socket:['):
                    sockets.add(s[8:-1])
            except OSError:
                pass
        chain.append({'pid': pid, 'ppid': int(stat[1]), 'start_ticks': int(stat[19]),
                      'comm': comm, 'uid': int(uid), 'fd_count': len(entries),
                      'nofile_line': limit, 'owns_ssh_transport': bool(sockets & ssh_inodes),
                      'transport_inodes': sorted(sockets & ssh_inodes)})
        pid = int(stat[1])
    except (OSError, ValueError, StopIteration):
        break
print(json.dumps({'remote_peer_port': peer_port, 'ssh_transport_inodes': sorted(ssh_inodes), 'chain': chain}))
