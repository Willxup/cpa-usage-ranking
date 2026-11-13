"""The second audited retry migration preserves budget, debts and boundaries."""
import hashlib
import json
import os
import tempfile
import time
from business_day import business_day
import unittest
from pathlib import Path
from unittest import mock

import sustained_reviewed_checkpoint as cp
from sustained_state import Budget, BudgetError, CONFIRM_RESERVE, NS, SEARCH_LIMIT, TOTAL_LIMIT


class RetryCheckpointTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.root = Path(self.tmp.name)
        self.task = self.root/'sustained-20261113-r1/formal-721cb71e50f60575'
        self.old = self.task/'continuation-4b45a832'
        self.archive = self.task/'retry'
        self.archive.mkdir(parents=True, mode=0o700)
        self.old.mkdir(mode=0o700)
        self.oracle_path = self.root/'parent-sustained-continue-review/independent-oracle.json'
        self.oracle_path.parent.mkdir()
        self.old_config = {'task_id': '721cb71e50f60575', 'local_task_dir': str(self.task),
                           'capacity_binary': str(self.old/'ranking-capacity'),
                           'capacity_sha256': 'fbebd0bca029514e0a3728e238fe0986301ea7a23ecd3d704dbb8443e062546a',
                           'max_pending': 125000, 'host': 'lax-racknerd'}
        self.binary = self.archive/'ranking-capacity'
        self.binary.write_bytes(b'new binary')
        self.manifest_path = self.archive/'retry-migration.json'
        self.new_config = dict(self.old_config, capacity_binary=str(self.binary), capacity_sha256='new-binary',
                               max_pending=100000, max_attempts=512, retry_migration=str(self.manifest_path))
        self.old_config_hash = 'c9fe089a5bfe2ab3fa0079dfe30d07aeeb247a1f55eaea6f9203ec19bfe77bf8'
        self.new_config_hash = 'new-config-hash'
        for p in (self.old/'config-reviewed.json', self.archive/'config-previous.json'):
            p.write_text(json.dumps(self.old_config))
        journal = self.task/'sustained.jsonl.gz'
        journal.write_bytes(b'old journal')
        self.manifest = {'approval': 'c54-721cb71e50f60575-retry-30-to-512',
                         'previous_journal_sha256': 'd08cb32d0c2b0dfc7bf0589d80ba6142a6d9be44c8a84c54bab922830a851c6e',
                         'previous_journal_bytes': journal.stat().st_size,
                         'previous_budget_sha256': 'dc40ff7f5adb81424a5dacd948ecddb9edfac6528106b04f27f4643a2a9768ae',
                         'previous_config_hash': self.old_config_hash, 'next_config_hash': self.new_config_hash,
                         'previous_binary_sha256': self.old_config['capacity_sha256'], 'next_binary_sha256': 'new-binary',
                         'previous_max_attempts': 30, 'next_max_attempts': 512,
                         'previous_max_pending': 125000, 'next_max_pending': 100000,
                         'task_sequence': 130422, 'task_client': 9034, 'task_revision': 54,
                         'task_day': '2026-11-13', 'task_body_sha256': 'ad6e280211e23aa21566bdad0074a0045452d71dbb1ced4d748aea1b21448a1a',
                         'task_attempts': 30, 'next_task_attempt': 31,
                         'task_finish_unix_ns': 1794560887575625142, 'task_retry_after_ns': 1000000000,
                         'task_not_before_unix_ns': 1794560902575625142}
        self.manifest_path.write_text(json.dumps(self.manifest))
        listener = {'comm': 'sshd', 'pid': 881, 'start_ticks': 686, 'nofile': [1024, 524288]}
        first, second = 100, 1_000_000_000_000
        error = "ControllerError('unexpected stage/target in sample stream')"
        self.state = {'version': 1, 'status': 'failed', 'config_hash': self.old_config_hash,
                      'search_ns': 612018194623, 'confirm_ns': 0, 'confirm_started': False, 'host_protections': 0,
                      'active': {'phase': 'search', 'start_ns': second, 'deadline_ns': second+9_000_000_000_000,
                                 'boot_id': 'test-boot'}, 'error': error,
                      'plan': {'current': 500, 'lower': 400, 'upper': 600, 'uncertain_repeats': 0, 'done': False},
                      'stage_index': 5, 'reviewed_checkpoint': {'original_capacity_sha256': cp.OLD_BINARY_SHA},
                      'preflight': {'business_day': business_day(),
                                    'remote': {'listener': listener}, 'runtime_hashes': {'old': 'hash'}},
                      'history': [{'event': 'begin', 'start_ns': first},
                                  {'event': 'failure', 'at_ns': first+612018194623},
                                  {'event': 'begin', 'start_ns': second},
                                  {'event': 'failure', 'at_ns': second+302284608837}]}
        self.raw = json.dumps(self.state).encode()
        self.anchor = self.root/'budget.json'
        self.anchor.write_bytes(self.raw)
        (self.archive/'budget-previous.json').write_bytes(self.raw)
        snapshot = {'snapshot_sha256': '27404c44ab3ad573229d4dfffcf476ffbe25887911a1c07c188494d7267e5e32',
                    'source_state': {'open_handles': []}}
        verified = {'produced': 363619, 'confirmed': 334393, 'pending': 29226,
                    'unconfirmed_commits': 4, 'ambiguous_registrations': 1}
        capture = {'capture_verified': True, 'ledger_database_verified': True, 'owned_watch_session_cleaned': True,
                   'snapshot': snapshot, 'verification': verified}
        (self.old/'stopped-capture-evidence.json').write_text(json.dumps(capture))
        oracle = {'verified': True, 'journal_sha256': self.manifest['previous_journal_sha256'],
                  'snapshot_sha256': snapshot['snapshot_sha256'], 'produced': 363619, 'confirmed': 334393,
                  'pending': 29226, 'ambiguous_commits': [{}]*4, 'ambiguous_registrations': [{}],
                  'daily_reports': 25684, 'daily_rows_12_metrics': 165997, 'period_rows_12_metrics': 920000,
                  'exhausted_task': {'sequence': 130422, 'client': 9034, 'attempts': 30,
                                     'all_statuses_503': True, 'still_pending': True}}
        self.oracle_path.write_text(json.dumps(oracle))
        # Preserve all production hash gates while isolating this synthetic test.
        self.hashes = {
            self.archive/'budget-previous.json': self.manifest['previous_budget_sha256'],
            self.archive/'journal-previous.jsonl.gz': self.manifest['previous_journal_sha256'],
            self.archive/'config-previous.json': 'fb7749138cae399c382b7ad0d3711c4def3eefdfcf28e5f72013399a136f2360',
            self.old/'config-reviewed.json': 'fb7749138cae399c382b7ad0d3711c4def3eefdfcf28e5f72013399a136f2360',
            journal: self.manifest['previous_journal_sha256'],
            self.task/'abort-continue-4b45a832-snapshot.sqlite': snapshot['snapshot_sha256'],
            self.task/'seed.sqlite': '70f4535010e15ebeaa9763ce4b2d9cb0d65a8cb9f81bd9b06e7f99c32d64ff61',
            self.task/'fixture.json': '33084756ddd509800083285fd70d196ff7be525e79048e1829cf541a8f3589c4',
            self.task/'seed-manifest.json': 'b6859cc7a67f930914421e0ae6d1bedf1aa4b5262a16686611f1cb14fea3ebfb',
            self.old/'stopped-capture-evidence.json': 'ce2ce780198a4b51b3b8be0d8adad576b262af7e7de17c63c57e7d27caff64c7',
            self.old/'final-readonly-summary.json': 'bd87ab453b84e8b3e2746021c115b9ef7748911e82dce389d9c1172fed78c44e',
            self.task/'adapter-cnt2zenz/driver-01-search/process-events.jsonl': '5abe03104adc5e3d6dc160acfa8c85c89d83da050a5c0cce8d7a0d6d5221f42b',
            self.task/'adapter-cnt2zenz/driver-01-search/deadline.jsonl': 'f946161c53b0919d3d0ef58c2efb9e41f638a4cafd44a7dce3815ed9d49f869f',
            self.task/'adapter-cnt2zenz/driver-01-search/deadline-1.jsonl': '635491e0aea15d4437d5ab944c3c3586c751536a763ef30d5ed7184dc1f3b737',
            self.oracle_path: '3bfc86016411540c47f4f8c5c4578a5b92c5c32c4821660050bc1a22f9370fcb',
            self.binary: 'new-binary', self.manifest_path: 'manifest-hash',
        }
        self.adapter = mock.Mock()
        self.adapter.preflight.return_value = {'verified': True, 'runtime_hashes': {'new': 'hash'}}
        self.adapter.preflight_stage.return_value = {'ok': True, 'issues': []}
        # Only the exact fixed budget SHA differs in synthetic inputs.
        real_sha256 = hashlib.sha256
        def sha(data=b''):
            if data == self.raw:
                return mock.Mock(hexdigest=lambda: self.manifest['previous_budget_sha256'])
            return real_sha256(data)
        self.patches = [mock.patch.object(cp, 'ROOT', self.root), mock.patch.object(cp, 'TASK', self.task),
                        mock.patch.object(cp, 'file_sha', side_effect=lambda p: self.hashes[Path(p)]),
                        mock.patch.object(cp, 'canonical_config', side_effect=lambda c: self.new_config_hash if 'max_attempts' in c else self.old_config_hash),
                        mock.patch.object(cp.hashlib, 'sha256', side_effect=sha),
                        mock.patch.object(cp.ReviewedCheckpoint, '_check_prior_processes')]
        for p in self.patches: p.start()
        self.budget = None

    def tearDown(self):
        if self.budget: self.budget.close()
        for p in reversed(self.patches): p.stop()
        self.tmp.cleanup()

    def open(self):
        reviewed = cp.ReviewedCheckpoint(self.new_config, self.archive, self.adapter, kind='retry')
        self.budget = Budget(self.anchor, self.new_config_hash, lambda: 1000, lambda: 'test-boot', reviewed=reviewed)
        return self.budget

    def test_debits_both_intervals_and_preserves_original_plan_and_debts(self):
        b = self.open()
        self.assertEqual(b.state['search_ns'], 914302803460)
        self.assertEqual(b.state['stage_index'], 6)
        self.assertEqual(b.state['plan'], {'current': 500, 'lower': 400, 'upper': 600, 'uncertain_repeats': 0, 'done': False})
        self.assertEqual(b.state['reviewed_checkpoint']['replay_expected'], [363619, 334393, 29226, 0])
        self.assertEqual(b.state['reviewed_checkpoint']['retry_manifest']['previous_max_attempts'], 30)
        self.assertEqual(b.state['reviewed_checkpoint']['retry_manifest']['next_max_attempts'], 512)
        self.assertFalse(b.state['confirm_started'])
        b.mark_loading('search')
        allowance = min(SEARCH_LIMIT-914302803460, TOTAL_LIMIT-914302803460-CONFIRM_RESERVE)
        self.assertEqual(b.begin('search')-1000, allowance)

    def test_altered_budget_and_journal_are_not_mutated(self):
        self.anchor.write_bytes(self.raw+b' ')
        with self.assertRaises(BudgetError): self.open()
        self.assertEqual(self.anchor.read_bytes(), self.raw+b' ')
        self.anchor.write_bytes(self.raw)
        self.hashes[self.archive/'journal-previous.jsonl.gz'] = 'wrong'
        with self.assertRaises(BudgetError): self.open()
        self.assertEqual(self.anchor.read_bytes(), self.raw)
        self.adapter.preflight.assert_not_called()

    def test_unapproved_manifest_or_config_rejected(self):
        self.manifest['next_max_attempts'] = 513
        self.manifest_path.write_text(json.dumps(self.manifest))
        with self.assertRaises(BudgetError): self.open()
        self.manifest['next_max_attempts'] = 512
        self.manifest_path.write_text(json.dumps(self.manifest))
        self.new_config['host'] = 'unapproved'
        with self.assertRaises(BudgetError): self.open()
        self.assertEqual(self.anchor.read_bytes(), self.raw)

    def test_remote_or_forecast_failure_cannot_write_budget(self):
        self.adapter.preflight_stage.return_value = {'ok': False, 'issues': ['local_memory_insufficient']}
        with self.assertRaises(BudgetError): self.open()
        self.assertEqual(self.anchor.read_bytes(), self.raw)


if __name__ == '__main__':
    unittest.main()
