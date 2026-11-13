"""Task-specific C54 remote adapter; importing this module starts nothing.

The controller requires an explicit remote-execution flag. Its safety chain
and resource assumptions remain restricted to the authorized C54 environment.
"""
import hashlib
import json
import os
import re
import shlex
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
from business_day import business_day
import urllib.request
from pathlib import Path, PurePosixPath

from sustained_process import LocalDriver
from sustained_remote_spec import RemoteSpec, validate_remote_assets
from sustained_resource import SustainedCPUCheck, forecast
from sustained_session import stop_owned_unit
from sustained_transfer import guarded_command


class RemoteError(RuntimeError):
    pass


class RemoteHostProtection(RemoteError):
    host_protection = True


def sha256_file(path):
    digest = hashlib.sha256()
    with open(path, 'rb') as f:
        for block in iter(lambda: f.read(1024*1024), b''):
            digest.update(block)
    return digest.hexdigest()


def runtime_hashes():
    root = Path(__file__).resolve().parent
    names = ('sustained_controller.py', 'sustained_state.py', 'sustained_deadline.py',
             'sustained_process.py', 'sustained_transfer.py', 'sustained_remote_spec.py',
             'sustained_remote_adapter.py', 'business_day.py', 'sustained_resource.py', 'sustained_session.py',
             'sustained_reviewed_checkpoint.py')
    paths = [root/name for name in names] + sorted((root/'remote').glob('*.py'))
    return {str(path.relative_to(root)): sha256_file(path) for path in paths}


class Commands:
    """Record each independent SSH/local command, including rc and stderr."""
    def __init__(self, log, host):
        if host != 'lax-racknerd':
            raise ValueError('only the authorized host is permitted')
        self.log = Path(log)
        self.host = host
        self.ssh_base = ['rtk', 'proxy', 'ssh', '-S', 'none', '-o', 'ControlMaster=no', '-o', 'BatchMode=yes',
                         '-o', 'ConnectTimeout=8', host]

    def record(self, row):
        with self.log.open('a', encoding='utf-8') as f:
            f.write(json.dumps({'time_ns': time.time_ns(), **row}, sort_keys=True)+'\n')
            f.flush()

    def run(self, argv, *, timeout=30, input_text=None, check=True, cwd=None):
        try:
            p = subprocess.run(argv, input=input_text, capture_output=True, text=True,
                               timeout=timeout, cwd=cwd)
        except BaseException as exc:
            self.record({'argv': argv, 'returncode': None, 'stderr': repr(exc)})
            raise
        self.record({'argv': argv, 'returncode': p.returncode, 'stderr': p.stderr, 'stdout': p.stdout})
        if check and p.returncode:
            raise RemoteError('command returned '+str(p.returncode)+': '+p.stderr[-1000:])
        return p

    def ssh(self, argv, *, timeout=30, check=True):
        return self.run(self.ssh_base+[shlex.join([str(x) for x in argv])], timeout=timeout, check=check)


class RemoteAdapter:
    """C54-only preflight, service/watch, snapshot, verification and cleanup."""
    def __init__(self, config, commands=None):
        self.cfg = dict(config)
        if config.get('host') != 'lax-racknerd':
            raise ValueError('only the authorized host is permitted')
        token = config.get('task_id', '')
        if (not isinstance(token, str) or not re.fullmatch(r'[0-9a-f]{16,32}', token) or
                config.get('service') != 'ranking-c54-'+token+'.service' or
                config.get('watch') != 'ranking-c54-'+token+'-watch.service' or
                PurePosixPath(config.get('remote_root', '')).name != 'task-'+token):
            raise ValueError('task root and units must share a unique hexadecimal task identity')
        self.local = Path(config['local_task_dir'])
        remote = PurePosixPath(config['remote_root'])
        parent = PurePosixPath('/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/sustained-20261113-r1')
        local_parent = Path('/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/sustained-20261113-r1')
        if (not self.local.is_absolute() or self.local.is_symlink() or not self.local.is_dir() or
                self.local == local_parent or not self.local.resolve().is_relative_to(local_parent)):
            raise ValueError('local task directory must be a new private subdirectory of the C54 task root')
        info = self.local.stat()
        if info.st_uid != os.getuid() or info.st_mode & 0o077:
            raise ValueError('local task directory is not private/task owned')
        if not remote.is_absolute() or '..' in remote.parts or remote == parent or not remote.is_relative_to(parent):
            raise ValueError('remote root must be a dedicated subdirectory of the C54 task root')
        validate_remote_assets(config['application_remote'], config['catalog_remote'], config['web_remote'])
        self.spec = RemoteSpec(str(remote), config['service'], config['watch'], config['application_remote'],
                               config['catalog_remote'], config['web_remote'])
        self.binary = Path(config['capacity_binary'])
        self.catalog = Path(config['catalog_local'])
        self.fixture = self.local/'fixture.json'
        self.seed = self.local/'seed.sqlite'
        self.journal = self.local/'sustained.jsonl.gz'
        self.seed_manifest = self.local/'seed-manifest.json'
        self.last_remote = None
        self.state = None
        self.run_dir = Path(tempfile.mkdtemp(prefix='adapter-', dir=self.local))
        # Never append a continued invocation's commands to the already
        # audited prior invocation's immutable command evidence.
        self.commands = commands or Commands(self.run_dir/'commands.jsonl', config['host'])
        self.session = self.session_pidfd = self.session_dir = None
        self.session_ready = None
        self.last_local_sample = 0.0
        self.last_fast_guard = 0.0
        self.cpu_checks = {'generator': SustainedCPUCheck(), 'local_ssh': SustainedCPUCheck(),
                           'local_combined': SustainedCPUCheck(), 'remote_ssh': SustainedCPUCheck()}
        self.cpu_warning_active = {}
        self.unwritten_since = None
        self.service_expected = False
        self.property_thread = None
        self.property_error = None
        self.property_started = 0
        self.journal_process = self.journal_pidfd = self.journal_thread = None
        self.journal_error = None
        self.journal_stop = threading.Event()
        self.driver = None
        self.transfer_index = 0
        self.phase_index = 0
        self.session_stderr = None
        self.cleanup_log_error = None

    def preflight(self, state):
        if 'utc_day' in (state.get('preflight') or {}):
            raise RemoteError('legacy UTC-day state cannot be reused; rebuild Shanghai fixtures')
        for path in (self.binary, self.catalog):
            if not path.is_absolute() or not path.is_file():
                raise RemoteError('missing absolute local executable/catalog: '+str(path))
        expected_binary = self.cfg['capacity_sha256']
        expected_catalog = self.cfg['catalog_sha256']
        if sha256_file(self.binary) != expected_binary or sha256_file(self.catalog) != expected_catalog:
            raise RemoteError('local executable or catalog hash mismatch')
        for path in (self.fixture, self.seed, self.seed_manifest, self.journal):
            if path.is_symlink():
                raise RemoteError('task evidence path may not be a symlink: '+str(path))
        if self.journal.exists() and state['status'] == 'new':
            raise RemoteError('a new run may not reuse an existing journal')
        if state['status'] in ('transition_ready', 'reviewed_search_ready'):
            expected = state.get('transition_evidence', {}).get('journal_sha256')
            if not self.journal.is_file() or not expected or sha256_file(self.journal) != expected:
                raise RemoteError('planned transition journal is missing or changed')
        if len({self.fixture.exists(), self.seed.exists(), self.seed_manifest.exists()}) != 1:
            raise RemoteError('incomplete fixture/seed/manifest; never fabricate a replacement')
        day = business_day()
        if not self.seed.exists():
            if state['status'] != 'new':
                raise RemoteError('planned transition lost the original seed')
            import shutil
            if shutil.disk_usage(self.local).free < 5*(1 << 30):
                raise RemoteError('insufficient local space before seed creation')
            p = self.commands.run([str(self.binary), 'seed-sustained', '--db', str(self.seed),
                                   '--fixture', str(self.fixture), '--models', str(self.catalog),
                                   '--clients', '10000', '--history-days', '90', '--backlog', '10000',
                                   '--day', day, '--model-seed', '8675309'], timeout=600)
            inspected = json.loads(p.stdout)
            if (inspected.get('clients'), inspected.get('daily_reports'), inspected.get('closed_backlog')) != (10000, 30000, 10000):
                raise RemoteError('synthetic seed counts do not match the fixed fixture')
            manifest = {'day': day, 'model_seed': 8675309, 'clients': 10000,
                        'fixture_sha256': sha256_file(self.fixture), 'seed_sha256': sha256_file(self.seed),
                        'capacity_sha256': expected_binary, 'catalog_sha256': expected_catalog}
            with self.seed_manifest.open('x', encoding='utf-8') as out:
                json.dump(manifest, out, sort_keys=True)
                out.write('\n')
                out.flush()
                os.fsync(out.fileno())
        manifest = json.loads(self.seed_manifest.read_text())
        fixture = json.loads(self.fixture.read_text())
        checkpoint = state.get('reviewed_checkpoint')
        manifest_binary = expected_binary
        if checkpoint:
            if (self.cfg['task_id'] != '721cb71e50f60575' or
                    checkpoint.get('original_capacity_sha256') != '404ba7bb01728bf37b0c768e200e42505379f2f3942921f13792fa47901365b8' or
                    checkpoint.get('new_capacity_sha256') != expected_binary):
                raise RemoteError('invalid reviewed checkpoint binary migration')
            if checkpoint.get('retry_manifest_sha256'):
                retry_path = Path(self.cfg.get('retry_migration', ''))
                if (retry_path.is_symlink() or not retry_path.is_file() or
                        sha256_file(retry_path) != checkpoint['retry_manifest_sha256'] or
                        json.loads(retry_path.read_text()) != checkpoint.get('retry_manifest')):
                    raise RemoteError('reviewed retry migration manifest changed')
            if checkpoint.get('pending_manifest_sha256'):
                pending_path = Path(self.cfg.get('pending_migration', ''))
                if (pending_path.is_symlink() or not pending_path.is_file() or
                        sha256_file(pending_path) != checkpoint['pending_manifest_sha256'] or
                        json.loads(pending_path.read_text()) != checkpoint.get('pending_manifest')):
                    raise RemoteError('reviewed pending migration manifest changed')
            manifest_binary = checkpoint['original_capacity_sha256']
        expected_manifest = {'day': day, 'model_seed': 8675309, 'clients': 10000,
                             'fixture_sha256': sha256_file(self.fixture), 'seed_sha256': sha256_file(self.seed),
                             'capacity_sha256': manifest_binary, 'catalog_sha256': expected_catalog}
        if manifest != expected_manifest or (fixture.get('day'), fixture.get('clients'), fixture.get('history_days'), fixture.get('backlog')) != (day, 10000, 90, 10000):
            raise RemoteError('fixture, Shanghai business day, seed or immutable manifest mismatch')
        previous = state.get('preflight')
        tools = runtime_hashes()
        if previous and (any(previous.get(key) != expected_manifest[key] for key in
                             ('fixture_sha256', 'seed_sha256', 'catalog_sha256')) or
                         previous.get('capacity_sha256') != expected_binary or
                         previous.get('runtime_hashes') != tools):
            raise RemoteError('preflight identities or scripts changed across a planned transition')
        # No remote unit, forward or load is started by this preflight.
        script = Path(__file__).with_name('remote')/'preflight.py'
        code = script.read_text()
        argv = ['/usr/bin/python3', '-c', code, self.spec.root,
                '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/baseline-containers.json',
                self.spec.app, self.spec.catalog, self.spec.web, self.spec.service, self.spec.watcher]
        result = self.commands.ssh(argv, timeout=35)
        remote = json.loads(result.stdout)
        expected_day = business_day()
        if day != expected_day or remote.get('business_day') != expected_day:
            raise RemoteError('local/remote Shanghai business day mismatch')
        expected_containers = remote.get('expected_containers', {})
        baseline_hash = hashlib.sha256(json.dumps(expected_containers, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
        if (remote.get('containers', {}).get('returncode') != 0 or not remote.get('containers_match') or
                len(remote.get('actual_containers', {})) != 7 or baseline_hash != self.cfg['baseline_containers_sha256']):
            raise RemoteError('original seven-container baseline changed')
        if remote.get('original_marker') or remote.get('task_marker'):
            raise RemoteError('existing host protection marker')
        if remote.get('port', {}).get('returncode') != 0 or remote['port'].get('stdout', '').strip():
            raise RemoteError('remote test port is not clear')
        for unit in (self.spec.service, self.spec.watcher, 'ranking-c54.service', 'ranking-c54-watch.service'):
            value = remote['units'][unit]
            if value.get('stdout', '').strip() not in ('inactive', 'unknown'):
                raise RemoteError('test unit is not inactive: '+unit)
        if remote.get('hashes', {}).get('application') != self.cfg['application_sha256'] or remote['hashes'].get('models') != expected_catalog:
            raise RemoteError('remote application/catalog hash mismatch')
        if (remote.get('listener') or {}).get('comm') != 'sshd':
            raise RemoteError('SSH listener baseline missing')
        if (remote.get('memory_available_mib', 0) < 512 or remote.get('memory_psi', {}).get('full', 1e9) > 1 or
                remote.get('io_psi', {}).get('full', 1e9) > 10):
            raise RemoteError('remote host is already under protected pressure')
        if remote.get('disk_free_bytes', 0) < 12*(1 << 30)+2*self.seed.stat().st_size:
            raise RemoteError('remote disk forecast would consume the 10GiB guard reserve')
        if state['status'] == 'new' and remote.get('root_exists'):
            raise RemoteError('new run may not reuse an existing remote root')
        if state['status'] in ('transition_ready', 'reviewed_search_ready') and not remote.get('root_exists'):
            raise RemoteError('planned transition database root is missing')
        if state['status'] == 'reviewed_search_ready':
            evidence = state.get('transition_evidence', {})
            if self._snapshot_state() != evidence.get('source_state'):
                raise RemoteError('reviewed remote stopped database identity changed')
            remote_hash = self.commands.ssh(['/usr/bin/sha256sum', '--', self.spec.db], timeout=120).stdout.split()
            if not remote_hash or remote_hash[0] != evidence.get('snapshot_sha256'):
                raise RemoteError('reviewed remote stopped database content changed')
            original_listener = checkpoint.get('original_listener') if checkpoint else None
            if remote.get('listener') != original_listener:
                raise RemoteError('reviewed SSH listener identity or limits changed')
        listener = socket.socket()
        try:
            listener.bind(('127.0.0.1', 14210))
        except OSError as exc:
            raise RemoteError('local test forwarding port unavailable') from exc
        finally:
            listener.close()
        self.last_remote = remote
        self.state = state
        return {'verified': True, 'remote': remote, 'business_day': expected_day,
                'local_utc_time': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                'capacity_sha256': expected_binary, 'catalog_sha256': expected_catalog,
                'fixture_sha256': manifest['fixture_sha256'], 'seed_sha256': manifest['seed_sha256'],
                'runtime_hashes': tools}

    def preflight_stage(self, rate, phase, state):
        if self.last_remote is None or not self.seed.exists():
            return {'ok': False, 'issues': ['no_remote_preflight_or_seed']}
        import shutil
        mem = next(int(line.split()[1])*1024 for line in Path('/proc/meminfo').read_text().splitlines()
                   if line.startswith('MemAvailable:'))
        remote_data = self.commands.ssh(['/usr/bin/python3', '-c',
                                         'import json,shutil,sys,time;from datetime import datetime;from zoneinfo import ZoneInfo;print(json.dumps({"free":shutil.disk_usage(sys.argv[1]).free,"day":datetime.now(ZoneInfo("Asia/Shanghai")).date().isoformat()}))',
                                         self.spec.root], timeout=8)
        data = json.loads(remote_data.stdout)
        day = json.loads(self.seed_manifest.read_text())['day']
        if day != business_day() or data.get('day') != day:
            return {'ok': False, 'issues': ['business_day_changed']}
        db_bytes = 0
        if self.session is not None:
            row = self._last_remote_sample()
            sizes = row.get('database', {})
            db, wal = sizes.get('db_bytes'), sizes.get('wal_bytes')
            if type(db) is not int or type(wal) is not int or min(db, wal) < 0:
                return {'ok': False, 'issues': ['database_size_unknown']}
            db_bytes = db+wal
        if type(db_bytes) is not int or db_bytes < 0:
            return {'ok': False, 'issues': ['database_size_unknown']}
        if phase == 'confirmation' or state.get('status') == 'reviewed_search_ready':
            source = state.get('transition_evidence', {}).get('source_state', {})
            source_bytes = source.get('files', {}).get(self.spec.db, {}).get('size', 0)
            if type(source_bytes) is not int or source_bytes <= 0:
                return {'ok': False, 'issues': ['database_size_unknown']}
            db_bytes = max(db_bytes, source_bytes)
        if phase == 'confirmation' and db_bytes == 0:
            return {'ok': False, 'issues': ['database_size_unknown']}
        basis = max(self.seed.stat().st_size, db_bytes)
        owned_anon = 0
        if self.driver is not None and self.driver.proc is not None and self.driver.proc.poll() is None:
            owned_anon = self.driver.resident_anon_bytes()
        return forecast(int(rate), phase, journal_bytes=self.journal.stat().st_size if self.journal.exists() else 0,
                        max_journal_bytes=int(self.cfg.get('max_journal_bytes', 4 << 30)),
                        local_free=shutil.disk_usage(self.local).free,
                        remote_free=int(data['free']), local_mem_available=mem,
                        pending_limit=int(self.cfg.get('max_pending', 200000)), seed_bytes=basis,
                        local_owned_anon=owned_anon)

    def _deploy(self):
        root = self.spec.root
        # The authorized sustained parent may not exist on the remote host.
        # Create it once without changing an existing directory's permissions.
        parent_code = ('import os,stat,sys\np=sys.argv[1]\n'
                       'try: os.mkdir(p,0o700)\nexcept FileExistsError: pass\n'
                       's=os.lstat(p)\n'
                       'if not stat.S_ISDIR(s.st_mode) or s.st_uid!=os.getuid() or s.st_mode&0o077:'
                       ' raise SystemExit("sustained parent is not a private owned directory")')
        self.commands.ssh(['/usr/bin/python3', '-c', parent_code, str(PurePosixPath(root).parent)])
        self.commands.ssh(['/bin/mkdir', '-m', '700', '--', root])
        self.commands.ssh(['/bin/mkdir', '-m', '700', '--', *[root+'/'+d for d in ('bin', 'data', 'staging', 'tmp', 'logs')]])
        self.commands.ssh(['/bin/cp', '--', '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/baseline-containers.json',
                           root+'/baseline-containers.json'])
        self.commands.ssh(['/usr/bin/touch', '--', root+'/logs/watch.jsonl'])
        remote_dir = Path(__file__).with_name('remote')
        names = ('watch.py', 'adjust-worker.py', 'probe-ancestry.py', 'remote-monitor.py',
                 'backlog-observer.py', 'direct-paced-copy.py', 'snapshot-state.py')
        # Only tiny task-specific scripts are uploaded before the watch starts.
        self.commands.run(['rtk', 'proxy', 'scp', '-l', '32768', '-o', 'BatchMode=yes',
                           '-o', 'ControlMaster=no', '-o', 'ControlPath=none',
                           *[str(remote_dir/name) for name in names], self.cfg['host']+':'+root+'/bin/'], timeout=60)

    def _verify_remote_scripts(self):
        remote_dir = Path(__file__).with_name('remote')
        for name in ('watch.py', 'adjust-worker.py', 'probe-ancestry.py', 'remote-monitor.py',
                     'backlog-observer.py', 'direct-paced-copy.py', 'snapshot-state.py'):
            out = self.commands.ssh(['/usr/bin/sha256sum', '--', self.spec.root+'/bin/'+name], timeout=30).stdout
            if out.split()[0] != sha256_file(remote_dir/name):
                raise RemoteError('remote task script changed: '+name)
        p = self.commands.ssh(['/usr/bin/sha256sum', '--',
                               '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/baseline-containers.json',
                               self.spec.root+'/baseline-containers.json'], timeout=30)
        hashes = [line.split()[0] for line in p.stdout.splitlines()]
        if len(hashes) != 2 or hashes[0] != hashes[1]:
            raise RemoteError('seven-container baseline file changed')

    def _start_session(self):
        self.session_dir = Path(tempfile.mkdtemp(prefix='session-', dir=self.local))
        config = {'local_session_dir': str(self.session_dir), 'remote_root': self.spec.root,
                  'service': self.spec.service, 'watch': self.spec.watcher, 'host': self.cfg['host'],
                  'application': self.spec.app, 'catalog': self.spec.catalog, 'web': self.spec.web}
        config_path = self.session_dir/'config.json'
        with config_path.open('x', encoding='utf-8') as out:
            json.dump(config, out, sort_keys=True)
            out.write('\n')
        self.session_stderr = (self.session_dir/'session-stderr.log').open('x', encoding='utf-8')
        argv = [sys.executable, str(Path(__file__).with_name('sustained_session.py')), str(config_path)]
        self.session = subprocess.Popen(argv, stdout=subprocess.DEVNULL, stderr=self.session_stderr,
                                        start_new_session=True)
        self.session_pidfd = os.pidfd_open(self.session.pid)
        self.commands.record({'event': 'session_start', 'argv': argv, 'pid': self.session.pid,
                              'returncode': None, 'stderr_path': str(self.session_dir/'session-stderr.log')})
        end = time.monotonic()+90
        ready = self.session_dir/'session-ready.json'
        while time.monotonic() < end:
            if self.session.poll() is not None:
                raise RemoteError('SSH/watch session exited before ready')
            if ready.exists():
                try:
                    data = json.loads(ready.read_text())
                except (OSError, json.JSONDecodeError):
                    time.sleep(.05)
                    continue
                if data.get('service') != self.spec.service or data.get('watch') != self.spec.watcher:
                    raise RemoteError('session identity mismatch')
                self.session_ready = data
                return
            time.sleep(.1)
        raise RemoteError('SSH/watch session startup timeout')

    def _last_remote_sample(self):
        if self.session is None or self.session.poll() is not None:
            raise RemoteError('SSH/watch session unavailable')
        path = self.session_dir/'remote-samples.jsonl'
        with path.open('rb') as f:
            f.seek(0, os.SEEK_END)
            f.seek(max(0, f.tell()-131072))
            lines = f.read().splitlines()
        for line in reversed(lines):
            try:
                row = json.loads(line)
            except (json.JSONDecodeError, UnicodeDecodeError):
                continue
            observed = row.get('observed_local_unix_ns')
            if not isinstance(observed, int) or not -2_000_000_000 <= time.time_ns()-observed <= 5_000_000_000:
                raise RemoteError('remote monitor stream is stale or clock moved')
            if row.get('fatal'):
                raise RemoteError('remote monitor reported fatal failure')
            return row
        raise RemoteError('remote monitor has no complete sample')

    def _cpu_warning(self, scope, active):
        # CPU saturation alone is diagnostic, not proof that arrivals or the
        # transport are invalid. Log transitions; the independent arrival,
        # lag, FD, connection, journal and host guards remain in force.
        if active and not self.cpu_warning_active.get(scope, False):
            with (self.run_dir/'cpu-warnings.jsonl').open('a', encoding='utf-8') as out:
                out.write(json.dumps({'time_ns': time.time_ns(), 'scope': scope,
                                      'event': 'sustained_cpu_warning'}, sort_keys=True)+'\n')
                out.flush()
        self.cpu_warning_active[scope] = bool(active)

    def guard(self):
        try:
            row = self._last_remote_sample()
            g = row.get('guard', {})
            reason = g.get('watch_stop_reason')
            if g.get('marker') or reason:
                return {'healthy': False, 'host_protection': True, 'reason': reason or 'remote stop marker'}
            if (g.get('units', {}).get(self.spec.watcher) != 'active' or
                    g.get('watch_age_s') is None or g['watch_age_s'] > 8):
                raise RemoteError('remote watchdog unavailable or stale')
            if g.get('disk_free_gib') is None or g['disk_free_gib'] < 10:
                return {'healthy': False, 'host_protection': True, 'reason': 'remote disk reserve below 10GiB'}
            expected = (self.session_ready or {}).get('worker', {})
            limits = (self.session_ready or {}).get('adjustment', {}).get('after', [{}])[0].get('nofile', [])
            worker = row.get('worker', {})
            if (len(limits) != 2 or worker.get('pid') != expected.get('pid') or
                    worker.get('start_ticks') != expected.get('start_ticks') or worker.get('ppid') != expected.get('ppid') or
                    not isinstance(worker.get('nofile'), list) or len(worker['nofile']) != 2 or
                    worker['nofile'][0] < 32768 or worker['nofile'][1] != limits[1]):
                raise RemoteError('SSH forwarding worker identity or limits changed')
            if self.service_expected and g.get('units', {}).get(self.spec.service) != 'active':
                raise RemoteError('isolated service is not active')
            if worker.get('fd') is None or worker['fd'] >= worker['nofile'][0]*.9:
                return {'healthy': True, 'tool_failure': 'forwarding worker near file-descriptor limit'}
            clock_hz = row.get('clock_ticks_per_second')
            if not isinstance(clock_hz, int) or clock_hz <= 0:
                raise RemoteError('remote worker CPU tick frequency unavailable')
            observed_seconds = row['observed_local_unix_ns']/1e9
            remote_busy = self.cpu_checks['remote_ssh'].observe(observed_seconds, worker['cpu_ticks'], clock_hz, 1)
            self._cpu_warning('remote_ssh', remote_busy and self.service_expected)
            if self.service_expected and self.property_error is not None:
                raise RemoteError('service property readback failed: '+str(self.property_error))
            if self.service_expected and self.property_thread and self.property_thread.is_alive() and time.monotonic()-self.property_started > 15:
                raise RemoteError('service property readback timed out')
            if self.service_expected and self.journal_error:
                raise RemoteError('service journal observation failed: '+str(self.journal_error))
            mem = next(int(line.split()[1])*1024 for line in Path('/proc/meminfo').read_text().splitlines()
                       if line.startswith('MemAvailable:'))
            free = shutil.disk_usage(self.local).free
            if free < 4*(1 << 30) or mem < 512*(1 << 20):
                return {'healthy': True, 'tool_failure': 'local memory or disk safety reserve exhausted'}
            if self.driver is not None and self.driver.proc is not None and self.driver.proc.poll() is None and time.monotonic()-self.last_local_sample >= 5:
                proc = Path('/proc')/str(self.driver.proc.pid)
                stat = (proc/'stat').read_text().rsplit(')', 1)[1].split()
                status = (proc/'status').read_text().splitlines()
                rss = next((int(x.split()[1]) for x in status if x.startswith('VmRSS:')), 0)
                local = {'time_ns': time.time_ns(), 'pid': self.driver.proc.pid, 'start_ticks': int(stat[19]),
                         'cpu_ticks': int(stat[11])+int(stat[12]), 'rss_kib': rss,
                         'fd_count': len(list((proc/'fd').iterdir())), 'mem_available_bytes': mem,
                         'disk_free_bytes': free}
                master = (self.session_ready or {}).get('master', {})
                master_stat = (Path('/proc')/str(master['pid'])/'stat').read_text().rsplit(')', 1)[1].split()
                if int(master_stat[19]) != master['start_ticks']:
                    raise RemoteError('local SSH master identity changed')
                local['ssh_master_cpu_ticks'] = int(master_stat[11])+int(master_stat[12])
                with (self.run_dir/'generator-resource-samples.jsonl').open('a', encoding='utf-8') as out:
                    out.write(json.dumps(local, sort_keys=True)+'\n')
                    out.flush()
                hz = os.sysconf('SC_CLK_TCK')
                when = local['time_ns']/1e9
                cores = max(1, len(os.sched_getaffinity(0)))
                if self.cpu_checks['generator'].observe(when, local['cpu_ticks'], hz, cores):
                    return {'healthy': True, 'tool_failure': 'local generator sustained near CPU allocation'}
                self._cpu_warning('local_ssh', self.cpu_checks['local_ssh'].observe(when, local['ssh_master_cpu_ticks'], hz, 1))
                if self.cpu_checks['local_combined'].observe(when, local['cpu_ticks']+local['ssh_master_cpu_ticks'], hz, cores):
                    return {'healthy': True, 'tool_failure': 'generator and local SSH sustained near CPU allocation'}
                self.last_local_sample = time.monotonic()
            return {'healthy': True, 'cache_generation': g.get('cache_generated_at'),
                    'observed_unix_ns': row['observed_local_unix_ns'], 'remote_sample': row}
        except (OSError, ValueError, KeyError, RemoteError) as exc:
            return {'healthy': False, 'reason': str(exc)}

    def _guard_or_raise(self):
        value = self.guard()
        if not value.get('healthy'):
            if value.get('host_protection'):
                raise RemoteHostProtection('remote host protection: '+repr(value))
            raise RemoteError('remote safety guard failed: '+repr(value))
        if value.get('tool_failure'):
            raise RemoteError('local tool resource protection: '+str(value['tool_failure']))

    def _upload_seed(self):
        self.transfer_index += 1
        dst = self.cfg['host']+':'+self.spec.root+'/staging/seed.sqlite'
        argv = ['rtk', 'proxy', 'scp', '-l', '32768', '-o', 'BatchMode=yes',
                '-o', 'ControlMaster=no', '-o', 'ControlPath=none', str(self.seed), dst]
        guarded_command(argv, self.run_dir, 'seed-upload-%d' % self.transfer_index,
                        self._guard_or_raise, self.commands.record, timeout=900)
        remote = self.commands.ssh(['/usr/bin/sha256sum', '--', self.spec.root+'/staging/seed.sqlite'], timeout=120)
        if remote.stdout.split()[0] != sha256_file(self.seed):
            raise RemoteError('uploaded seed hash mismatch')

    def _read_service_journal(self):
        try:
            with (self.run_dir/'service-journal-live.log').open('x', encoding='utf-8', buffering=1) as out:
                for line in self.journal_process.stdout:
                    out.write(line)
                    out.flush()
                    if 'cache refresh failed' in line:
                        self.journal_error = 'cache refresh failed: '+line.strip()
                        break
        except BaseException as exc:
            self.journal_error = repr(exc)
        finally:
            if not self.journal_stop.is_set() and not self.journal_error:
                self.journal_error = 'service journal stream closed'

    def _start_journal(self):
        if self.journal_process is not None:
            return
        # Direct exec gives a verifiable PID for this independent SSH stream.
        argv = ['/usr/bin/ssh', '-S', 'none', '-o', 'ControlMaster=no', '-o', 'BatchMode=yes',
                '-o', 'ConnectTimeout=8', self.cfg['host'],
                shlex.join(['/usr/bin/journalctl', '--follow', '--lines=0',
                            '--no-pager', '--output=cat', '--unit='+self.spec.service])]
        self.journal_stderr = (self.run_dir/'service-journal-stderr.log').open('x', encoding='utf-8')
        self.journal_process = subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=self.journal_stderr,
                                                text=True, bufsize=1, start_new_session=True)
        self.journal_pidfd = os.pidfd_open(self.journal_process.pid)
        self.commands.record({'event': 'journal_follow_start', 'argv': argv, 'pid': self.journal_process.pid,
                              'returncode': None, 'stderr_path': str(self.run_dir/'service-journal-stderr.log')})
        self.journal_thread = threading.Thread(target=self._read_service_journal, daemon=True)
        self.journal_thread.start()

    def _read_properties(self):
        try:
            p = self.commands.ssh(['/usr/bin/systemctl', 'show', self.spec.service], timeout=15)
            problems = self.spec.check_service_properties(p.stdout)
            if problems:
                raise RemoteError('service configuration mismatch: '+json.dumps(problems, sort_keys=True))
        except BaseException as exc:
            self.property_error = repr(exc)

    def _snapshot_state(self):
        p = self.commands.ssh(['/usr/bin/python3', self.spec.root+'/bin/snapshot-state.py', self.spec.root], timeout=40)
        state = json.loads(p.stdout)
        if state.get('root') != self.spec.root or state.get('database') != self.spec.db or state.get('open_handles') != []:
            raise RemoteError('stopped database ownership/handle state invalid')
        return state

    def prepare_phase(self, phase):
        if self.state is None or phase not in ('search', 'resumed-search', 'confirmation'):
            raise RemoteError('missing preflight or invalid phase')
        if self.session is None:
            if phase == 'search':
                self._deploy()
            self._start_session()
            self._guard_or_raise()
            self._verify_remote_scripts()
            if phase == 'search':
                self._upload_seed()
        else:
            self._guard_or_raise()
        if phase in ('confirmation', 'resumed-search'):
            expected = self.state.get('transition_evidence', {}).get('source_state')
            if not expected or self._snapshot_state() != expected:
                raise RemoteError('same stopped database could not be proven across transition')
        if phase != 'confirmation':
            rate = self.state.get('plan', {}).get('current', 100)
            if not self.preflight_stage(rate, 'search', self.state).get('ok'):
                raise RemoteError('insufficient resources before service startup')
        self._start_journal()
        self.commands.ssh(self.spec.service_args(phase), timeout=30)
        self.property_error = None
        self.property_started = time.monotonic()
        self.property_thread = threading.Thread(target=self._read_properties, daemon=True)
        self.property_thread.start()
        active_until = time.monotonic()+15
        while True:
            self._guard_or_raise()
            row = self._last_remote_sample()
            if row.get('guard', {}).get('units', {}).get(self.spec.service) == 'active':
                self.service_expected = True
                break
            if time.monotonic() >= active_until:
                raise RemoteError('service did not become active')
            time.sleep(.2)
        # Health, not readiness or an empty maintenance queue, gates load.
        health_until = time.monotonic()+420
        while True:
            self._guard_or_raise()
            try:
                with urllib.request.urlopen('http://127.0.0.1:14210/healthz', timeout=2) as r:
                    if r.status == 200:
                        return
            except (OSError, TimeoutError):
                pass
            if time.monotonic() >= health_until:
                raise RemoteError('service health timeout')
            time.sleep(.25)

    def _driver_liveness(self):
        # Called by a separate local thread even while assessment or other
        # controller code is blocked; it performs no network I/O.
        if self.journal_error:
            return 'service journal observation failed: '+str(self.journal_error)
        if self.property_error:
            return 'service properties failed: '+str(self.property_error)
        if self.session is None or self.session.poll() is not None:
            return 'SSH/watch session exited'
        if time.monotonic()-self.last_fast_guard < 1:
            return None
        self.last_fast_guard = time.monotonic()
        try:
            row = self._last_remote_sample()
            guard = row.get('guard', {})
            local_free = shutil.disk_usage(self.local).free
            memory = next(int(line.split()[1])*1024 for line in Path('/proc/meminfo').read_text().splitlines()
                          if line.startswith('MemAvailable:'))
            if local_free < 4*(1 << 30) or memory < 512*(1 << 20):
                return 'local memory or disk safety reserve exhausted'
            journal_limit = int(self.cfg.get('max_journal_bytes', 4 << 30))
            margin = min(64*(1 << 20), journal_limit//4)
            if self.journal.exists() and self.journal.stat().st_size >= journal_limit-margin:
                return 'journal is approaching its local safety bound'
            if guard.get('disk_free_gib') is None or guard['disk_free_gib'] < 10:
                return 'remote disk reserve below 10GiB'
            if guard.get('marker') or guard.get('watch_stop_reason'):
                return 'remote host protection'
            if guard.get('units', {}).get(self.spec.watcher) != 'active' or guard.get('watch_age_s') is None or guard['watch_age_s'] > 8:
                return 'remote watchdog unavailable or stale'
            if self.service_expected and guard.get('units', {}).get(self.spec.service) != 'active':
                return 'isolated service stopped'
        except BaseException as exc:
            return 'remote monitor unavailable: '+repr(exc)
        return None

    def observe_sample(self, sample):
        inflight = sample.get('in_flight', 0)
        written = sample.get('written_unfinished', 0)
        now = sample.get('time_unix_ns', 0)
        limit = int(self.cfg.get('max_in_flight', 4096))
        if sample.get('target_rate', 0) > 0 and inflight >= .9*limit and written < .5*inflight:
            if self.unwritten_since is None:
                self.unwritten_since = now
            elif now-self.unwritten_since >= 60_000_000_000:
                return 'sustained local connection/write waiting at the in-flight bound'
        else:
            self.unwritten_since = None
        return None

    def _check_retry_manifest(self):
        if not self.cfg.get('retry_migration'):
            if self.cfg.get('pending_migration'):
                raise RemoteError('pending migration requires the reviewed retry migration')
            return
        checkpoint = (self.state or {}).get('reviewed_checkpoint', {})
        path = Path(self.cfg['retry_migration'])
        if (path.is_symlink() or not path.is_file() or not checkpoint.get('retry_manifest_sha256') or
                sha256_file(path) != checkpoint['retry_manifest_sha256'] or
                json.loads(path.read_text()) != checkpoint.get('retry_manifest')):
            raise RemoteError('reviewed retry manifest changed or is not bound to budget')
        if self.cfg.get('pending_migration'):
            path = Path(self.cfg['pending_migration'])
            if (path.is_symlink() or not path.is_file() or not checkpoint.get('pending_manifest_sha256') or
                    sha256_file(path) != checkpoint['pending_manifest_sha256'] or
                    json.loads(path.read_text()) != checkpoint.get('pending_manifest')):
                raise RemoteError('reviewed pending manifest changed or is not bound to budget')

    def start_driver(self, phase, deadline, resume):
        self._check_retry_manifest()
        self.phase_index += 1
        self.cpu_checks['generator'] = SustainedCPUCheck()
        self.cpu_checks['local_combined'] = SustainedCPUCheck()
        self.unwritten_since = None
        self.driver = LocalDriver(self.binary, self.fixture, self.catalog, self.journal,
                                  self.run_dir/('driver-%02d-%s' % (self.phase_index, phase)),
                                  'http://127.0.0.1:14210', resume=resume,
                                  max_pending=int(self.cfg.get('max_pending', 200000)),
                                  max_in_flight=int(self.cfg.get('max_in_flight', 4096)),
                                  max_journal_bytes=int(self.cfg.get('max_journal_bytes', 4 << 30)),
                                  max_attempts=int(self.cfg.get('max_attempts', 30)),
                                  retry_migration=self.cfg.get('retry_migration'),
                                  pending_migration=self.cfg.get('pending_migration'),
                                  external_guard=self._driver_liveness)
        self.driver.start(deadline)
        return self.driver

    def _cleanup_command(self, command, timeout=12):
        argv = self.commands.ssh_base+[command]
        try:
            p = subprocess.run(argv, capture_output=True, text=True, timeout=timeout)
        except BaseException as exc:
            try:
                self.commands.record({'event': 'cleanup_command', 'argv': argv, 'returncode': None, 'stderr': repr(exc)})
            except OSError as log_exc:
                self.cleanup_log_error = repr(log_exc)
            raise
        try:
            self.commands.record({'event': 'cleanup_command', 'argv': argv, 'returncode': p.returncode,
                                  'stdout': p.stdout, 'stderr': p.stderr})
        except OSError as exc:
            self.cleanup_log_error = repr(exc)
        if p.returncode:
            raise RemoteError('cleanup SSH returned '+str(p.returncode))
        return p.stdout

    def _stop_service(self, *, require_active=False):
        # Capture final cgroup/service state before a transient unit is
        # collected. Even failed readback must not skip an owned-unit stop.
        before, read_error = {}, None
        try:
            raw = self._cleanup_command('systemctl show '+shlex.quote(self.spec.service)+
                                        ' -p ActiveState -p Result -p ExecMainStatus -p NRestarts -p MemoryPeak -p CPUUsageNSec')
            before = dict(line.split('=', 1) for line in raw.splitlines() if '=' in line)
        except BaseException as exc:
            read_error = exc
        self.service_expected = False
        stop_owned_unit(self.spec, self._cleanup_command, self.spec.service,
                        self.spec.owner+'-service', service=True)
        if read_error is not None:
            raise RemoteError('service stop readback failed: '+repr(read_error))
        if require_active and (before.get('ActiveState') != 'active' or before.get('Result') != 'success'):
            raise RemoteError('service was not healthy at planned stop: '+repr(before))
        if self.cleanup_log_error:
            raise RemoteError('service stopped but cleanup evidence failed: '+self.cleanup_log_error)

    def _copy_remote(self, source, target, label, timeout=900):
        self.transfer_index += 1
        argv = ['rtk', 'proxy', 'scp', '-l', '32768', '-o', 'BatchMode=yes',
                '-o', 'ControlMaster=no', '-o', 'ControlPath=none',
                self.cfg['host']+':'+source, str(target)]
        return guarded_command(argv, self.run_dir, label+'-%d' % self.transfer_index,
                               self._guard_or_raise, self.commands.record, timeout=timeout)

    def _snapshot(self, label):
        self._guard_or_raise()
        before = self._snapshot_state()
        # Each invocation has its own evidence namespace. A preserved abort
        # snapshot from an earlier segment must never block or be overwritten
        # by the stopped snapshot for a later segment of the same task.
        dest = self.run_dir/(label+'-snapshot.sqlite')
        if any(Path(str(dest)+suffix).exists() or Path(str(dest)+suffix).is_symlink() for suffix in ('', '-wal', '-shm')):
            raise RemoteError('snapshot destination or sidecar already exists')
        self._copy_remote(self.spec.db, dest, 'snapshot-'+label)
        self._guard_or_raise()
        after = self._snapshot_state()
        if before != after or dest.stat().st_size != before['files'][self.spec.db]['size']:
            raise RemoteError('source changed during stopped snapshot download')
        return {'source_state': before, 'snapshot_path': str(dest), 'snapshot_sha256': sha256_file(dest)}

    def _verify_snapshot(self, label, snapshot):
        self._check_retry_manifest()
        expected = self.run_dir/(label+'-expected.sqlite')
        if any(Path(str(expected)+suffix).exists() or Path(str(expected)+suffix).is_symlink() for suffix in ('', '-wal', '-shm')):
            raise RemoteError('reconstruction database or sidecar already exists')
        self.transfer_index += 1
        argv = [str(self.binary), 'verify-sustained', '--fixture', str(self.fixture),
                '--models', str(self.catalog), '--model-seed', '8675309', '--seed-db', str(self.seed),
                '--db', snapshot['snapshot_path'], '--journal', str(self.journal), '--expected-db', str(expected)]
        if self.cfg.get('retry_migration'):
            argv.extend(['--reviewed-retry-migration', self.cfg['retry_migration']])
        if self.cfg.get('pending_migration'):
            argv.extend(['--reviewed-pending-migration', self.cfg['pending_migration']])
        out, _ = guarded_command(argv, self.run_dir, 'verify-'+label+'-%d' % self.transfer_index,
                                 self._guard_or_raise, self.commands.record, timeout=1800)
        result = json.loads(out.read_text())
        if result.get('produced') is None or result.get('pending') != result['produced']-result['confirmed']:
            raise RemoteError('verification did not return a consistent ledger summary')
        self.transfer_index += 1
        args = [str(self.binary), 'summarize-sustained', '--journal', str(self.journal),
                '--models', str(self.catalog), '--max-pending', str(self.cfg.get('max_pending', 200000))]
        if self.cfg.get('retry_migration'):
            args.extend(['--reviewed-retry-migration', self.cfg['retry_migration']])
        if self.cfg.get('pending_migration'):
            args.extend(['--reviewed-pending-migration', self.cfg['pending_migration']])
        summary_path, _ = guarded_command(args, self.run_dir, 'summary-'+label+'-%d' % self.transfer_index,
                                          self._guard_or_raise, self.commands.record, timeout=1800)
        summary = json.loads(summary_path.read_text())
        if any(summary.get(key) != result.get(key) for key in ('produced', 'confirmed', 'pending')):
            raise RemoteError('streaming statistics disagree with full verification')
        return result, str(summary_path)

    def _capture_service_log(self, label):
        self.transfer_index += 1
        remote = shlex.join(['/usr/bin/journalctl', '--no-pager', '--output=cat', '--unit='+self.spec.service])
        argv = self.commands.ssh_base+[remote]
        out, _ = guarded_command(argv, self.run_dir, 'service-log-'+label+'-%d' % self.transfer_index,
                                 self._guard_or_raise, self.commands.record, timeout=120)
        last_listener = last_stop = -1
        stop_level = None
        with out.open('rb') as f:
            line_no = 0
            while True:
                line = f.readline(1 << 20)
                if not line:
                    break
                line_no += 1
                if len(line) == 1 << 20 and not line.endswith(b'\n'):
                    raise RemoteError('service journal contains an oversized line')
                if b'cache refresh failed' in line:
                    raise RemoteError('service journal records a cache refresh failure')
                try:
                    record = json.loads(line)
                except (ValueError, UnicodeError):
                    continue
                if isinstance(record, dict) and record.get('msg') == 'http listener opened':
                    last_listener = line_no
                if isinstance(record, dict) and record.get('msg') == 'service stopped':
                    last_stop, stop_level = line_no, record.get('level')
        if last_listener < 0 or last_stop <= last_listener or stop_level != 'INFO':
            raise RemoteError('service journal lacks a clean stop after the latest listener')
        return str(out)

    @staticmethod
    def _match_driver_verification(stopped, verification):
        sample = stopped.get('sample', {})
        for key in ('produced', 'confirmed', 'pending'):
            if key not in sample or verification.get(key) != sample[key]:
                raise RemoteError('driver and independent verification disagree on '+key)
        if sample.get('in_flight') != 0:
            raise RemoteError('driver stopped with attempts still in flight')

    def transition(self, stopped):
        self._stop_service(require_active=True)
        service_log = self._capture_service_log('transition')
        snapshot = self._snapshot('transition')
        verification, summary_path = self._verify_snapshot('transition', snapshot)
        self._match_driver_verification(stopped, verification)
        return {'verified': True, **snapshot, 'journal_sha256': sha256_file(self.journal),
                'fixture_sha256': sha256_file(self.fixture), 'verification': verification,
                'summary_path': summary_path, 'service_log': service_log,
                'pending_at_transition': stopped['sample']['pending']}

    def finalize(self, result, stopped):
        self._stop_service(require_active=True)
        service_log = self._capture_service_log('final')
        snapshot = self._snapshot('final')
        verification, summary_path = self._verify_snapshot('final', snapshot)
        self._match_driver_verification(stopped, verification)
        return {'verified': True, **snapshot, 'journal_sha256': sha256_file(self.journal),
                'verification': verification, 'summary_path': summary_path, 'service_log': service_log,
                'pending_at_stop': stopped['sample']['pending']}

    def abort(self, reason):
        # Never invent a verified result from an interrupted or unclean ledger.
        try:
            self.commands.record({'event': 'abort', 'reason': reason})
        except OSError as exc:
            self.cleanup_log_error = repr(exc)
        if self.session is None:
            return
        self._stop_service()
        if self.guard().get('healthy'):
            try:
                self._capture_service_log('abort')
            except BaseException as exc:
                try:
                    self.commands.record({'event': 'abort_service_log_warning', 'error': repr(exc)})
                except OSError:
                    pass
            snapshot = self._snapshot('abort')
            if self.journal.exists():
                try:
                    verification, summary_path = self._verify_snapshot('abort', snapshot)
                    self.commands.record({'event': 'abort_verification', 'verified': True,
                                          'snapshot': snapshot, 'verification': verification,
                                          'summary_path': summary_path})
                except BaseException as exc:
                    self.commands.record({'event': 'abort_verification', 'verified': False,
                                          'snapshot': snapshot, 'error': repr(exc)})

    def _final_readonly_check(self):
        script = Path(__file__).with_name('remote')/'preflight.py'
        args = ['/usr/bin/python3', '-c', script.read_text(), self.spec.root,
                '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/baseline-containers.json',
                self.spec.app, self.spec.catalog, self.spec.web, self.spec.service, self.spec.watcher]
        result = json.loads(self.commands.ssh(args, timeout=35).stdout)
        baseline = hashlib.sha256(json.dumps(result.get('expected_containers', {}), sort_keys=True,
                                             separators=(',', ':')).encode()).hexdigest()
        if (result.get('containers', {}).get('returncode') != 0 or not result.get('containers_match') or
                len(result.get('actual_containers', {})) != 7 or baseline != self.cfg['baseline_containers_sha256']):
            raise RemoteError('original seven-container state changed after cleanup')
        if result.get('task_marker') or result.get('original_marker'):
            raise RemoteHostProtection('a protection marker remained after cleanup')
        for name in (self.spec.service, self.spec.watcher, 'ranking-c54.service', 'ranking-c54-watch.service'):
            if result['units'][name].get('stdout', '').strip() not in ('inactive', 'unknown', 'failed'):
                raise RemoteError('test unit still active after cleanup: '+name)
        if result.get('listener') != self.last_remote.get('listener'):
            raise RemoteError('SSH listener identity or limits changed')
        port = self.commands.ssh(['/usr/bin/ss', '-H', '-ltn', '( sport = :4210 )'], timeout=10)
        if port.stdout.strip():
            raise RemoteError('remote test port still listening')
        local = self.commands.run(['rtk', 'proxy', 'ss', '-H', '-ltn', '( sport = :14210 )'], timeout=10)
        if local.stdout.strip():
            raise RemoteError('local forwarding port still listening')
        ready_path = self.session_dir/'session-ready.json'
        if ready_path.exists():
            ready = json.loads(ready_path.read_text())
            worker = ready['worker']
            code = ('import pathlib,sys; p=pathlib.Path("/proc")/sys.argv[1]; '
                    'print("alive" if p.exists() and int((p/"stat").read_text().rsplit(")",1)[1].split()[19])==int(sys.argv[2]) else "gone")')
            p = self.commands.ssh(['/usr/bin/python3', '-c', code, str(worker['pid']), str(worker['start_ticks'])], timeout=10)
            if p.stdout.strip() != 'gone':
                raise RemoteError('owned SSH worker has not exited')
        return result

    def _copy_final_watch(self):
        source = self.spec.root+'/logs/watch.jsonl'
        code = ('import json,os,sys;s=os.stat(sys.argv[1]);'
                'print(json.dumps({"size":s.st_size,"mtime_ns":s.st_mtime_ns,"inode":s.st_ino}))')
        before = json.loads(self.commands.ssh(['/usr/bin/python3', '-c', code, source], timeout=10).stdout)
        size = before['size']
        if size > 32*(1 << 20):
            raise RemoteError('watch log is too large for unguarded post-stop transfer')
        dest = self.run_dir/'watch-final.jsonl'
        if dest.exists():
            raise RemoteError('watch log destination exists')
        self.commands.run(['rtk', 'proxy', 'scp', '-l', '32768', '-o', 'BatchMode=yes',
                           '-o', 'ControlMaster=no', '-o', 'ControlPath=none',
                           self.cfg['host']+':'+source, str(dest)], timeout=120)
        after = json.loads(self.commands.ssh(['/usr/bin/python3', '-c', code, source], timeout=10).stdout)
        if dest.stat().st_size != size or before != after:
            raise RemoteError('final watch log changed or transfer size mismatched')

    def cleanup(self):
        errors = []
        self.journal_stop.set()
        if self.journal_process is not None:
            try:
                if self.journal_process.poll() is None:
                    signal.pidfd_send_signal(self.journal_pidfd, signal.SIGTERM)
                    try:
                        self.journal_process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        signal.pidfd_send_signal(self.journal_pidfd, signal.SIGKILL)
                        self.journal_process.wait(timeout=5)
                if self.journal_thread:
                    self.journal_thread.join(timeout=3)
                self.commands.record({'event': 'journal_follow_exit', 'returncode': self.journal_process.returncode,
                                      'stderr_path': str(self.run_dir/'service-journal-stderr.log')})
            except BaseException as exc:
                errors.append('journal follower cleanup: '+repr(exc))
            finally:
                if self.journal_process.stdout:
                    self.journal_process.stdout.close()
                self.journal_stderr.close()
                if self.journal_pidfd is not None:
                    os.close(self.journal_pidfd)
                    self.journal_pidfd = None
        if self.session is not None:
            try:
                try:
                    (self.session_dir/'stop-requested').write_text('stop\n')
                except OSError as exc:
                    errors.append('session stop request evidence: '+repr(exc))
                    if self.session.poll() is None and self.session_pidfd is not None:
                        signal.pidfd_send_signal(self.session_pidfd, signal.SIGTERM)
                try:
                    self.session.wait(timeout=65)
                except subprocess.TimeoutExpired:
                    signal.pidfd_send_signal(self.session_pidfd, signal.SIGTERM)
                    try:
                        self.session.wait(timeout=35)
                    except subprocess.TimeoutExpired:
                        signal.pidfd_send_signal(self.session_pidfd, signal.SIGKILL)
                        self.session.wait(timeout=5)
                        raise RemoteError('session was killed; remote cleanup is unproven')
                self.commands.record({'event': 'session_exit', 'returncode': self.session.returncode,
                                      'stderr_path': str(self.session_dir/'session-stderr.log')})
                if self.session.returncode:
                    raise RemoteError('SSH/watch session cleanup returned nonzero')
            except BaseException as exc:
                errors.append('session cleanup: '+repr(exc))
            finally:
                if self.session_stderr:
                    self.session_stderr.close()
                if self.session_pidfd is not None:
                    os.close(self.session_pidfd)
                    self.session_pidfd = None
            try:
                self._final_readonly_check()
            except BaseException as exc:
                errors.append('final independent readback: '+repr(exc))
            try:
                self._copy_final_watch()
            except BaseException as exc:
                errors.append('final watch evidence: '+repr(exc))
        if errors:
            raise RemoteError('; '.join(errors))
