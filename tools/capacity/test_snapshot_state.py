import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name('remote')/'snapshot-state.py'


class SnapshotStateTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.root = Path(self.temp.name)
        (self.root/'data').mkdir()
        self.db = self.root/'data/ranking.sqlite'
        self.db.write_bytes(b'task-only mock database')

    def tearDown(self):
        self.temp.cleanup()

    def run_check(self):
        return subprocess.run([sys.executable, str(SCRIPT), str(self.root)],
                              capture_output=True, text=True, timeout=15)

    def test_closed_file_and_open_handle(self):
        closed = self.run_check()
        self.assertEqual(closed.returncode, 0, closed.stderr)
        self.assertEqual(json.loads(closed.stdout)['files'][str(self.db)]['size'], self.db.stat().st_size)
        with self.db.open('rb'):
            opened = self.run_check()
        self.assertNotEqual(opened.returncode, 0)
        self.assertIn('handles still open', opened.stderr)

    def test_nonempty_wal_and_symlink_refused(self):
        wal = Path(str(self.db)+'-wal')
        wal.write_bytes(b'not empty')
        p = self.run_check()
        self.assertNotEqual(p.returncode, 0)
        self.assertIn('nonempty WAL', p.stderr)
        wal.unlink()
        self.db.unlink()
        self.db.symlink_to('/etc/passwd')
        p = self.run_check()
        self.assertNotEqual(p.returncode, 0)
        self.assertIn('regular files', p.stderr)


if __name__ == '__main__':
    unittest.main()
