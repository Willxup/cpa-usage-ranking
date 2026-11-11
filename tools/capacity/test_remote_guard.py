"""Run the parameterized legacy guard against local mock proc/docker/systemctl."""
import collections
import json
import os
import runpy
import tempfile
import unittest
from pathlib import Path
from unittest import mock

WATCH = Path(__file__).with_name('remote')/'watch.py'
Disk = collections.namedtuple('Disk', 'total used free')


class StopAfterOne(Exception):
    pass


class GuardTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.root = Path(self.tmp.name)
        (self.root/'logs').mkdir()
        self.baseline = {'container-'+str(i): ['true', '0', 'healthy'] for i in range(7)}
        (self.root/'baseline-containers.json').write_text(json.dumps(self.baseline))
        self.service = 'ranking-c54-test-abc.service'

    def tearDown(self):
        self.tmp.cleanup()

    def run_guard(self, *, available_mib=1024, memory_psi=0, io_psi=0, disk_gib=20,
                  oom=0, container_change=False, single=False):
        original_read = Path.read_text
        def read(path, *args, **kwargs):
            text = str(path)
            if text == '/proc/meminfo':
                return 'MemAvailable: %d kB\n' % (available_mib*1024)
            if text == '/proc/pressure/memory':
                return 'some avg10=0 avg60=0 avg300=0 total=0\nfull avg10=%s avg60=0 avg300=0 total=0\n' % memory_psi
            if text == '/proc/pressure/io':
                return 'some avg10=0 avg60=0 avg300=0 total=0\nfull avg10=%s avg60=0 avg300=0 total=0\n' % io_psi
            if text.startswith('/sys/fs/cgroup/system.slice/'+self.service+'/'):
                name = path.name
                if name in ('memory.current', 'memory.peak'):
                    return '1048576'
                if name == 'memory.events':
                    return 'oom %d\n' % oom
                return 'total 0\n'
            return original_read(path, *args, **kwargs)
        output = ''.join('/%s true %s healthy\n' % (name, '1' if container_change and i == 0 else '0')
                         for i, name in enumerate(self.baseline))
        sleep = mock.Mock(side_effect=StopAfterOne if single else None)
        run = mock.Mock()
        argv = [str(WATCH), str(self.root), self.service]
        with mock.patch('sys.argv', argv), mock.patch.object(Path, 'read_text', read), \
             mock.patch('shutil.disk_usage', return_value=Disk(30*2**30, (30-disk_gib)*2**30, disk_gib*2**30)), \
             mock.patch('subprocess.check_output', return_value=output), mock.patch('subprocess.run', run), \
             mock.patch('urllib.request.urlopen', side_effect=OSError('no local service')), \
             mock.patch('time.sleep', sleep):
            try:
                runpy.run_path(str(WATCH), run_name='__main__')
            except StopAfterOne:
                pass
        rows = [json.loads(line) for line in (self.root/'logs/watch.jsonl').read_text().splitlines()]
        return rows, run

    def test_healthy_sample_does_not_stop(self):
        rows, run = self.run_guard(single=True)
        self.assertEqual(len(rows), 1)
        self.assertNotIn('stop_reason', rows[0])
        run.assert_not_called()

    def test_original_thresholds_and_seven_container_guard(self):
        cases = [
            ({'available_mib': 511}, 5, 'host available memory below 512MiB'),
            ({'memory_psi': 1.01}, 5, 'sustained host memory pressure'),
            ({'io_psi': 10.01}, 10, 'sustained host IO pressure'),
            ({'disk_gib': 9}, 1, 'disk reserve below 10GiB'),
            ({'oom': 1}, 1, 'test cgroup OOM'),
            ({'container_change': True}, 1, 'existing container state changed'),
        ]
        for kwargs, count, reason in cases:
            with self.subTest(reason=reason):
                (self.root/'logs/watch.jsonl').unlink(missing_ok=True)
                (self.root/'STOPPED').unlink(missing_ok=True)
                rows, run = self.run_guard(**kwargs)
                self.assertEqual(len(rows), count)
                self.assertEqual(rows[-1]['stop_reason'], reason)
                run.assert_called_once_with(['systemctl', 'stop', self.service], timeout=15)
                self.assertEqual((self.root/'STOPPED').read_text().strip(), reason)

    def test_rejects_non_task_unit_before_start(self):
        with mock.patch('sys.argv', [str(WATCH), str(self.root), 'ssh.service']):
            with self.assertRaises(SystemExit):
                runpy.run_path(str(WATCH), run_name='__main__')
        self.assertFalse((self.root/'logs/watch.jsonl').exists())


if __name__ == '__main__':
    unittest.main()
