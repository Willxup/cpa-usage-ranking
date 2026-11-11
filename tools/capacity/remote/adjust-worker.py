#!/usr/bin/env python3
"""One authorized soft-limit change on the identified forwarding worker only."""
import json
import os
import resource
import select
import sys
from pathlib import Path

ancestry = json.loads(sys.argv[1])
chain = ancestry['chain']
tracks = [x for x in chain if x['owns_ssh_transport']]
if len(tracks) != 2 or chain[1] != tracks[0] or chain[2] != tracks[1]:
    raise SystemExit('unexpected forwarding ancestry')
worker, parent = tracks
listener = chain[3]
if worker['comm'] != 'sshd-session' or parent['comm'] != 'sshd-session' or listener['comm'] != 'sshd':
    raise SystemExit('unexpected process roles')
if worker['ppid'] != parent['pid'] or parent['ppid'] != listener['pid']:
    raise SystemExit('unexpected parent chain')
transport = set(worker['transport_inodes']) & set(parent['transport_inodes']) & set(ancestry['ssh_transport_inodes'])
if len(transport) != 1:
    raise SystemExit('transport inode is not unique')

def state(x):
    p = Path('/proc') / str(x['pid'])
    stat = (p / 'stat').read_text().rsplit(')', 1)[1].split()
    if int(stat[19]) != x['start_ticks'] or int(stat[1]) != x['ppid'] or (p / 'comm').read_text().strip() != x['comm']:
        raise RuntimeError('pid_identity_changed_' + str(x['pid']))
    inodes = set()
    for fd in (p / 'fd').iterdir():
        try:
            link = os.readlink(fd)
        except OSError:
            continue
        if link.startswith('socket:['):
            inodes.add(link[8:-1])
    return {'pid': x['pid'], 'ppid': int(stat[1]), 'start_ticks': int(stat[19]),
            'comm': x['comm'], 'nofile': list(resource.prlimit(x['pid'], resource.RLIMIT_NOFILE)),
            'transport_present': bool(transport & inodes)}

pidfd = os.pidfd_open(worker['pid'])
poll = select.poll()
poll.register(pidfd, select.POLLIN)
if poll.poll(0):
    raise SystemExit('worker already exited')
before = [state(x) for x in (worker, parent, listener)]
if not before[0]['transport_present'] or not before[1]['transport_present'] or before[2]['transport_present']:
    raise SystemExit('transport ownership changed')
soft, hard = before[0]['nofile']
if soft < 32768 and hard < 32768:
    print(json.dumps({'result': 'hard_limit_insufficient', 'before': before, 'transport_inode': list(transport)[0]}))
    raise SystemExit(2)
# Check the same PID/start-ticks/parent/transport immediately before the sole
# prlimit call, and retain a pidfd to detect exit during the operation.
checked = state(worker)
if checked != before[0] or poll.poll(0):
    raise SystemExit('worker changed immediately before adjustment')
if soft < 32768:
    old = resource.prlimit(worker['pid'], resource.RLIMIT_NOFILE, (32768, hard))
    if tuple(old) != (soft, hard):
        raise RuntimeError('worker limits changed concurrently')
after = [state(x) for x in (worker, parent, listener)]
if poll.poll(0):
    raise RuntimeError('worker exited during adjustment')
if after[0]['nofile'] != [max(soft, 32768), hard] or after[1:] != before[1:]:
    raise RuntimeError('limit readback or untouched process changed')
print(json.dumps({'result': 'adjusted' if soft < 32768 else 'already_sufficient',
                  'transport_inode': list(transport)[0], 'before': before,
                  'immediately_before': checked, 'after': after, 'hard_limit_preserved': True}))
os.close(pidfd)
