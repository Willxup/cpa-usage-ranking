import os
import sys
import tempfile
import unittest
from pathlib import Path

from sustained_transfer import guarded_command


class TransferTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.root = Path(self.tmp.name)
        self.events = []

    def tearDown(self):
        self.tmp.cleanup()

    def test_normal_command_saves_stdout_stderr_and_status(self):
        argv = [sys.executable, '-c', 'import sys;print("ok");print("note",file=sys.stderr)']
        out, err = guarded_command(argv, self.root, 'normal', lambda: None, self.events.append, timeout=5)
        self.assertEqual(out.read_text().strip(), 'ok')
        self.assertEqual(err.read_text().strip(), 'note')
        self.assertEqual(self.events[-1]['returncode'], 0)

    def test_guard_loss_terminates_owned_group_and_saves_status(self):
        calls = 0
        def guard():
            nonlocal calls
            calls += 1
            if calls >= 2:
                raise RuntimeError('stale guard')
        argv = [sys.executable, '-c', 'import time;time.sleep(60)']
        with self.assertRaisesRegex(RuntimeError, 'stale guard'):
            guarded_command(argv, self.root, 'guard-loss', guard, self.events.append, timeout=10)
        self.assertIsNotNone(self.events[-1]['returncode'])
        self.assertIn('stale guard', self.events[-1]['error'])
        self.assertTrue((self.root/'guard-loss.stderr').exists())


if __name__ == '__main__':
    unittest.main()
