#!/usr/bin/env python3
"""Independent SSH sampler; original watchdog remains authoritative."""
import collections
import json
import os
import re
import resource
import subprocess
import sys
import time
from pathlib import Path

if len(sys.argv) != 7:
    raise SystemExit('usage: remote-monitor.py TASK_ROOT TEST.service WATCH.service BACKLOG_PATH WORKER_JSON EXPECTED_HARD')
if not Path(sys.argv[1]).is_absolute():
    raise SystemExit('task root must be absolute')
R = Path(sys.argv[1]).resolve()
service, watcher = sys.argv[2:4]
if any(not re.fullmatch(r'ranking-c54(?:-[a-z0-9-]+)?\.service', unit) for unit in (service, watcher)) or service == watcher:
    raise SystemExit('invalid task units')
backlog_path = Path(sys.argv[4]).resolve()
if not backlog_path.is_relative_to(R):
    raise SystemExit('backlog path must belong to task root')
worker = json.loads(sys.argv[5])
expected_hard = int(sys.argv[6])
watch = R / 'logs/watch.jsonl'
start = time.monotonic()

def last_json(path):
    with path.open('rb') as f:
        f.seek(0, os.SEEK_END)
        f.seek(max(0, f.tell()-131072))
        for line in reversed(f.read().splitlines()):
            try:
                return json.loads(line)
            except (ValueError, UnicodeError):
                pass
    return None

def tcp_map():
    result = {}
    for name in ('/proc/net/tcp', '/proc/net/tcp6'):
        for line in Path(name).read_text().splitlines()[1:]:
            f = line.split()
            lp, rp = int(f[1].split(':')[1], 16), int(f[2].split(':')[1], 16)
            if lp == 4210 or rp == 4210:
                result[f[9]] = ('local' if lp == 4210 else 'remote') + '_' + f[3]
    return result

def inspect(pid, ticks=None, tcp=None):
    p = Path('/proc') / str(pid)
    st = (p / 'stat').read_text().rsplit(')', 1)[1].split()
    if ticks is not None and int(st[19]) != ticks:
        raise RuntimeError('worker_pid_reused')
    status = (p / 'status').read_text().splitlines()
    rss = next((int(x.split()[1]) for x in status if x.startswith('VmRSS:')), 0)
    fd = list((p / 'fd').iterdir())
    sockets = 0
    paths = collections.Counter()
    for item in fd:
        try:
            link = os.readlink(item)
        except OSError:
            continue
        if link.startswith('socket:['):
            sockets += 1
            if tcp and link[8:-1] in tcp:
                paths[tcp[link[8:-1]]] += 1
    return {'pid':pid, 'ppid':int(st[1]), 'start_ticks':int(st[19]),
            'fd':len(fd), 'sockets':sockets, 'rss_kib':rss,
            'cpu_ticks':int(st[11])+int(st[12]),
            'nofile':list(resource.prlimit(pid, resource.RLIMIT_NOFILE)), 'tcp_4210':dict(paths)}

try:
    while time.monotonic() - start < 6900:
        tick = time.monotonic()
        unit = {}
        for name in (service, watcher):
            p = subprocess.run(['systemctl', 'is-active', name], capture_output=True, text=True, timeout=3)
            unit[name] = p.stdout.strip()
        w = last_json(watch)
        guard = {'units': unit, 'marker': (R/'STOPPED').exists(),
                 'watch_age_s': None if not w else max(0, time.time()-w['time']),
                 'watch_stop_reason': None if not w else w.get('stop_reason'),
                 'available_mib': None if not w else w.get('available_mib'),
                 'memory_psi_full': None if not w else w.get('memory_psi', {}).get('full'),
                 'io_psi_full': None if not w else w.get('io_psi', {}).get('full'),
                 'disk_free_gib': None if not w else w.get('disk_free_gib'),
                 'cache_generated_at': None if not w else w.get('cache_generated_at'),
                 'cache_publish_upper_seconds': None if not w else w.get('cache_publish_upper_seconds')}
        if unit[watcher] != 'active' or guard['marker'] or guard['watch_stop_reason'] or guard['watch_age_s'] is None or guard['watch_age_s'] > 8:
            raise RuntimeError('original_guard_unavailable_or_stopped: '+json.dumps(guard))
        tunnel = inspect(worker['pid'], worker['start_ticks'], tcp_map())
        if tunnel['ppid'] != worker['ppid'] or tunnel['nofile'][0] < 32768 or tunnel['nofile'][1] != expected_hard:
            raise RuntimeError('worker_identity_or_limits_changed')
        cgroup = Path('/sys/fs/cgroup/system.slice') / service / 'cgroup.procs'
        service_procs = []
        if cgroup.exists():
            for item in cgroup.read_text().splitlines():
                try:
                    service_procs.append(inspect(int(item)))
                except (OSError, ProcessLookupError):
                    pass
        selfstat = (Path('/proc/self/stat').read_text().rsplit(')', 1)[1].split())
        current_backlog = backlog_path
        if backlog_path.is_dir():
            candidates = list(backlog_path.glob('backlog-*.jsonl'))
            current_backlog = max(candidates, key=lambda p: p.stat().st_mtime_ns) if candidates else None
        backlog = last_json(current_backlog) if current_backlog and current_backlog.is_file() else None
        db_path = R / 'data/ranking.sqlite'
        database = {}
        for suffix, name in (('', 'db_bytes'), ('-wal', 'wal_bytes'), ('-shm', 'shm_bytes')):
            try:
                database[name] = Path(str(db_path)+suffix).stat().st_size
            except FileNotFoundError:
                database[name] = 0
        print(json.dumps({'time':time.time(), 'event':'sample', 'worker':tunnel, 'database':database,
                          'clock_ticks_per_second':os.sysconf('SC_CLK_TCK'),
                          'service_processes':service_procs, 'guard':guard, 'backlog':backlog,
                          'host_sockstat':Path('/proc/net/sockstat').read_text().splitlines(),
                          'monitor_pid':os.getpid(), 'monitor_cpu_ticks':int(selfstat[11])+int(selfstat[12])}), flush=True)
        time.sleep(max(.05, 1-(time.monotonic()-tick)))
    raise RuntimeError('monitor_deadline')
except Exception as e:
    print(json.dumps({'time':time.time(), 'fatal':type(e).__name__+': '+str(e)}), flush=True)
    sys.exit(2)
