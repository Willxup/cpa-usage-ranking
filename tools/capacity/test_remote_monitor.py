"""Local mocks for the remote monitor; never opens SSH or runs systemctl."""
import contextlib
import io
import json
import os
import resource
import runpy
import tempfile
import time
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from sustained_deadline import proc_info

SCRIPT = Path(__file__).with_name('remote')/'remote-monitor.py'


class RemoteMonitorTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.root = Path(self.tmp.name)
        (self.root/'logs').mkdir()
        (self.root/'data').mkdir()
        (self.root/'data/ranking.sqlite').write_bytes(b'mock-db')
        own = proc_info(os.getpid())
        self.worker = {'pid': os.getpid(), 'ppid': own['ppid'], 'start_ticks': own['start_ticks']}
        self.hard = resource.getrlimit(resource.RLIMIT_NOFILE)[1]
        if resource.getrlimit(resource.RLIMIT_NOFILE)[0] < 32768:
            self.skipTest('test process already has a low soft limit; no limits are changed')
        self.service = 'ranking-c54-test-1.service'
        self.watch = 'ranking-c54-test-1-watch.service'

    def tearDown(self):
        self.tmp.cleanup()

    def run_monitor(self, age):
        row = {'time': time.time()-age, 'available_mib': 1024, 'disk_free_gib': 14,
               'memory_psi': {'full': 0}, 'io_psi': {'full': 0}, 'cache_generated_at': 'actual-mock'}
        (self.root/'logs/watch.jsonl').write_text(json.dumps(row)+'\n')
        argv = [str(SCRIPT), str(self.root), self.service, self.watch, str(self.root/'logs'),
                json.dumps(self.worker), str(self.hard)]
        stdout = io.StringIO()
        active = SimpleNamespace(returncode=0, stdout='active\n', stderr='')
        with mock.patch('sys.argv', argv), mock.patch('subprocess.run', return_value=active), \
             mock.patch('time.sleep', side_effect=SystemExit(0)), contextlib.redirect_stdout(stdout):
            with self.assertRaises(SystemExit) as result:
                runpy.run_path(str(SCRIPT), run_name='__main__')
        return result.exception.code, [json.loads(line) for line in stdout.getvalue().splitlines()]

    def test_sample_includes_real_db_size_worker_and_clock_frequency(self):
        rc, rows = self.run_monitor(0)
        self.assertEqual(rc, 0)
        self.assertEqual(rows[0]['worker']['pid'], os.getpid())
        self.assertEqual(rows[0]['database']['db_bytes'], len(b'mock-db'))
        self.assertEqual(rows[0]['database']['wal_bytes'], 0)
        self.assertGreater(rows[0]['clock_ticks_per_second'], 0)
        self.assertEqual(rows[0]['guard']['cache_generated_at'], 'actual-mock')

    def test_stale_watch_is_fatal(self):
        rc, rows = self.run_monitor(100)
        self.assertEqual(rc, 2)
        self.assertIn('guard_unavailable_or_stopped', rows[-1]['fatal'])


if __name__ == '__main__':
    unittest.main()
