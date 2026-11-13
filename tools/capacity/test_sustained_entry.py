import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from sustained_controller import result_exit_code

SCRIPT = Path(__file__).with_name('sustained_controller.py')


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


@unittest.skipUnless(os.environ.get('CAPACITY_TEST_BIN'), 'requires built Go capacity CLI')
class EntryTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.root = Path(self.tmp.name)
        self.root.chmod(0o700)
        repo = Path(__file__).resolve().parents[2]
        token = '1234567890abcdef'
        binary = os.environ['CAPACITY_TEST_BIN']
        catalog = repo/'config/models.json'
        self.config = {'host': 'lax-racknerd', 'task_id': token, 'local_task_dir': str(self.root),
                       'remote_root': '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/sustained-20261113-r1/task-'+token,
                       'service': 'ranking-c54-'+token+'.service', 'watch': 'ranking-c54-'+token+'-watch.service',
                       'application_remote': '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/bin/ranking',
                       'catalog_remote': '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/bin/models.json',
                       'web_remote': '/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/web-dist',
                       'capacity_binary': binary, 'catalog_local': str(catalog),
                       'capacity_sha256': digest(binary), 'catalog_sha256': digest(catalog), 'application_sha256': 'a'*64,
                       'baseline_containers_sha256': 'b'*64}
        self.path = self.root/'config.json'
        self.path.write_text(json.dumps(self.config))

    def tearDown(self):
        self.tmp.cleanup()

    def run_entry(self):
        # Only substitute this test's isolated root for the historical root.
        # Keep all other path, ownership and production safety checks intact.
        bootstrap = '''
import runpy, sys
from pathlib import Path
from unittest import mock
root = Path(sys.argv[1])
script = sys.argv[2]
original = Path.is_relative_to
historical = Path('/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/sustained-20261113-r1')
sys.path.insert(0, str(Path(script).parent))
sys.argv = [script, '--config', str(root/'config.json')]
with mock.patch.object(Path, 'is_relative_to', lambda path, other:
        True if path == root and other == historical else original(path, other)):
    runpy.run_path(script, run_name='__main__')
'''
        return subprocess.run([sys.executable, '-c', bootstrap, str(self.root), str(SCRIPT)],
                              capture_output=True, text=True, timeout=10)

    def test_default_is_dry_run_and_never_creates_state(self):
        p = self.run_entry()
        self.assertEqual(p.returncode, 0, p.stderr)
        result = json.loads(p.stdout)
        self.assertEqual(result['mode'], 'dry_run_no_remote_no_load')
        self.assertEqual(result['initial_rate'], 100)
        self.assertEqual(result['single_confirmation_seconds'], 1200)
        self.assertFalse((self.root/'budget.json').exists())
        self.assertEqual(sorted(x.name for x in self.root.iterdir()), ['config.json'])

    def test_unmocked_cli_rejects_outside_historical_root(self):
        p = subprocess.run([sys.executable, str(SCRIPT), '--config', str(self.path)],
                           capture_output=True, text=True, timeout=10)
        self.assertNotEqual(p.returncode, 0)
        self.assertIn('local task directory must be a private C54 task subdirectory', p.stderr)
        self.assertFalse((self.root/'budget.json').exists())

    def test_no_observed_upper_bound_is_incomplete_even_after_confirmation(self):
        self.assertEqual(result_exit_code({'verdict': 'confirmed'}, {'lower': 100, 'upper': 0}), 2)
        self.assertEqual(result_exit_code({'verdict': 'confirmed'}, {'lower': 100, 'upper': 125}), 0)
        self.assertEqual(result_exit_code({'verdict': 'unconfirmed'}, {'lower': 100, 'upper': 125}), 2)

    def test_unsafe_config_fails_before_remote_access(self):
        self.config['max_journal_bytes'] = 20 << 30
        self.path.write_text(json.dumps(self.config))
        p = self.run_entry()
        self.assertNotEqual(p.returncode, 0)
        self.assertIn('invalid local safety bound', p.stderr)
        self.assertFalse((self.root/'budget.json').exists())


if __name__ == '__main__':
    unittest.main()
