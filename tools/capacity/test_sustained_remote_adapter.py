import hashlib
import json
import os
import tempfile
import time
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from sustained_remote_adapter import RemoteAdapter, RemoteError
from sustained_resource import GIB


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


class FakeCommands:
    def __init__(self, remote):
        self.remote = remote
        self.ssh_base = ['mock-independent-ssh']
        self.stage_day = time.strftime('%Y-%m-%d', time.gmtime())
        self.calls = []

    def ssh(self, argv, **kwargs):
        self.calls.append(('ssh', argv))
        if argv[2].startswith('import json,shutil,sys,time;'):
            return SimpleNamespace(stdout=json.dumps({'free': 14*GIB, 'day': self.stage_day}), stderr='', returncode=0)
        return SimpleNamespace(stdout=json.dumps(self.remote), stderr='', returncode=0)

    def run(self, argv, **kwargs):
        raise AssertionError('pre-existing valid fixture should not be recreated')

    def record(self, row):
        self.calls.append(('record', row))


class RemotePreflightTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.root = Path(self.temp.name)
        self.root.chmod(0o700)
        binary, catalog = self.root/'binary', self.root/'models'
        binary.write_bytes(b'test binary')
        catalog.write_bytes(b'test catalog')
        fixture, seed = self.root/'fixture.json', self.root/'seed.sqlite'
        self.day = time.strftime('%Y-%m-%d', time.gmtime())
        fixture.write_text(json.dumps({'day': self.day, 'clients': 10000, 'history_days': 90,
                                       'backlog': 10000, 'seed': 'private-test-only'}))
        seed.write_bytes(b'seed placeholder')
        token = '0123456789abcdef'
        self.config = {'host': 'lax-racknerd', 'local_task_dir': str(self.root), 'task_id': token,
                       'remote_root': '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/sustained-20261113-r1/task-'+token,
                       'service': 'ranking-c54-'+token+'.service', 'watch': 'ranking-c54-'+token+'-watch.service',
                       'application_remote': '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/bin/ranking',
                       'catalog_remote': '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/bin/models.json',
                       'web_remote': '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/web-dist',
                       'capacity_binary': str(binary), 'catalog_local': str(catalog),
                       'capacity_sha256': digest(binary), 'catalog_sha256': digest(catalog),
                       'application_sha256': 'app-hash'}
        containers = {str(x): [] for x in range(7)}
        self.config['baseline_containers_sha256'] = hashlib.sha256(json.dumps(containers, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
        manifest = {'day': self.day, 'model_seed': 8675309, 'clients': 10000,
                    'fixture_sha256': digest(fixture), 'seed_sha256': digest(seed),
                    'capacity_sha256': digest(binary), 'catalog_sha256': digest(catalog)}
        (self.root/'seed-manifest.json').write_text(json.dumps(manifest))
        units = {u: {'stdout': 'inactive\n'} for u in (self.config['service'], self.config['watch'],
                                                       'ranking-c54.service', 'ranking-c54-watch.service')}
        self.remote = {'utc_day': self.day, 'containers_match': True, 'containers': {'returncode': 0},
                       'actual_containers': containers,
                       'expected_containers': containers,
                       'original_marker': False, 'task_marker': False, 'port': {'returncode': 0, 'stdout': ''},
                       'units': units, 'hashes': {'application': 'app-hash', 'models': digest(catalog)},
                       'listener': {'comm': 'sshd'}, 'memory_available_mib': 1000,
                       'memory_psi': {'full': 0}, 'io_psi': {'full': 0}, 'disk_free_bytes': 14*GIB, 'root_exists': False}
        self.fake = FakeCommands(self.remote)

    def tearDown(self):
        self.temp.cleanup()

    def test_readonly_preflight_preserves_original_and_uses_independent_ssh(self):
        adapter = RemoteAdapter(self.config, self.fake)
        result = adapter.preflight({'status': 'new'})
        self.assertTrue(result['verified'])
        self.assertEqual(len(self.fake.calls), 1)
        command = self.fake.calls[0][1]
        self.assertEqual(command[:2], ['/usr/bin/python3', '-c'])
        self.assertIn('Read-only C54 host preflight', command[2])
        self.assertFalse((self.root/'sustained.jsonl.gz').exists())

    def test_preflight_rejects_foreign_marker_hash_or_root(self):
        for key, value in [('task_marker', True), ('root_exists', True), ('containers_match', False)]:
            with self.subTest(key=key):
                original = self.remote[key]
                self.remote[key] = value
                with self.assertRaises(RemoteError):
                    RemoteAdapter(self.config, self.fake).preflight({'status': 'new'})
                self.remote[key] = original
        self.remote['hashes']['application'] = 'unexpected'
        with self.assertRaises(RemoteError):
            RemoteAdapter(self.config, self.fake).preflight({'status': 'new'})

    def test_seed_and_journal_change_fail_closed(self):
        (self.root/'seed.sqlite').write_bytes(b'changed')
        with self.assertRaisesRegex(RemoteError, 'mismatch'):
            RemoteAdapter(self.config, self.fake).preflight({'status': 'new'})
        with self.assertRaisesRegex(RemoteError, 'journal'):
            RemoteAdapter(self.config, self.fake).preflight({'status': 'transition_ready', 'transition_evidence': {}})

    def test_utc_rollover_stops_before_stage(self):
        adapter = RemoteAdapter(self.config, self.fake)
        adapter.preflight({'status': 'new'})
        self.fake.stage_day = '1900-01-01'
        result = adapter.preflight_stage(100, 'search', {'status': 'new'})
        self.assertFalse(result['ok'])
        self.assertIn('utc_day_changed', result['issues'])

    def test_stage_forecast_credits_only_the_owned_active_driver(self):
        adapter = RemoteAdapter(self.config, self.fake)
        adapter.preflight({'status': 'new'})
        idle = adapter.preflight_stage(100, 'search', {'status': 'new'})
        self.assertEqual(idle['estimates']['driver_owned_anon_bytes'], 0)
        resident = mock.Mock(return_value=384 << 20)
        adapter.driver = SimpleNamespace(resident_anon_bytes=resident, proc=SimpleNamespace(poll=lambda: None))
        running = adapter.preflight_stage(100, 'search', {'status': 'new'})
        resident.assert_called_once_with()
        self.assertEqual(running['estimates']['driver_owned_anon_bytes'], 384 << 20)
        self.assertEqual(idle['estimates']['memory_need_bytes'], running['estimates']['memory_need_bytes'])
        resident.side_effect = RuntimeError('memory identity unavailable')
        with self.assertRaisesRegex(RuntimeError, 'memory identity unavailable'):
            adapter.preflight_stage(100, 'search', {'status': 'new'})
        adapter.driver.proc.poll = lambda: 0
        stopped = adapter.preflight_stage(100, 'search', {'status': 'new'})
        self.assertEqual(stopped['estimates']['driver_owned_anon_bytes'], 0)

    def test_guard_requires_fresh_owned_worker_and_real_cache_observation(self):
        adapter = RemoteAdapter(self.config, self.fake)
        adapter.session = SimpleNamespace(poll=lambda: None)
        adapter.session_dir = self.root/'fake-session'
        adapter.session_dir.mkdir()
        worker = {'pid': 123, 'ppid': 122, 'start_ticks': 456, 'nofile': [32768, 65536],
                  'fd': 10, 'cpu_ticks': 1}
        adapter.session_ready = {'worker': dict(worker), 'adjustment': {'after': [{'nofile': [32768, 65536]}]}}
        row = {'observed_local_unix_ns': time.time_ns(), 'worker': worker, 'clock_ticks_per_second': 100,
               'guard': {'units': {adapter.spec.watcher: 'active', adapter.spec.service: 'active'},
                         'watch_age_s': 1, 'disk_free_gib': 14, 'cache_generated_at': 'real-generation'}}
        path = adapter.session_dir/'remote-samples.jsonl'
        path.write_text(json.dumps(row)+'\n')
        adapter.service_expected = True
        result = adapter.guard()
        self.assertTrue(result['healthy'], result)
        self.assertEqual(result['cache_generation'], 'real-generation')
        self.assertEqual(result['observed_unix_ns'], row['observed_local_unix_ns'])
        row['worker']['start_ticks'] += 1
        path.write_text(json.dumps(row)+'\n')
        self.assertFalse(adapter.guard()['healthy'])
        row['worker']['start_ticks'] -= 1
        row['guard']['watch_stop_reason'] = 'host memory protection'
        path.write_text(json.dumps(row)+'\n')
        self.assertTrue(adapter.guard()['host_protection'])
        row['guard']['watch_stop_reason'] = None
        row['observed_local_unix_ns'] -= 10_000_000_000
        path.write_text(json.dumps(row)+'\n')
        self.assertFalse(adapter.guard()['healthy'])

    def test_ssh_cpu_is_diagnostic_but_fd_limit_still_stops(self):
        adapter = RemoteAdapter(self.config, self.fake)
        adapter.session = SimpleNamespace(poll=lambda: None)
        adapter.session_dir = self.root/'cpu-session'
        adapter.session_dir.mkdir()
        worker = {'pid': 123, 'ppid': 122, 'start_ticks': 456, 'nofile': [32768, 65536],
                  'fd': 10, 'cpu_ticks': 9999}
        adapter.session_ready = {'worker': dict(worker), 'adjustment': {'after': [{'nofile': [32768, 65536]}]}}
        row = {'observed_local_unix_ns': time.time_ns(), 'worker': worker, 'clock_ticks_per_second': 100,
               'guard': {'units': {adapter.spec.watcher: 'active', adapter.spec.service: 'active'},
                         'watch_age_s': 1, 'disk_free_gib': 14}}
        (adapter.session_dir/'remote-samples.jsonl').write_text(json.dumps(row)+'\n')
        adapter.service_expected = True
        adapter.cpu_checks['remote_ssh'].observe = mock.Mock(return_value=True)
        result = adapter.guard()
        self.assertTrue(result['healthy'], result)
        self.assertNotIn('tool_failure', result)
        self.assertIn('remote_ssh', (adapter.run_dir/'cpu-warnings.jsonl').read_text())
        row['worker']['fd'] = 32768
        (adapter.session_dir/'remote-samples.jsonl').write_text(json.dumps(row)+'\n')
        self.assertIn('file-descriptor', adapter.guard()['tool_failure'])

    def test_generator_cpu_tool_limits_remain_while_local_ssh_is_diagnostic(self):
        adapter = RemoteAdapter(self.config, self.fake)
        adapter.session = SimpleNamespace(poll=lambda: None)
        adapter.session_dir = self.root/'local-cpu-session'
        adapter.session_dir.mkdir()
        worker = {'pid': 123, 'ppid': 122, 'start_ticks': 456, 'nofile': [32768, 65536],
                  'fd': 10, 'cpu_ticks': 1}
        own_pid = os.getpid()
        own_ticks = int((Path('/proc')/str(own_pid)/'stat').read_text().rsplit(')', 1)[1].split()[19])
        adapter.session_ready = {'worker': dict(worker), 'adjustment': {'after': [{'nofile': [32768, 65536]}]},
                                 'master': {'pid': own_pid, 'start_ticks': own_ticks}}
        row = {'observed_local_unix_ns': time.time_ns(), 'worker': worker, 'clock_ticks_per_second': 100,
               'guard': {'units': {adapter.spec.watcher: 'active', adapter.spec.service: 'active'},
                         'watch_age_s': 1, 'disk_free_gib': 14}}
        (adapter.session_dir/'remote-samples.jsonl').write_text(json.dumps(row)+'\n')
        adapter.service_expected = True
        adapter.driver = SimpleNamespace(proc=SimpleNamespace(pid=own_pid, poll=lambda: None))
        adapter.cpu_checks['generator'].observe = mock.Mock(return_value=True)
        self.assertIn('local generator sustained', adapter.guard()['tool_failure'])
        adapter.cpu_checks['generator'].observe.return_value = False
        adapter.cpu_checks['local_combined'].observe = mock.Mock(return_value=True)
        self.assertIn('generator and local SSH sustained', adapter.guard()['tool_failure'])
        adapter.cpu_checks['local_combined'].observe.return_value = False
        adapter.cpu_checks['local_ssh'].observe = mock.Mock(return_value=True)
        result = adapter.guard()
        self.assertTrue(result['healthy'])
        self.assertNotIn('tool_failure', result)
        self.assertIn('local_ssh', (adapter.run_dir/'cpu-warnings.jsonl').read_text())

    def test_deploy_handles_absent_private_sustained_parent(self):
        adapter = RemoteAdapter(self.config, self.fake)
        with mock.patch.object(self.fake, 'run', return_value=SimpleNamespace(returncode=0, stdout='', stderr='')) as run:
            adapter._deploy()
        calls = [args for kind, args in self.fake.calls if kind == 'ssh']
        self.assertIn('os.mkdir(p,0o700)', calls[0][2])
        self.assertEqual(calls[0][-1], str(Path(adapter.spec.root).parent))
        self.assertEqual(calls[1][:4], ['/bin/mkdir', '-m', '700', '--'])
        self.assertIn('scp', run.call_args.args[0])

    def test_prepare_phase_mock_drives_deploy_health_and_owned_unit(self):
        adapter = RemoteAdapter(self.config, self.fake)
        adapter.state = {'status': 'new'}
        def started():
            adapter.session = SimpleNamespace(poll=lambda: None)
        def ssh(argv, **kwargs):
            self.fake.calls.append(('ssh', argv))
            return SimpleNamespace(stdout='', stderr='', returncode=0)
        self.fake.ssh = ssh
        active = {'guard': {'units': {adapter.spec.service: 'active'}}}
        response = mock.MagicMock()
        response.__enter__.return_value.status = 200
        with mock.patch.object(adapter, '_deploy') as deploy, \
             mock.patch.object(adapter, '_start_session', side_effect=started), \
             mock.patch.object(adapter, '_guard_or_raise'), \
             mock.patch.object(adapter, '_verify_remote_scripts') as verify_scripts, \
             mock.patch.object(adapter, '_upload_seed') as upload, \
             mock.patch.object(adapter, '_start_journal'), \
             mock.patch.object(adapter, '_read_properties'), \
             mock.patch.object(adapter, 'preflight_stage', return_value={'ok': True}), \
             mock.patch.object(adapter, '_last_remote_sample', return_value=active), \
             mock.patch('urllib.request.urlopen', return_value=response):
            adapter.prepare_phase('search')
        adapter.property_thread.join(timeout=2)
        deploy.assert_called_once()
        upload.assert_called_once()
        verify_scripts.assert_called_once()
        sent = [args for kind, args in self.fake.calls if kind == 'ssh']
        self.assertEqual(len(sent), 1)
        self.assertIn('--unit='+adapter.spec.service.removesuffix('.service'), sent[0])
        self.assertTrue(adapter.service_expected)

    def test_service_log_must_show_clean_latest_stop_and_no_cache_failure(self):
        adapter = RemoteAdapter(self.config, self.fake)
        path = self.root/'mock-service.log'
        def rows(messages):
            path.write_text(''.join(json.dumps({'level': level, 'msg': msg})+'\n' for level, msg in messages))
        with mock.patch('sustained_remote_adapter.guarded_command', return_value=(path, self.root/'stderr')):
            rows([('INFO', 'http listener opened'), ('INFO', 'service stopped')])
            self.assertEqual(adapter._capture_service_log('good'), str(path))
            rows([('INFO', 'service stopped'), ('INFO', 'http listener opened')])
            with self.assertRaisesRegex(RemoteError, 'lacks a clean stop'):
                adapter._capture_service_log('incomplete')
            rows([('INFO', 'http listener opened'), ('ERROR', 'service stopped')])
            with self.assertRaisesRegex(RemoteError, 'lacks a clean stop'):
                adapter._capture_service_log('error')
            rows([('INFO', 'http listener opened'), ('ERROR', 'cache refresh failed'), ('INFO', 'service stopped')])
            with self.assertRaisesRegex(RemoteError, 'cache refresh failure'):
                adapter._capture_service_log('cache-error')

    def test_snapshot_requires_stopped_unchanged_source_and_exact_size(self):
        adapter = RemoteAdapter(self.config, self.fake)
        source = {'files': {adapter.spec.db: {'size': 4, 'inode': 22, 'mtime_ns': 33}}}
        def copy(source_path, target, label):
            Path(target).write_bytes(b'data')
        with mock.patch.object(adapter, '_guard_or_raise'), \
             mock.patch.object(adapter, '_snapshot_state', side_effect=[source, source]), \
             mock.patch.object(adapter, '_copy_remote', side_effect=copy):
            result = adapter._snapshot('good')
        self.assertEqual(result['source_state'], source)
        self.assertEqual(result['snapshot_sha256'], hashlib.sha256(b'data').hexdigest())
        changed = {'files': {adapter.spec.db: {'size': 4, 'inode': 23, 'mtime_ns': 33}}}
        with mock.patch.object(adapter, '_guard_or_raise'), \
             mock.patch.object(adapter, '_snapshot_state', side_effect=[source, changed]), \
             mock.patch.object(adapter, '_copy_remote', side_effect=copy):
            with self.assertRaisesRegex(RemoteError, 'source changed'):
                adapter._snapshot('changed')
        # The partial evidence is preserved, not silently removed.
        self.assertTrue((adapter.run_dir/'changed-snapshot.sqlite').exists())

    def test_snapshots_are_separate_per_invocation_and_never_overwrite(self):
        old = self.root/'abort-snapshot.sqlite'
        old.write_bytes(b'old audited snapshot')
        adapter = RemoteAdapter(self.config, self.fake)
        source = {'files': {adapter.spec.db: {'size': 4, 'inode': 22, 'mtime_ns': 33}}}
        def copy(source_path, target, label):
            Path(target).write_bytes(b'data')
        with mock.patch.object(adapter, '_guard_or_raise'), \
             mock.patch.object(adapter, '_snapshot_state', side_effect=[source, source]), \
             mock.patch.object(adapter, '_copy_remote', side_effect=copy):
            result = adapter._snapshot('abort')
        self.assertEqual(Path(result['snapshot_path']), adapter.run_dir/'abort-snapshot.sqlite')
        self.assertEqual(old.read_bytes(), b'old audited snapshot')
        with mock.patch.object(adapter, '_guard_or_raise'), \
             mock.patch.object(adapter, '_snapshot_state', return_value=source):
            with self.assertRaisesRegex(RemoteError, 'destination or sidecar already exists'):
                adapter._snapshot('abort')
        self.assertEqual((adapter.run_dir/'abort-snapshot.sqlite').read_bytes(), b'data')

    def test_reconstruction_path_is_separate_per_invocation(self):
        adapter = RemoteAdapter(self.config, self.fake)
        old = self.root/'abort-expected.sqlite'
        old.write_bytes(b'old reconstruction')
        snapshot = {'snapshot_path': 'mock-snapshot'}
        responses = iter([{'produced': 12, 'confirmed': 10, 'pending': 2},
                          {'produced': 12, 'confirmed': 10, 'pending': 2}])
        def run(argv, *args, **kwargs):
            if argv[1] == 'verify-sustained':
                self.assertEqual(argv[argv.index('--expected-db')+1], str(adapter.run_dir/'abort-expected.sqlite'))
            out = adapter.run_dir/('mock-output-%s.json' % argv[1])
            out.write_text(json.dumps(next(responses)))
            return out, adapter.run_dir/'mock-stderr'
        with mock.patch('sustained_remote_adapter.guarded_command', side_effect=run):
            result, _ = adapter._verify_snapshot('abort', snapshot)
        self.assertEqual(result['pending'], 2)
        self.assertEqual(old.read_bytes(), b'old reconstruction')

    def test_retry_manifest_is_bound_for_driver_and_both_verifiers(self):
        adapter = RemoteAdapter(self.config, self.fake)
        manifest = self.root/'retry-manifest.json'
        payload = {'approval': 'explicit-test'}
        manifest.write_text(json.dumps(payload))
        adapter.cfg['retry_migration'] = str(manifest)
        pending = self.root/'pending-manifest.json'
        pending_payload = {'approval': 'explicit-pending-test'}
        pending.write_text(json.dumps(pending_payload))
        adapter.cfg['pending_migration'] = str(pending)
        adapter.state = {'reviewed_checkpoint': {'retry_manifest_sha256': digest(manifest), 'retry_manifest': payload,
                                                'pending_manifest_sha256': digest(pending), 'pending_manifest': pending_payload}}
        with mock.patch('sustained_remote_adapter.LocalDriver') as driver:
            adapter.start_driver('confirmation', 1000, True)
            self.assertEqual(driver.call_args.kwargs['pending_migration'], str(pending))
        responses = iter([{'produced': 12, 'confirmed': 10, 'pending': 2},
                          {'produced': 12, 'confirmed': 10, 'pending': 2}])
        commands = []
        def run(argv, *args, **kwargs):
            commands.append(argv)
            out = adapter.run_dir/('mock-'+argv[1]+'.json')
            out.write_text(json.dumps(next(responses)))
            return out, adapter.run_dir/'mock-stderr'
        with mock.patch('sustained_remote_adapter.guarded_command', side_effect=run):
            result, _ = adapter._verify_snapshot('abort', {'snapshot_path': 'mock-snapshot'})
        self.assertEqual(result['pending'], 2)
        self.assertEqual(len(commands), 2)
        for argv in commands:
            self.assertEqual(argv[argv.index('--reviewed-retry-migration')+1], str(manifest))
            self.assertEqual(argv[argv.index('--reviewed-pending-migration')+1], str(pending))
        pending.write_text(json.dumps({'approval': 'changed'}))
        with self.assertRaisesRegex(RemoteError, 'pending manifest changed'):
            adapter.start_driver('confirmation', 1000, True)
        pending.write_text(json.dumps(pending_payload))
        manifest.write_text(json.dumps({'approval': 'changed'}))
        with self.assertRaisesRegex(RemoteError, 'manifest changed'):
            adapter._verify_snapshot('changed', {'snapshot_path': 'mock'})
        with self.assertRaisesRegex(RemoteError, 'manifest changed'):
            adapter.start_driver('search', 1000, True)

    def test_transition_preserves_same_database_and_verifies_ledger(self):
        adapter = RemoteAdapter(self.config, self.fake)
        adapter.journal.write_bytes(b'closed mock journal')
        stopped = {'sample': {'produced': 12, 'confirmed': 10, 'pending': 2, 'in_flight': 0}}
        verification = {'produced': 12, 'confirmed': 10, 'pending': 2}
        snapshot = {'source_state': {'same': 'database'}, 'snapshot_path': 'mock-snapshot', 'snapshot_sha256': 'hash'}
        with mock.patch.object(adapter, '_stop_service') as stop, \
             mock.patch.object(adapter, '_capture_service_log', return_value='mock-log'), \
             mock.patch.object(adapter, '_snapshot', return_value=snapshot), \
             mock.patch.object(adapter, '_verify_snapshot', return_value=(verification, 'mock-summary')):
            evidence = adapter.transition(stopped)
        stop.assert_called_once()
        self.assertTrue(evidence['verified'])
        self.assertEqual(evidence['source_state'], snapshot['source_state'])
        self.assertEqual(evidence['pending_at_transition'], 2)
        self.assertEqual(evidence['journal_sha256'], digest(adapter.journal))
        with mock.patch.object(adapter, '_stop_service'), \
             mock.patch.object(adapter, '_capture_service_log', return_value='mock-log'), \
             mock.patch.object(adapter, '_snapshot', return_value=snapshot), \
             mock.patch.object(adapter, '_verify_snapshot', return_value=({'produced': 12, 'confirmed': 11, 'pending': 1}, 'summary')):
            with self.assertRaises(RemoteError):
                adapter.transition(stopped)

    def test_independent_liveness_stops_before_journal_bound(self):
        adapter = RemoteAdapter(self.config, self.fake)
        adapter.session = SimpleNamespace(poll=lambda: None)
        adapter.cfg['max_journal_bytes'] = 128 << 20
        with adapter.journal.open('wb') as out:
            out.truncate(100 << 20)  # sparse local test file; no 100MiB allocation
        row = {'guard': {'disk_free_gib': 14, 'units': {adapter.spec.watcher: 'active'}, 'watch_age_s': 1}}
        with mock.patch.object(adapter, '_last_remote_sample', return_value=row):
            self.assertIn('approaching', adapter._driver_liveness())

    def test_sustained_unwritten_inflight_is_tool_limited(self):
        adapter = RemoteAdapter(self.config, self.fake)
        first = {'time_unix_ns': 1, 'target_rate': 100, 'in_flight': 4096, 'written_unfinished': 100}
        self.assertIsNone(adapter.observe_sample(first))
        second = dict(first, time_unix_ns=60_000_000_001)
        self.assertIn('connection/write', adapter.observe_sample(second))
        self.assertIsNone(adapter.observe_sample(dict(second, in_flight=10)))

    @unittest.skipUnless(os.environ.get('CAPACITY_TEST_BIN'), 'requires real Go assessment bridge')
    def test_controller_and_remote_adapter_lifecycle_with_mocked_boundary(self):
        from sustained_controller import Controller, GoAssessment
        from sustained_state import Budget
        from test_sustained_controller import Clock, FakeAdapter
        clock = Clock()
        fake_load = FakeAdapter(clock)
        adapter = RemoteAdapter(self.config, self.fake)
        adapter.journal.write_bytes(b'mock journal, no remote calls')
        budget = Budget(self.root/'budget.json', 'mock-config', clock, lambda: 'mock-boot')
        assessment = GoAssessment(os.environ['CAPACITY_TEST_BIN'], self.root/'assess-log.jsonl')
        snapshot = {'source_state': {'inode': 99}, 'snapshot_path': 'mock-snapshot', 'snapshot_sha256': 'mock'}
        def verify(label, state):
            s = fake_load.sample
            return ({'produced': s['produced'], 'confirmed': s['confirmed'],
                     'pending': s['produced']-s['confirmed']}, 'mock-summary')
        try:
            with mock.patch.object(adapter, 'preflight', side_effect=fake_load.preflight), \
                 mock.patch.object(adapter, 'preflight_stage', side_effect=fake_load.preflight_stage), \
                 mock.patch.object(adapter, 'prepare_phase', side_effect=fake_load.prepare_phase), \
                 mock.patch.object(adapter, 'start_driver', side_effect=fake_load.start_driver), \
                 mock.patch.object(adapter, 'guard', side_effect=fake_load.guard), \
                 mock.patch.object(adapter, '_stop_service'), \
                 mock.patch.object(adapter, '_capture_service_log', return_value='mock-log'), \
                 mock.patch.object(adapter, '_snapshot', return_value=snapshot), \
                 mock.patch.object(adapter, '_verify_snapshot', side_effect=verify), \
                 mock.patch.object(adapter, 'cleanup', side_effect=fake_load.cleanup), \
                 mock.patch.object(adapter, 'abort', side_effect=fake_load.abort):
                result = Controller(budget, adapter, assessment, self.root/'events.jsonl').run()
            self.assertEqual(result['verdict'], 'confirmed')
            self.assertEqual(budget.state['status'], 'completed')
            self.assertEqual(budget.state['transition_evidence']['source_state'], {'inode': 99})
            self.assertTrue(fake_load.cleaned)
            self.assertEqual([c['rate'] for c in fake_load.commands], [100, 200, 400, 300, 350, 325, 300])
        finally:
            budget.close()

    def test_service_stop_attempts_owned_cleanup_even_if_readback_fails(self):
        adapter = RemoteAdapter(self.config, self.fake)
        with mock.patch.object(adapter, '_cleanup_command', side_effect=OSError('lost evidence')), \
             mock.patch('sustained_remote_adapter.stop_owned_unit') as stop:
            with self.assertRaisesRegex(RemoteError, 'readback failed'):
                adapter._stop_service(require_active=True)
            stop.assert_called_once()
        with mock.patch.object(adapter, '_cleanup_command', return_value='ActiveState=failed\nResult=exit-code\n'), \
             mock.patch('sustained_remote_adapter.stop_owned_unit') as stop:
            with self.assertRaisesRegex(RemoteError, 'not healthy'):
                adapter._stop_service(require_active=True)
            stop.assert_called_once()

    def test_driver_result_must_match_full_verification(self):
        good = {'sample': {'produced': 12, 'confirmed': 10, 'pending': 2, 'in_flight': 0}}
        RemoteAdapter._match_driver_verification(good, {'produced': 12, 'confirmed': 10, 'pending': 2})
        with self.assertRaises(RemoteError):
            RemoteAdapter._match_driver_verification(good, {'produced': 12, 'confirmed': 11, 'pending': 1})
        with self.assertRaises(RemoteError):
            RemoteAdapter._match_driver_verification({'sample': dict(good['sample'], in_flight=1)},
                                                     {'produced': 12, 'confirmed': 10, 'pending': 2})

    def test_wrong_host_or_remote_directory_rejected(self):
        config = dict(self.config, host='somewhere-else')
        with self.assertRaises(ValueError):
            RemoteAdapter(config, self.fake)
        config = dict(self.config, remote_root='/root/production')
        with self.assertRaises(ValueError):
            RemoteAdapter(config, self.fake)


if __name__ == '__main__':
    unittest.main()
