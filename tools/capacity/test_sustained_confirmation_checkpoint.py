"""Only the specific clean forecast-stop transition can retain confirmation."""
import copy
import hashlib
import json
import os
import tempfile
import time
from business_day import business_day
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

import sustained_reviewed_checkpoint as cp
from sustained_state import Budget, BudgetError, CONFIRM_RESERVE


class ConfirmationCheckpointTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.root = Path(self.tmp.name)
        self.task = self.root/'task'
        self.archive = self.task/'retry-resume-20261113-01'
        self.archive.mkdir(parents=True, mode=0o700)
        self.adapter_dir = self.task/'adapter-8qvek47s'
        self.adapter_dir.mkdir()
        (self.adapter_dir/'driver-01-search').mkdir()
        self.config = {'task_id': '721cb71e50f60575', 'local_task_dir': str(self.task),
                       'capacity_binary': str(self.archive/'ranking-capacity-v4'),
                       'capacity_sha256': 'a56cfa16fba285e31e4d364138d6569838ecea682e8096e0964a5291302b3f1b',
                       'max_attempts': 512, 'max_pending': 125000}
        (self.archive/'config-retry.json').write_text(json.dumps(self.config))
        listener = {'comm': 'sshd', 'pid': 881, 'start_ticks': 686, 'nofile': [1024, 524288]}
        source = {'open_handles': []}
        self.evidence = {'verified': True, 'journal_sha256': cp.CONFIRM_JOURNAL_SHA,
                         'snapshot_sha256': cp.CONFIRM_SNAPSHOT_SHA,
                         'snapshot_path': str(self.adapter_dir/'transition-snapshot.sqlite'),
                         'pending_at_transition': 16330, 'source_state': source,
                         'verification': {'produced': 456569, 'confirmed': 440239, 'pending': 16330,
                                          'unconfirmed_commits': 16, 'ambiguous_registrations': 0}}
        self.state = {'version': 1, 'status': 'failed', 'active': None, 'config_hash': cp.CONFIRM_CONFIG_HASH,
                      'search_ns': 1095787699798, 'confirm_ns': 0, 'confirm_started': False, 'host_protections': 0,
                      'plan': {'current': 550, 'done': True, 'lower': 500, 'upper': 600,
                               'reason': 'next search resource forecast insufficient', 'uncertain_repeats': 0},
                      'stage_index': 7, 'confirm_rate': 500, 'transition_evidence': self.evidence,
                      'error': 'ResourceForecastLimit("test {\'issues\': [\'local_memory_insufficient\']}")',
                      'reviewed_checkpoint': {'retry_manifest_sha256': 'old'},
                      'preflight': {'business_day': business_day(),
                                    'remote': {'listener': listener}, 'runtime_hashes': {'old': 'hash'}},
                      'history': [{'event': 'end', 'phase': 'search', 'search_ns': 1095787699798},
                                  {'event': 'failure'}]}
        self.anchor = self.root/'budget.json'
        self.update_raw()
        events = [{'event': 'assessment', 'phase': 'search', 'result': {'stage': {'verdict': 'candidate', 'target_rate': 500}}},
                  {'event': 'planned_transition', 'evidence': self.evidence}]
        (self.adapter_dir/'controller-events.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in events))
        proc = [{'event': 'driver_exit', 'returncode': 0}, {'event': 'deadline_exit', 'returncode': 0},
                {'event': 'deadline_exit', 'returncode': 0}]
        (self.adapter_dir/'driver-01-search/process-events.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in proc))
        self.hashes = {
            self.archive/'confirmation-transition-budget.json': cp.CONFIRM_BUDGET_SHA,
            self.archive/'confirmation-transition-journal.jsonl.gz': cp.CONFIRM_JOURNAL_SHA,
            self.task/'sustained.jsonl.gz': cp.CONFIRM_JOURNAL_SHA,
            self.adapter_dir/'transition-snapshot.sqlite': cp.CONFIRM_SNAPSHOT_SHA,
            self.adapter_dir/'abort-snapshot.sqlite': cp.CONFIRM_SNAPSHOT_SHA,
            self.adapter_dir/'controller-events.jsonl': '198f1aba47c7d23fa54b7bfebbaa78ca5b1675341f1b2bf899886c09cebfc617',
            self.adapter_dir/'commands.jsonl': 'e20cb879da49a318d6f4973adf68fcc7863f6e32b28ccb2c127925f7aaec0bd1',
            self.adapter_dir/'driver-01-search/process-events.jsonl': 'bdaff50e04de031a8a515c1fa797cbcb40b73df542a2aa76075bed45d0963f91',
            self.adapter_dir/'driver-01-search/deadline.jsonl': '9f6531decf984c32ab6dd24da90599bc5bb4f20f9622b03d0dcd327d3faecaa4',
            self.adapter_dir/'driver-01-search/deadline-1.jsonl': 'db9d91f0364069a55a3cfb35a5d4a332217a685f50baa0a9eb004629f369c735',
            self.adapter_dir/'verify-transition-3.stdout': '40007481fd6eac9d9d5fc329a719ca5791ceb6bda6d26c5c83e6de0a26061e91',
            self.adapter_dir/'summary-transition-4.stdout': 'a1121b543a48a636e0b3f37ab4bfb27cda2d65be6a06b084d8f69bb71ee4c113',
            self.task/'session-xb45svmp/session-events.jsonl': 'defebcbaab266ce416a5c1971af0d8e0bc0b9ed3309e9a995b7ffb1dd7059f41',
            self.archive/'retry-controller.rc': '4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865',
            self.archive/'retry-controller.stderr': '1f1bfc9e12f04991ac17fd263de1334a2720d8b91546c3b6212eb2d27b5643db',
            self.archive/'config-retry.json': '4a601676bb22bb93768c34459bf3d34a7f4d6ab454f40d8e71d371f48746b6c0',
            self.archive/'retry-migration.json': 'c8b7f6e39305ff8ad49baa8449c85cfcc58a2d930be7de44e0bad183c233b295',
            self.task/'seed.sqlite': '70f4535010e15ebeaa9763ce4b2d9cb0d65a8cb9f81bd9b06e7f99c32d64ff61',
            self.task/'fixture.json': '33084756ddd509800083285fd70d196ff7be525e79048e1829cf541a8f3589c4',
            Path(self.config['capacity_binary']): self.config['capacity_sha256'],
        }
        self.adapter = mock.Mock()
        self.adapter.preflight.return_value = {'verified': True, 'remote': {'listener': listener}, 'runtime_hashes': {'new': 'hash'}}
        self.adapter._snapshot_state.return_value = source
        self.adapter.spec.db = '/approved/stopped-db'
        self.adapter.commands.ssh.return_value = SimpleNamespace(stdout=cp.CONFIRM_SNAPSHOT_SHA+'  db\n')
        self.adapter.preflight_stage.return_value = {'ok': True}
        self.expected_raw_sha = cp.CONFIRM_BUDGET_SHA
        real_sha = hashlib.sha256
        def sha(data=b''):
            if data == self.raw: return mock.Mock(hexdigest=lambda: self.expected_raw_sha)
            return real_sha(data)
        self.patches = [mock.patch.object(cp, 'TASK', self.task),
                        mock.patch.object(cp, 'file_sha', side_effect=lambda p: self.hashes[Path(p)]),
                        mock.patch.object(cp, 'canonical_config', return_value=cp.CONFIRM_CONFIG_HASH),
                        mock.patch.object(cp.hashlib, 'sha256', side_effect=sha),
                        mock.patch.object(cp.ReviewedCheckpoint, '_check_prior_processes')]
        for p in self.patches: p.start()
        self.budget = None

    def update_raw(self):
        self.raw = json.dumps(self.state).encode()
        self.anchor.write_bytes(self.raw)
        (self.archive/'confirmation-transition-budget.json').write_bytes(self.raw)

    def tearDown(self):
        if self.budget: self.budget.close()
        for p in reversed(self.patches): p.stop()
        self.tmp.cleanup()

    def open(self):
        reviewed = cp.ReviewedCheckpoint(self.config, self.archive, self.adapter, kind='confirmation')
        self.budget = Budget(self.anchor, cp.CONFIRM_CONFIG_HASH, lambda: 1000, lambda: 'test-boot', reviewed=reviewed)
        return self.budget

    def test_clean_forecast_failure_can_preserve_one_confirmation(self):
        b = self.open()
        self.assertEqual(b.state['status'], 'transition_ready')
        self.assertEqual(b.state['search_ns'], 1095787699798)
        self.assertFalse(b.state['confirm_started'])
        self.assertEqual(b.state['transition_evidence']['verification']['pending'], 16330)
        b.mark_loading('confirmation')
        self.assertEqual(b.begin('confirmation')-1000, CONFIRM_RESERVE)
        self.assertTrue(b.state['confirm_started'])

    def test_active_or_already_started_confirmation_is_rejected(self):
        for field, value in [('active', {'phase': 'search'}), ('confirm_started', True), ('host_protections', 1)]:
            self.state[field] = value
            self.update_raw()
            with self.assertRaises(BudgetError): self.open()
            self.assertEqual(self.anchor.read_bytes(), self.raw)
            self.state[field] = None if field == 'active' else (False if field == 'confirm_started' else 0)
        self.adapter.preflight.assert_not_called()

    def test_changed_evidence_or_failed_forecast_does_not_mutate_budget(self):
        self.hashes[self.task/'sustained.jsonl.gz'] = 'wrong'
        with self.assertRaises(BudgetError): self.open()
        self.assertEqual(self.anchor.read_bytes(), self.raw)
        self.hashes[self.task/'sustained.jsonl.gz'] = cp.CONFIRM_JOURNAL_SHA
        self.adapter.preflight_stage.return_value = {'ok': False, 'issues': ['local_memory_insufficient']}
        with mock.patch.object(cp, 'CONFIRM_PREFLIGHT_WAIT', 0):
            with self.assertRaises(BudgetError): self.open()
        self.assertEqual(self.anchor.read_bytes(), self.raw)

    def pending_case(self):
        archive = self.task/'pending-100k-20261113-01'
        archive.mkdir(mode=0o700)
        (archive/'budget-before-reservation.json').write_bytes(self.raw)
        (self.task/'sustained.jsonl.gz').write_bytes(b'audited synthetic prefix')
        self.hashes[archive/'budget-before-reservation.json'] = cp.CONFIRM_BUDGET_SHA
        self.hashes[archive/'journal-before-pending-migration.jsonl.gz'] = cp.CONFIRM_JOURNAL_SHA
        binary = archive/'ranking-capacity-v5'
        self.hashes[binary] = 'b'*64
        self.hashes[archive/'pending-migration.json'] = 'c'*64
        config = {**self.config, 'capacity_binary': str(binary), 'capacity_sha256': 'b'*64,
                  'max_pending': 100000, 'pending_migration': str(archive/'pending-migration.json')}
        reservation = {'approval': 'c54-721cb71e50f60575-pending-125000-to-100000',
                       'previous_budget_sha256': cp.CONFIRM_BUDGET_SHA, 'previous_journal_sha256': cp.CONFIRM_JOURNAL_SHA,
                       'previous_config_hash': cp.CONFIRM_CONFIG_HASH, 'previous_snapshot_sha256': cp.CONFIRM_SNAPSHOT_SHA,
                       'previous_max_pending': 125000, 'next_max_pending': 100000, 'max_attempts': 512,
                       'existing_pending': 16330, 'search_ns': 1095787699798, 'confirm_started': False, 'confirm_rate': 500}
        state = copy.deepcopy(self.state)
        state['status'] = 'pending_limit_migration_reserved'
        state['pending_limit_migration_reservation'] = reservation
        state['history'].append({'event': 'approved_pending_limit_migration_reserved', **reservation})
        self.raw = json.dumps(state).encode()
        self.expected_raw_sha = cp.PENDING_RESERVED_SHA
        self.anchor.write_bytes(self.raw)
        new_hash = 'd'*64
        manifest = {'approval': reservation['approval'], 'previous_journal_sha256': cp.CONFIRM_JOURNAL_SHA,
                    'previous_journal_bytes': (self.task/'sustained.jsonl.gz').stat().st_size,
                    'previous_budget_sha256': cp.CONFIRM_BUDGET_SHA, 'reserved_budget_sha256': cp.PENDING_RESERVED_SHA,
                    'previous_config_hash': cp.CONFIRM_CONFIG_HASH, 'next_config_hash': new_hash,
                    'previous_binary_sha256': self.config['capacity_sha256'], 'next_binary_sha256': 'b'*64,
                    'previous_max_pending': 125000, 'next_max_pending': 100000, 'max_attempts': 512,
                    'produced': 456569, 'confirmed': 440239, 'pending': 16330}
        (archive/'pending-migration.json').write_text(json.dumps(manifest))
        return archive, config, new_hash

    def test_exact_pending_decrease_preserves_budget_and_debt(self):
        archive, config, new_hash = self.pending_case()
        reviewed = cp.ReviewedCheckpoint(config, archive, self.adapter, kind='pending')
        with mock.patch.object(cp, 'canonical_config', side_effect=lambda x: new_hash if x.get('max_pending') == 100000 else cp.CONFIRM_CONFIG_HASH):
            self.budget = Budget(self.anchor, new_hash, lambda: 1000, lambda: 'test-boot', reviewed=reviewed)
        b = self.budget.state
        self.assertEqual((b['status'], b['search_ns'], b['confirm_ns'], b['confirm_started']),
                         ('transition_ready', 1095787699798, 0, False))
        self.assertEqual(b['transition_evidence']['verification']['pending'], 16330)
        self.assertEqual(b['config_hash'], new_hash)
        self.assertEqual(b['reviewed_checkpoint']['pending_manifest_sha256'], 'c'*64)
        self.assertEqual(b['reviewed_checkpoint']['pending_manifest']['max_attempts'], 512)

    def test_pending_decrease_rejects_other_config_or_low_memory(self):
        archive, config, new_hash = self.pending_case()
        reviewed = cp.ReviewedCheckpoint({**config, 'max_in_flight': 3000}, archive, self.adapter, kind='pending')
        with mock.patch.object(cp, 'canonical_config', side_effect=lambda x: new_hash if x.get('max_pending') == 100000 else cp.CONFIRM_CONFIG_HASH):
            with self.assertRaises(BudgetError): Budget(self.anchor, new_hash, lambda: 1000, lambda: 'test-boot', reviewed=reviewed)
        self.assertEqual(self.anchor.read_bytes(), self.raw)
        reviewed = cp.ReviewedCheckpoint(config, archive, self.adapter, kind='pending')
        self.adapter.preflight_stage.return_value = {'ok': False, 'issues': ['local_memory_insufficient']}
        with mock.patch.object(cp, 'canonical_config', side_effect=lambda x: new_hash if x.get('max_pending') == 100000 else cp.CONFIRM_CONFIG_HASH):
            with self.assertRaises(BudgetError): Budget(self.anchor, new_hash, lambda: 1000, lambda: 'test-boot', reviewed=reviewed)
        self.assertEqual(self.anchor.read_bytes(), self.raw)

    def test_transient_memory_forecast_waits_and_rechecks_remote_before_save(self):
        self.adapter.preflight_stage.side_effect = [{'ok': False, 'issues': ['local_memory_insufficient']},
                                                    {'ok': True}, {'ok': True}]
        with mock.patch.object(cp.time, 'sleep'):
            b = self.open()
        self.assertEqual(b.state['status'], 'transition_ready')
        self.assertFalse(b.state['confirm_started'])
        self.assertEqual(self.adapter.preflight.call_count, 2)
        self.assertEqual(self.adapter._snapshot_state.call_count, 2)
        self.assertEqual(self.adapter.preflight_stage.call_count, 3)


if __name__ == '__main__':
    unittest.main()
