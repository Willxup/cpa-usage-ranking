"""Mock the unchanged legacy SSH worker adjustment, never touch real limits."""
import contextlib
import io
import json
import os
import resource
import select
import sys
import unittest
from pathlib import Path
from unittest import mock

SCRIPT = Path(__file__).with_name('remote')/'adjust-worker.py'
SOURCE = compile(SCRIPT.read_text(), str(SCRIPT), 'exec')


class AdjustWorkerTest(unittest.TestCase):
    def exercise(self, wrong_ticks=False):
        worker = {'pid': 201, 'ppid': 202, 'start_ticks': 11, 'comm': 'sshd-session',
                  'owns_ssh_transport': True, 'transport_inodes': ['123']}
        parent = {'pid': 202, 'ppid': 203, 'start_ticks': 12, 'comm': 'sshd-session',
                  'owns_ssh_transport': True, 'transport_inodes': ['123']}
        listener = {'pid': 203, 'ppid': 1, 'start_ticks': 13, 'comm': 'sshd',
                    'owns_ssh_transport': False, 'transport_inodes': []}
        ancestry = {'chain': [{'owns_ssh_transport': False}, worker, parent, listener],
                    'ssh_transport_inodes': ['123']}
        process = {x['pid']: x for x in (worker, parent, listener)}
        limits = {201: (1024, 1048576), 202: (1024, 1048576), 203: (1024, 1048576)}
        writes = []
        class FakePath:
            def __init__(self, value):
                self.value = str(value)
            def __truediv__(self, part):
                return FakePath(self.value+'/'+str(part))
            def read_text(self):
                parts = self.value.split('/')
                pid = int(parts[2])
                item = process[pid]
                if parts[-1] == 'comm':
                    return item['comm']+'\n'
                if parts[-1] == 'stat':
                    fields = ['S']*20
                    fields[1] = str(item['ppid'])
                    fields[19] = str(item['start_ticks']+(1 if wrong_ticks and pid == 201 else 0))
                    return '%s (%s) %s' % (pid, item['comm'], ' '.join(fields))
                raise AssertionError(self.value)
            def iterdir(self):
                pid = int(self.value.split('/')[2])
                return [FakePath(self.value+'/4')] if pid in (201, 202) else []
        def prlimit(pid, which, new=None):
            self.assertEqual(which, resource.RLIMIT_NOFILE)
            old = limits[pid]
            if new is not None:
                writes.append((pid, new))
                limits[pid] = tuple(new)
            return old
        class Poll:
            def register(self, *_):
                pass
            def poll(self, _):
                return []
        stdout = io.StringIO()
        with mock.patch('sys.argv', [str(SCRIPT), json.dumps(ancestry)]), \
             mock.patch('pathlib.Path', FakePath), \
             mock.patch.object(os, 'readlink', return_value='socket:[123]'), \
             mock.patch.object(os, 'pidfd_open', return_value=77), \
             mock.patch.object(os, 'close'), \
             mock.patch.object(select, 'poll', return_value=Poll()), \
             mock.patch.object(resource, 'prlimit', side_effect=prlimit), \
             contextlib.redirect_stdout(stdout):
            if wrong_ticks:
                with self.assertRaisesRegex(RuntimeError, 'pid_identity_changed'):
                    exec(SOURCE, {'__name__': '__main__'})
            else:
                exec(SOURCE, {'__name__': '__main__'})
        return writes, limits, stdout.getvalue()

    def test_only_proven_worker_soft_limit_changes(self):
        writes, limits, output = self.exercise()
        self.assertEqual(writes, [(201, (32768, 1048576))])
        self.assertEqual(limits[202], (1024, 1048576))
        self.assertEqual(limits[203], (1024, 1048576))
        self.assertTrue(json.loads(output)['hard_limit_preserved'])

    def test_reused_pid_is_refused_before_any_limit_write(self):
        writes, _, _ = self.exercise(wrong_ticks=True)
        self.assertEqual(writes, [])


if __name__ == '__main__':
    unittest.main()
