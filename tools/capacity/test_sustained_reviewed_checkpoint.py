"""The one audited recovery is not a generic failed-budget restart."""
import hashlib
import json
import os
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

import sustained_reviewed_checkpoint as checkpoint
from sustained_state import Budget, BudgetError, CONFIRM_RESERVE, NS, SEARCH_LIMIT, TOTAL_LIMIT


class ReviewedCheckpointTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.root = Path(self.tmp.name)
        self.task = self.root/'task'
        self.archive = self.task/'continuation'
        self.archive.mkdir(parents=True, mode=0o700)
        self.task.chmod(0o700)
        self.oracle_path = self.root/'oracle.json'
        old_binary = self.task/'old-bin'
        new_binary = self.archive/'new-bin'
        old_binary.write_bytes(b'old')
        new_binary.write_bytes(b'new')
        self.old_config = {'task_id': '721cb71e50f60575', 'local_task_dir': str(self.task),
                           'capacity_binary': str(old_binary), 'capacity_sha256': checkpoint.OLD_BINARY_SHA,
                           'max_pending': 125000}
        self.new_config = dict(self.old_config, capacity_binary=str(new_binary), capacity_sha256='new-binary-sha')
        (self.task/'config.json').write_text(json.dumps(self.old_config))
        (self.archive/'config-original.json').write_text(json.dumps(self.old_config))
        self.old_hash = checkpoint.canonical_config(self.old_config)
        self.new_hash = checkpoint.canonical_config(self.new_config)
        listener = {'comm': 'sshd', 'pid': 881, 'start_ticks': 686, 'nofile': [1024, 524288]}
        begin = 100
        self.state = {'version': 1, 'status': 'failed', 'config_hash': self.old_hash,
                      'search_ns': 0, 'confirm_ns': 0, 'confirm_started': False, 'host_protections': 0,
                      'active': {'phase': 'search', 'start_ns': begin, 'deadline_ns': 9999999999999,
                                 'boot_id': 'test-boot'},
                      'error': "ToolLimit('generator/transport path invalid: remote SSH worker sustained near one CPU')",
                      'plan': {'current': 800, 'lower': 400, 'upper': 0, 'uncertain_repeats': 0, 'done': False},
                      'stage_index': 3, 'preflight': {'utc_day': time.strftime('%Y-%m-%d', time.gmtime()),
                                                     'remote': {'listener': listener}, 'runtime_hashes': {'old': 'hash'}},
                      'history': [{'event': 'begin', 'phase': 'search', 'start_ns': begin},
                                  {'event': 'failure', 'at_ns': begin+checkpoint.OLD_USED_NS}]}
        self.raw = (json.dumps(self.state, sort_keys=True)+'\n').encode()
        self.original_hash = hashlib.sha256(self.raw).hexdigest()
        self.anchor = self.root/'budget.json'
        self.anchor.write_bytes(self.raw)
        (self.archive/'budget-original.json').write_bytes(self.raw)
        (self.task/'independent-final-readback.json').write_text(json.dumps({'issues': []}))
        verified = {'produced': 188920, 'confirmed': 163994, 'pending': 24926, 'unconfirmed_commits': 8}
        snapshot = {'snapshot_sha256': checkpoint.OLD_SNAPSHOT_SHA, 'source_state': {'open_handles': []}}
        (self.task/'commands.jsonl').write_text(json.dumps({'event': 'abort_verification', 'verified': True,
                                                            'verification': verified, 'snapshot': snapshot})+'\n')
        oracle = {'verified': True, 'journal_sha256': checkpoint.OLD_JOURNAL_SHA,
                  'snapshot_sha256': checkpoint.OLD_SNAPSHOT_SHA, 'produced': 188920, 'confirmed': 163994,
                  'pending': 24926, 'ambiguous_commits': [{} for _ in range(8)], 'daily_reports': 22607,
                  'daily_rows_12_metrics': 142512, 'period_rows_12_metrics': 920000}
        self.oracle_path.write_text(json.dumps(oracle))
        self.hashes = {
            self.archive/'budget-original.json': self.original_hash,
            self.task/'config.json': '31ebf0232f702dd93cc62fe542d6a0e7dd4e30bd332e62ee92db11ed8654aa5e',
            self.archive/'config-original.json': '31ebf0232f702dd93cc62fe542d6a0e7dd4e30bd332e62ee92db11ed8654aa5e',
            new_binary: 'new-binary-sha', self.task/'sustained.jsonl.gz': checkpoint.OLD_JOURNAL_SHA,
            self.archive/'journal-original.jsonl.gz': checkpoint.OLD_JOURNAL_SHA,
            self.task/'abort-snapshot.sqlite': checkpoint.OLD_SNAPSHOT_SHA,
            self.task/'seed.sqlite': '70f4535010e15ebeaa9763ce4b2d9cb0d65a8cb9f81bd9b06e7f99c32d64ff61',
            self.task/'fixture.json': '33084756ddd509800083285fd70d196ff7be525e79048e1829cf541a8f3589c4',
            self.task/'seed-manifest.json': 'b6859cc7a67f930914421e0ae6d1bedf1aa4b5262a16686611f1cb14fea3ebfb',
            self.task/'commands.jsonl': '0e64991192e5c2941f99e29c9765b9e2ac496e815a8a37a50b153642790feaab',
            self.task/'independent-final-readback.json': '851015c75503e2c8372da7c7964531716706ea5c7178984814112333f4352501',
            self.task/'adapter-izhsqo__/driver-01-search/process-events.jsonl': '77bf08bf24435e64e9d81d61e0d442a9821a37d16061b6bd1d8d4041fc09b9fa',
            self.task/'adapter-izhsqo__/driver-01-search/deadline.jsonl': 'e3dde362e71f89e79776a71fcdcf70350e06096ff7d50831eb8c63e046ec5b91',
            self.task/'adapter-izhsqo__/driver-01-search/deadline-1.jsonl': '40f8fd6cc8ede11e93b5d7ec998255d8f7cc0f4884c224cbb68e87c5149a6e2c',
            self.oracle_path: 'b8422ae0d0a80ec1d18de3e428b7dfdcbfef56d6e267301809813add7cde5db4',
        }
        self.adapter = mock.Mock()
        self.adapter.preflight.return_value = {'verified': True, 'runtime_hashes': {'new': 'hash'}}
        self.adapter.preflight_stage.return_value = {'ok': True, 'issues': []}
        self.patches = [mock.patch.object(checkpoint, 'TASK', self.task), mock.patch.object(checkpoint, 'ORACLE', self.oracle_path),
                        mock.patch.object(checkpoint, 'OLD_BUDGET_SHA', self.original_hash),
                        mock.patch.object(checkpoint, 'OLD_CONFIG_HASH', self.old_hash),
                        mock.patch.object(checkpoint, 'file_sha', side_effect=lambda p: self.hashes[Path(p)]),
                        mock.patch.object(checkpoint.ReviewedCheckpoint, '_check_prior_processes')]
        for p in self.patches:
            p.start()
        self.budget = None

    def tearDown(self):
        if self.budget:
            self.budget.close()
        for p in reversed(self.patches):
            p.stop()
        self.tmp.cleanup()

    def open(self):
        reviewed = checkpoint.ReviewedCheckpoint(self.new_config, self.archive, self.adapter)
        self.budget = Budget(self.anchor, self.new_hash, lambda: 1000, lambda: 'test-boot', reviewed=reviewed)
        return self.budget

    def test_audited_migration_debits_original_time_and_preserves_debt_identity(self):
        b = self.open()
        self.assertEqual(b.state['status'], 'reviewed_search_ready')
        self.assertEqual(b.state['search_ns'], checkpoint.OLD_USED_NS)
        self.assertIsNone(b.state['active'])
        self.assertEqual(b.state['transition_evidence']['verification']['pending'], 24926)
        self.assertEqual(b.state['transition_evidence']['verification']['unconfirmed_commits'], 8)
        self.assertEqual(b.state['plan'], {'current': 600, 'lower': 400, 'upper': 0, 'uncertain_repeats': 0, 'done': False})
        self.assertFalse(b.state['confirm_started'])
        b.mark_loading('search')
        deadline = b.begin('search')
        allowance = min(SEARCH_LIMIT-checkpoint.OLD_USED_NS, TOTAL_LIMIT-checkpoint.OLD_USED_NS-CONFIRM_RESERVE)
        self.assertEqual(deadline-1000, allowance)
        self.adapter.preflight.assert_called_once()
        self.adapter.preflight_stage.assert_called_once()

    def test_changed_evidence_does_not_mutate_anchor_or_contact_remote(self):
        self.hashes[self.archive/'journal-original.jsonl.gz'] = 'wrong'
        with self.assertRaises(BudgetError):
            self.open()
        self.assertEqual(self.anchor.read_bytes(), self.raw)
        self.adapter.preflight.assert_not_called()

    def test_unapproved_config_or_remote_failure_does_not_mutate_anchor(self):
        self.new_config['max_pending'] = 150000
        self.new_hash = checkpoint.canonical_config(self.new_config)
        with self.assertRaises(BudgetError):
            self.open()
        self.assertEqual(self.anchor.read_bytes(), self.raw)
        self.new_config['max_pending'] = 125000
        self.new_hash = checkpoint.canonical_config(self.new_config)
        self.adapter.preflight.side_effect = RuntimeError('remote identity changed')
        with self.assertRaisesRegex(RuntimeError, 'remote identity changed'):
            self.open()
        self.assertEqual(self.anchor.read_bytes(), self.raw)

    def test_arbitrary_reviewed_object_is_rejected(self):
        with self.assertRaises(BudgetError):
            Budget(self.anchor, self.new_hash, lambda: 1000, lambda: 'test-boot', reviewed=object())
        self.assertEqual(self.anchor.read_bytes(), self.raw)


if __name__ == '__main__':
    unittest.main()
