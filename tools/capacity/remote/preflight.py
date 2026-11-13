#!/usr/bin/env python3
"""Read-only C54 host preflight; reads metadata, not business DBs or secrets."""
from datetime import datetime
from zoneinfo import ZoneInfo
import hashlib
import json
import os
import resource
import shutil
import subprocess
import sys
import time
from pathlib import Path

if len(sys.argv) != 8:
    raise SystemExit('usage: preflight.py ROOT BASELINE_JSON APP CATALOG WEB SERVICE WATCH')
root, baseline, app, catalog, web, service, watcher = sys.argv[1:]
if any(not Path(p).is_absolute() for p in (root, baseline, app, catalog, web)):
    raise SystemExit('paths must be absolute')
allowed = Path('/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54').resolve()
for candidate, name in ((baseline, 'baseline-containers.json'), (app, 'ranking'),
                        (catalog, 'models.json'), (web, 'web-dist')):
    path = Path(candidate)
    if path.is_symlink() or not path.resolve(strict=True).is_relative_to(allowed) or path.name != name:
        raise SystemExit('unexpected task asset path: '+candidate)
original = json.loads(Path(baseline).read_text())
if not isinstance(original, dict) or len(original) != 7:
    raise SystemExit('expected seven baseline containers')


def command(args):
    p = subprocess.run(args, capture_output=True, text=True, timeout=12)
    return {'returncode': p.returncode, 'stdout': p.stdout, 'stderr': p.stderr}


def digest(path):
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for block in iter(lambda: f.read(1024*1024), b''):
            h.update(block)
    return h.hexdigest()


def pressure(kind):
    return {line.split()[0]: float(line.split()[1].split('=')[1])
            for line in Path('/proc/pressure/'+kind).read_text().splitlines()}


fmt = '{{.Name}} {{.State.Running}} {{.RestartCount}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}'
containers = command(['docker', 'inspect', '-f', fmt, *original])
actual = {fields[0].lstrip('/'): fields[1:] for line in containers['stdout'].splitlines() if (fields := line.split())}
listener = command(['systemctl', 'show', 'ssh.service', '-p', 'MainPID', '--value'])
pid = int(listener['stdout'].strip() or '0')
listener_info = None
if pid:
    p = Path('/proc')/str(pid)
    st = (p/'stat').read_text().rsplit(')', 1)[1].split()
    listener_info = {'pid': pid, 'start_ticks': int(st[19]), 'comm': (p/'comm').read_text().strip(),
                     'nofile': list(resource.prlimit(pid, resource.RLIMIT_NOFILE))}
mem = {line.split(':')[0]: int(line.split()[1]) for line in Path('/proc/meminfo').read_text().splitlines()
       if line.startswith('MemAvailable:')}
unit = {name: command(['systemctl', 'is-active', name]) for name in
        (service, watcher, 'ranking-c54.service', 'ranking-c54-watch.service')}
port = command(['ss', '-H', '-tan', '( sport = :4210 or dport = :4210 )'])
root_path = Path(root)
parent = root_path.parent
while not parent.exists() and parent != parent.parent:
    parent = parent.parent
result = {
    'time': time.time(), 'business_day': datetime.now(ZoneInfo('Asia/Shanghai')).date().isoformat(),
    'utc_time': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
    'containers': containers, 'expected_containers': original, 'actual_containers': actual,
    'containers_match': actual == original, 'units': unit, 'port': port,
    'disk_free_bytes': shutil.disk_usage(parent).free, 'memory_available_mib': mem['MemAvailable']/1024,
    'memory_psi': pressure('memory'), 'io_psi': pressure('io'), 'root_exists': root_path.exists(),
    'task_marker': (root_path/'STOPPED').exists(), 'original_marker': (Path(baseline).parent/'STOPPED').exists(),
    'listener': listener_info, 'hashes': {'application': digest(app), 'models': digest(catalog),
                                         'baseline_file': digest(baseline)},
}
print(json.dumps(result, sort_keys=True))
