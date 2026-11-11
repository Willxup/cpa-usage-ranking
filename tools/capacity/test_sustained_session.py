import json
import os
import tempfile
import time
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from sustained_remote_spec import RemoteSpec
from sustained_session import Session


class SessionOwnershipTest(unittest.TestCase):
    def setUp(self):
        self.session = Session.__new__(Session)
        self.session.spec = RemoteSpec('/root/task/new', 'ranking-c54-sus-1.service',
                                       'ranking-c54-sus-1-watch.service', '/root/task/bin/ranking',
                                       '/root/task/bin/models.json', '/root/task/web-dist')
        self.session.watch_start_attempted = True
        self.calls = []
        self.state = 'active'
        self.bad = ''
        self.session.cleanup_command = self.command

    def command(self, command, timeout=12):
        self.calls.append(command)
        if command.startswith('systemctl stop '):
            self.state = 'inactive'
            return ''
        service = self.session.spec.service in command and self.session.spec.watcher not in command
        # The full unit query contains the watch unit only when querying it.
        if command.startswith('systemctl show '+self.session.spec.service+' '):
            service = True
        desc = self.session.spec.owner+('-service' if service else '-watch')
        if self.bad == 'description':
            desc = 'unowned'
        binds = self.session.spec.watcher if self.bad != 'binds' else 'somebody-else.service'
        bound_by = self.session.spec.service if self.bad != 'bound_by' else 'unowned.service'
        work = self.session.spec.root if self.bad != 'workdir' else '/elsewhere'
        executable = self.session.spec.root+'/bin/watch.py' if self.bad != 'executable' else '/elsewhere/watch.py'
        return '\n'.join(['ActiveState='+self.state, 'Description='+desc, 'BindsTo='+binds,
                          'BoundBy='+bound_by, 'WorkingDirectory='+work, 'ExecStart='+executable])

    def test_owned_service_is_stopped_and_read_back(self):
        self.session.cleanup_unit(self.session.spec.service, self.session.spec.owner+'-service', service=True)
        self.assertEqual(len(self.calls), 3)
        self.assertEqual(self.state, 'inactive')
        self.assertTrue(self.calls[1].startswith('systemctl stop '))

    def test_unproven_service_is_never_stopped(self):
        for mismatch in ('description', 'binds', 'workdir'):
            with self.subTest(mismatch=mismatch):
                self.calls.clear()
                self.bad = mismatch
                with self.assertRaisesRegex(RuntimeError, 'ownership not proven'):
                    self.session.cleanup_unit(self.session.spec.service, self.session.spec.owner+'-service', service=True)
                self.assertFalse(any(x.startswith('systemctl stop ') for x in self.calls))

    def test_unowned_watcher_or_external_bound_unit_never_stopped(self):
        for mismatch in ('description', 'executable', 'bound_by'):
            with self.subTest(mismatch=mismatch):
                self.calls.clear()
                self.bad = mismatch
                with self.assertRaisesRegex(RuntimeError, 'ownership not proven'):
                    self.session.cleanup_unit(self.session.spec.watcher, self.session.spec.owner+'-watch', service=False)
                self.assertFalse(any(x.startswith('systemctl stop ') for x in self.calls))

    def test_no_start_attempt_means_no_remote_cleanup(self):
        self.session.watch_start_attempted = False
        self.session.cleanup_unit(self.session.spec.watcher, self.session.spec.owner+'-watch', service=False)
        self.assertEqual(self.calls, [])


class SessionMockLifecycleTest(unittest.TestCase):
    def test_session_starts_watch_tunnel_calibrates_and_cleans_with_mock_ssh(self):
        with tempfile.TemporaryDirectory(dir=os.environ['TMPDIR']) as temp:
            d = Path(temp)
            d.chmod(0o700)
            (d/'stop-requested').write_text('stop')
            cfg = {'local_session_dir': str(d), 'remote_root': '/root/task/new', 'host': 'lax-racknerd',
                   'service': 'ranking-c54-sus-1.service', 'watch': 'ranking-c54-sus-1-watch.service',
                   'application': '/root/task/bin/ranking', 'catalog': '/root/task/bin/models.json',
                   'web': '/root/task/web-dist'}
            session = Session(cfg)
            commands = []
            worker = {'pid': 42, 'ppid': 43, 'start_ticks': 100, 'owns_ssh_transport': True}
            parent = {'pid': 43, 'ppid': 44, 'start_ticks': 101, 'owns_ssh_transport': True}
            chain = [{'owns_ssh_transport': False}, worker, parent, {'owns_ssh_transport': False}]
            adjustment = {'hard_limit_preserved': True, 'after': [{'nofile': [32768, 65536]}]}
            def independent(command, timeout=12, check=True):
                commands.append(command)
                if command.startswith('stat -c %s'):
                    out = '0\n'
                elif command == 'systemctl show '+session.spec.watcher:
                    out = '\n'.join(['ActiveState=active', 'RuntimeMaxUSec=2h', 'MemoryMax=67108864',
                                     'CPUQuotaPerSecUSec=100ms', 'Description='+session.spec.owner+'-watch'])
                elif 'json,sys,time' in command:
                    out = json.dumps({'size': 10, 'age': .1, 'stop_reason': None})
                elif 'adjust-worker.py' in command:
                    out = json.dumps(adjustment)
                else:
                    out = ''
                return SimpleNamespace(returncode=0, stdout=out, stderr='')
            class FakeProc:
                def __init__(self, pid):
                    self.pid, self.returncode = pid, None
                    self.stdout = None
                def poll(self):
                    return self.returncode
                def wait(self, timeout=None):
                    self.returncode = 0
                    return 0
            processes = []
            def popen(args, **kwargs):
                p = FakeProc(1000+len(processes))
                processes.append(p)
                if len(processes) == 1:
                    (d/'f').touch()
                return p
            def run(args, **kwargs):
                output = json.dumps({'chain': chain}) if any('probe-ancestry.py' in str(x) for x in args) else ''
                return SimpleNamespace(returncode=0, stdout=output, stderr='')
            def monitor():
                session.monitor_last = time.monotonic()
            info = {'pid': 1000, 'ppid': os.getpid(), 'start_ticks': 77, 'nofile_line': 'Max open files 1024 1048576 files'}
            with mock.patch.object(session, 'independent', side_effect=independent), \
                 mock.patch.object(session, 'cleanup_unit') as cleanup, \
                 mock.patch.object(session, 'read_monitor', side_effect=monitor), \
                 mock.patch.object(session, 'signal_child'), \
                 mock.patch('sustained_session.local_process_info', return_value=info), \
                 mock.patch('sustained_session.os.pidfd_open', side_effect=lambda _: os.open('/dev/null', os.O_RDONLY)), \
                 mock.patch('sustained_session.subprocess.Popen', side_effect=popen), \
                 mock.patch('sustained_session.subprocess.run', side_effect=run), \
                 mock.patch('sustained_session.signal.signal'):
                self.assertEqual(session.run(), 0)
            self.assertEqual(cleanup.call_count, 2)
            self.assertTrue(any('RuntimeMaxSec=2h' in c for c in commands))
            self.assertTrue(any('adjust-worker.py' in c for c in commands))
            ready = json.loads((d/'session-ready.json').read_text())
            self.assertEqual(ready['worker']['pid'], worker['pid'])
            self.assertTrue(ready['adjustment']['hard_limit_preserved'])
            events = [json.loads(line)['event'] for line in (d/'session-events.jsonl').read_text().splitlines()]
            self.assertIn('ancestry_probe', events)
            self.assertIn('session_finished', events)


if __name__ == '__main__':
    unittest.main()
