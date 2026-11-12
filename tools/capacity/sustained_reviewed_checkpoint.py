"""One explicitly authorized C54 checkpoint, not general failed-state recovery.

The old bytes, independent oracle, stopped DB and full journal must all match
this particular audited interruption before the budget anchor is changed.
The normal Budget path remains fail-closed for every other failure.
"""
import copy
import hashlib
import json
import os
import time
from pathlib import Path

from sustained_state import BudgetError

ROOT = Path('/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54')
TASK = ROOT/'sustained-20261113-r1/formal-721cb71e50f60575'
ORACLE = ROOT/'parent-sustained-run-review/independent-oracle.json'
OLD_BUDGET_SHA = 'd040810acb4bb1b5f363bae6d4f4231556babf91ac4694f4ff8caf8f5009ad4a'
OLD_CONFIG_HASH = 'f1a528fa0daf531ea393a267b86d518a386754106b46a16993e3ae95354e6dd2'
OLD_JOURNAL_SHA = '6e98dbcd8c0498e0dabd410d949a0f6e4438b3ca2e2b328d94003f5c1f7bab40'
OLD_SNAPSHOT_SHA = '40f16735373af7180522a4a8a8238e77e0de8a536f08d0b4d89738433e883258'
OLD_BINARY_SHA = '404ba7bb01728bf37b0c768e200e42505379f2f3942921f13792fa47901365b8'
OLD_USED_NS = 612018194623


def file_sha(path):
    path = Path(path)
    if path.is_symlink() or not path.is_file():
        raise BudgetError('reviewed evidence missing or symlink: '+str(path))
    h = hashlib.sha256()
    with path.open('rb') as source:
        for block in iter(lambda: source.read(4*1024*1024), b''):
            h.update(block)
    return h.hexdigest()


def canonical_config(config):
    return hashlib.sha256(json.dumps(config, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


class ReviewedCheckpoint:
    """Migrate only the specific parent-audited failed/active checkpoint."""
    def __init__(self, config, archive_dir, adapter, kind='original'):
        if kind not in ('original', 'retry'):
            raise BudgetError('unknown reviewed checkpoint kind')
        self.config = dict(config)
        self.archive_dir = Path(archive_dir)
        self.adapter = adapter
        self.kind = kind

    def _check_prior_processes(self):
        # Refuse even a recycled PID rather than guessing ownership. No other
        # controller or task tunnel may be alive during the migration.
        for pid in (3184171, 3184172, 3184214, 3184226, 3184364, 3184406, 3184407, 3184415,
                    3236214, 3236217, 3236271, 3236283, 3236348, 3236349, 3236365,
                    3244196, 3244210, 3246156):
            if (Path('/proc')/str(pid)).exists():
                raise BudgetError('prior task PID exists; ownership cannot be assumed: '+str(pid))
        for proc in Path('/proc').iterdir():
            if not proc.name.isdigit() or int(proc.name) == os.getpid():
                continue
            try:
                args = [x.decode(errors='replace') for x in (proc/'cmdline').read_bytes().split(b'\0') if x]
            except FileNotFoundError:
                continue
            except PermissionError as exc:
                raise BudgetError('cannot inspect controller process ownership') from exc
            if (any(Path(x).name in ('sustained_controller.py', 'sustained_session.py', 'sustained_deadline.py') for x in args) or
                any(Path(x).name.startswith('ranking-capacity') for x in args) and
                any(x in ('sustained', 'verify-sustained', 'summarize-sustained') for x in args) or
                any(Path(x).name == 'ssh' for x in args) and any('14210:127.0.0.1:4210' in x for x in args)):
                raise BudgetError('another controller, generator, watchdog, session or task tunnel exists: '+proc.name)

    def migrate(self, state, raw, new_config_hash, clock, boot):
        if self.kind == 'retry':
            return self._migrate_retry(state, raw, new_config_hash, clock, boot)
        if hashlib.sha256(raw).hexdigest() != OLD_BUDGET_SHA:
            raise BudgetError('reviewed original budget hash does not match')
        if (self.config.get('task_id') != '721cb71e50f60575' or Path(self.config.get('local_task_dir', '')) != TASK or
                self.archive_dir.is_symlink() or not self.archive_dir.is_dir() or
                not self.archive_dir.resolve().is_relative_to(TASK.resolve())):
            raise BudgetError('reviewed task or archive path does not match')
        if self.archive_dir.stat().st_uid != os.getuid() or self.archive_dir.stat().st_mode & 0o077:
            raise BudgetError('reviewed archive is not private and task-owned')
        if file_sha(self.archive_dir/'budget-original.json') != OLD_BUDGET_SHA:
            raise BudgetError('immutable original budget copy does not match')
        old_config = json.loads((TASK/'config.json').read_text())
        if (file_sha(TASK/'config.json') != '31ebf0232f702dd93cc62fe542d6a0e7dd4e30bd332e62ee92db11ed8654aa5e' or
                file_sha(self.archive_dir/'config-original.json') != file_sha(TASK/'config.json') or
                canonical_config(old_config) != OLD_CONFIG_HASH or canonical_config(self.config) != new_config_hash):
            raise BudgetError('reviewed configuration identity mismatch')
        for key in old_config:
            if key not in ('capacity_binary', 'capacity_sha256') and self.config.get(key) != old_config[key]:
                raise BudgetError('reviewed configuration changed: '+key)
        if set(self.config) != set(old_config):
            raise BudgetError('reviewed configuration fields changed')
        binary = Path(self.config['capacity_binary'])
        if (not binary.is_absolute() or binary.parent != self.archive_dir or
                file_sha(binary) != self.config['capacity_sha256'] or
                old_config['capacity_sha256'] != OLD_BINARY_SHA):
            raise BudgetError('reviewed binary identity mismatch')
        checks = {
            TASK/'sustained.jsonl.gz': OLD_JOURNAL_SHA,
            self.archive_dir/'journal-original.jsonl.gz': OLD_JOURNAL_SHA,
            TASK/'abort-snapshot.sqlite': OLD_SNAPSHOT_SHA,
            TASK/'seed.sqlite': '70f4535010e15ebeaa9763ce4b2d9cb0d65a8cb9f81bd9b06e7f99c32d64ff61',
            TASK/'fixture.json': '33084756ddd509800083285fd70d196ff7be525e79048e1829cf541a8f3589c4',
            TASK/'seed-manifest.json': 'b6859cc7a67f930914421e0ae6d1bedf1aa4b5262a16686611f1cb14fea3ebfb',
            TASK/'commands.jsonl': '0e64991192e5c2941f99e29c9765b9e2ac496e815a8a37a50b153642790feaab',
            TASK/'independent-final-readback.json': '851015c75503e2c8372da7c7964531716706ea5c7178984814112333f4352501',
            TASK/'adapter-izhsqo__/driver-01-search/process-events.jsonl': '77bf08bf24435e64e9d81d61e0d442a9821a37d16061b6bd1d8d4041fc09b9fa',
            TASK/'adapter-izhsqo__/driver-01-search/deadline.jsonl': 'e3dde362e71f89e79776a71fcdcf70350e06096ff7d50831eb8c63e046ec5b91',
            TASK/'adapter-izhsqo__/driver-01-search/deadline-1.jsonl': '40f8fd6cc8ede11e93b5d7ec998255d8f7cc0f4884c224cbb68e87c5149a6e2c',
            ORACLE: 'b8422ae0d0a80ec1d18de3e428b7dfdcbfef56d6e267301809813add7cde5db4',
        }
        for path, expected in checks.items():
            if file_sha(path) != expected:
                raise BudgetError('reviewed evidence hash mismatch: '+str(path))
        oracle = json.loads(ORACLE.read_text())
        if (oracle.get('verified') is not True or oracle.get('journal_sha256') != OLD_JOURNAL_SHA or
                oracle.get('snapshot_sha256') != OLD_SNAPSHOT_SHA or
                (oracle.get('produced'), oracle.get('confirmed'), oracle.get('pending')) != (188920, 163994, 24926) or
                len(oracle.get('ambiguous_commits', [])) != 8 or
                (oracle.get('daily_reports'), oracle.get('daily_rows_12_metrics'), oracle.get('period_rows_12_metrics')) != (22607, 142512, 920000)):
            raise BudgetError('independent oracle does not prove this checkpoint')
        prior = json.loads((TASK/'independent-final-readback.json').read_text())
        if prior.get('issues') != []:
            raise BudgetError('prior task cleanup was not independently verified')
        active = state.get('active') or {}
        starts = [e for e in state.get('history', []) if e.get('event') == 'begin']
        failures = [e for e in state.get('history', []) if e.get('event') == 'failure']
        if (state.get('version') != 1 or state.get('status') != 'failed' or
                state.get('config_hash') != OLD_CONFIG_HASH or state.get('search_ns') != 0 or
                state.get('confirm_ns') != 0 or state.get('confirm_started') is not False or
                state.get('host_protections') != 0 or active.get('phase') != 'search' or
                active.get('boot_id') != boot() or len(starts) != 1 or len(failures) != 1 or
                starts[0].get('start_ns') != active.get('start_ns') or
                failures[0].get('at_ns', 0)-active.get('start_ns', 0) != OLD_USED_NS or
                state.get('error') != "ToolLimit('generator/transport path invalid: remote SSH worker sustained near one CPU')" or
                state.get('plan') != {'current': 800, 'lower': 400, 'upper': 0, 'uncertain_repeats': 0, 'done': False} or
                state.get('stage_index') != 3):
            raise BudgetError('reviewed active budget or old search plan does not match')
        if time.strftime('%Y-%m-%d', time.gmtime()) != state.get('preflight', {}).get('utc_day'):
            raise BudgetError('UTC day changed; original fixture cannot be fabricated')
        self._check_prior_processes()
        records = [json.loads(line) for line in (TASK/'commands.jsonl').read_text().splitlines()]
        abort = [r for r in records if r.get('event') == 'abort_verification']
        if len(abort) != 1 or abort[0].get('verified') is not True:
            raise BudgetError('reviewed stopped snapshot was not verified')
        verified = abort[0].get('verification', {})
        snapshot = abort[0].get('snapshot', {})
        if ((verified.get('produced'), verified.get('confirmed'), verified.get('pending'),
             verified.get('unconfirmed_commits')) != (188920, 163994, 24926, 8) or
                snapshot.get('snapshot_sha256') != OLD_SNAPSHOT_SHA or
                snapshot.get('source_state', {}).get('open_handles') != []):
            raise BudgetError('reviewed snapshot and debt are inconsistent')
        listener = state['preflight']['remote']['listener']
        if listener != {'comm': 'sshd', 'pid': 881, 'start_ticks': 686, 'nofile': [1024, 524288]}:
            raise BudgetError('original SSH listener baseline differs')
        new = copy.deepcopy(state)
        checkpoint = {'original_budget_sha256': OLD_BUDGET_SHA, 'original_config_hash': OLD_CONFIG_HASH,
                      'original_journal_sha256': OLD_JOURNAL_SHA, 'original_snapshot_sha256': OLD_SNAPSHOT_SHA,
                      'original_capacity_sha256': OLD_BINARY_SHA, 'new_capacity_sha256': self.config['capacity_sha256'],
                      'original_runtime_hashes': state['preflight']['runtime_hashes'], 'original_listener': listener,
                      'parent_oracle_sha256': checks[ORACLE], 'archive_dir': str(self.archive_dir),
                      'accounted_search_ns': OLD_USED_NS}
        new.update({'status': 'reviewed_search_ready', 'active': None, 'search_ns': OLD_USED_NS,
                    'config_hash': new_config_hash, 'reviewed_checkpoint': checkpoint,
                    'plan': {'current': 600, 'lower': 400, 'upper': 0, 'uncertain_repeats': 0, 'done': False},
                    'stage_index': 4, 'preflight': None,
                    'transition_evidence': {'verified': True, **snapshot, 'journal_sha256': OLD_JOURNAL_SHA,
                                            'verification': verified}})
        new['previous_error'] = new.pop('error')
        # This is still read-only against the remote host and occurs while the
        # one global budget lock is held, before the anchor is mutated.
        preflight = self.adapter.preflight(new)
        if not isinstance(preflight, dict) or preflight.get('verified') is not True:
            raise BudgetError('reviewed remote preflight was not proven')
        forecast = self.adapter.preflight_stage(600, 'search', new)
        if not forecast.get('ok'):
            raise BudgetError('reviewed initial search forecast insufficient: '+repr(forecast))
        checkpoint['new_runtime_hashes'] = preflight['runtime_hashes']
        checkpoint['preflight_600'] = forecast
        new['preflight'] = preflight
        new['history'].append({'event': 'reviewed_checkpoint_migration', 'at_ns': clock(),
                               'original_budget_sha256': OLD_BUDGET_SHA, 'search_ns': OLD_USED_NS,
                               'old_config_hash': OLD_CONFIG_HASH, 'new_config_hash': new_config_hash,
                               'journal_sha256': OLD_JOURNAL_SHA, 'snapshot_sha256': OLD_SNAPSHOT_SHA,
                               'resume_rate': 600, 'upper': 0})
        return new

    def _migrate_retry(self, state, raw, new_config_hash, clock, boot):
        # Only the second, independently audited C54 stop is authorized here.
        budget_sha = 'dc40ff7f5adb81424a5dacd948ecddb9edfac6528106b04f27f4643a2a9768ae'
        journal_sha = 'd08cb32d0c2b0dfc7bf0589d80ba6142a6d9be44c8a84c54bab922830a851c6e'
        snapshot_sha = '27404c44ab3ad573229d4dfffcf476ffbe25887911a1c07c188494d7267e5e32'
        config_hash = 'c9fe089a5bfe2ab3fa0079dfe30d07aeeb247a1f55eaea6f9203ec19bfe77bf8'
        prior = TASK/'continuation-4b45a832'
        oracle_path = ROOT/'parent-sustained-continue-review/independent-oracle.json'
        if hashlib.sha256(raw).hexdigest() != budget_sha:
            raise BudgetError('reviewed retry budget hash does not match')
        if (self.config.get('task_id') != '721cb71e50f60575' or Path(self.config.get('local_task_dir', '')) != TASK or
                self.archive_dir.is_symlink() or not self.archive_dir.is_dir() or
                not self.archive_dir.resolve().is_relative_to(TASK.resolve()) or
                self.archive_dir.stat().st_uid != os.getuid() or self.archive_dir.stat().st_mode & 0o077):
            raise BudgetError('reviewed retry task or archive path invalid')
        checks = {
            self.archive_dir/'budget-previous.json': budget_sha,
            self.archive_dir/'journal-previous.jsonl.gz': journal_sha,
            self.archive_dir/'config-previous.json': 'fb7749138cae399c382b7ad0d3711c4def3eefdfcf28e5f72013399a136f2360',
            prior/'config-reviewed.json': 'fb7749138cae399c382b7ad0d3711c4def3eefdfcf28e5f72013399a136f2360',
            TASK/'sustained.jsonl.gz': journal_sha,
            TASK/'abort-continue-4b45a832-snapshot.sqlite': snapshot_sha,
            TASK/'seed.sqlite': '70f4535010e15ebeaa9763ce4b2d9cb0d65a8cb9f81bd9b06e7f99c32d64ff61',
            TASK/'fixture.json': '33084756ddd509800083285fd70d196ff7be525e79048e1829cf541a8f3589c4',
            TASK/'seed-manifest.json': 'b6859cc7a67f930914421e0ae6d1bedf1aa4b5262a16686611f1cb14fea3ebfb',
            prior/'stopped-capture-evidence.json': 'ce2ce780198a4b51b3b8be0d8adad576b262af7e7de17c63c57e7d27caff64c7',
            prior/'final-readonly-summary.json': 'bd87ab453b84e8b3e2746021c115b9ef7748911e82dce389d9c1172fed78c44e',
            TASK/'adapter-cnt2zenz/driver-01-search/process-events.jsonl': '5abe03104adc5e3d6dc160acfa8c85c89d83da050a5c0cce8d7a0d6d5221f42b',
            TASK/'adapter-cnt2zenz/driver-01-search/deadline.jsonl': 'f946161c53b0919d3d0ef58c2efb9e41f638a4cafd44a7dce3815ed9d49f869f',
            TASK/'adapter-cnt2zenz/driver-01-search/deadline-1.jsonl': '635491e0aea15d4437d5ab944c3c3586c751536a763ef30d5ed7184dc1f3b737',
            oracle_path: '3bfc86016411540c47f4f8c5c4578a5b92c5c32c4821660050bc1a22f9370fcb',
        }
        for path, expected in checks.items():
            if file_sha(path) != expected:
                raise BudgetError('reviewed retry evidence hash mismatch: '+str(path))
        old_config = json.loads((prior/'config-reviewed.json').read_text())
        if canonical_config(old_config) != config_hash or canonical_config(self.config) != new_config_hash:
            raise BudgetError('reviewed retry configuration identity mismatch')
        if set(self.config) != set(old_config) | {'max_attempts', 'retry_migration'}:
            raise BudgetError('reviewed retry configuration fields changed')
        for key in old_config:
            if key not in ('capacity_binary', 'capacity_sha256', 'max_pending') and self.config.get(key) != old_config[key]:
                raise BudgetError('unapproved reviewed retry configuration change: '+key)
        if self.config.get('max_pending') not in (125000, 100000) or self.config.get('max_attempts') != 512:
            raise BudgetError('unapproved retry/pending limit')
        binary = Path(self.config['capacity_binary'])
        manifest_path = Path(self.config['retry_migration'])
        if (not binary.is_absolute() or binary.parent != self.archive_dir or file_sha(binary) != self.config['capacity_sha256'] or
                old_config['capacity_sha256'] != 'fbebd0bca029514e0a3728e238fe0986301ea7a23ecd3d704dbb8443e062546a' or
                manifest_path != self.archive_dir/'retry-migration.json'):
            raise BudgetError('reviewed retry binary or manifest identity mismatch')
        manifest = json.loads(manifest_path.read_text())
        expected_manifest = {'approval': 'c54-721cb71e50f60575-retry-30-to-512',
                             'previous_journal_sha256': journal_sha, 'previous_journal_bytes': (TASK/'sustained.jsonl.gz').stat().st_size,
                             'previous_budget_sha256': budget_sha, 'previous_config_hash': config_hash,
                             'next_config_hash': new_config_hash, 'previous_binary_sha256': old_config['capacity_sha256'],
                             'next_binary_sha256': self.config['capacity_sha256'], 'previous_max_attempts': 30,
                             'next_max_attempts': 512, 'previous_max_pending': 125000,
                             'next_max_pending': self.config['max_pending'], 'task_sequence': 130422,
                             'task_client': 9034, 'task_revision': 54, 'task_day': '2026-11-13',
                             'task_body_sha256': 'ad6e280211e23aa21566bdad0074a0045452d71dbb1ced4d748aea1b21448a1a',
                             'task_attempts': 30, 'next_task_attempt': 31,
                             'task_finish_unix_ns': 1794560887575625142, 'task_retry_after_ns': 1000000000,
                             'task_not_before_unix_ns': 1794560902575625142}
        if manifest != expected_manifest:
            raise BudgetError('reviewed retry manifest does not match exact migration')
        oracle = json.loads(oracle_path.read_text())
        exhausted = {'sequence': 130422, 'client': 9034, 'attempts': 30, 'all_statuses_503': True, 'still_pending': True}
        if (oracle.get('verified') is not True or oracle.get('journal_sha256') != journal_sha or
                oracle.get('snapshot_sha256') != snapshot_sha or
                (oracle.get('produced'), oracle.get('confirmed'), oracle.get('pending')) != (363619, 334393, 29226) or
                len(oracle.get('ambiguous_commits', [])) != 4 or len(oracle.get('ambiguous_registrations', [])) != 1 or
                (oracle.get('daily_reports'), oracle.get('daily_rows_12_metrics'), oracle.get('period_rows_12_metrics')) != (25684, 165997, 920000) or
                oracle.get('exhausted_task') != exhausted):
            raise BudgetError('parent oracle does not prove reviewed retry debt')
        capture = json.loads((prior/'stopped-capture-evidence.json').read_text())
        verification, snapshot = capture.get('verification', {}), capture.get('snapshot', {})
        if (capture.get('capture_verified') is not True or capture.get('ledger_database_verified') is not True or
                capture.get('owned_watch_session_cleaned') is not True or snapshot.get('snapshot_sha256') != snapshot_sha or
                snapshot.get('source_state', {}).get('open_handles') != [] or
                (verification.get('produced'), verification.get('confirmed'), verification.get('pending'),
                 verification.get('unconfirmed_commits'), verification.get('ambiguous_registrations')) != (363619, 334393, 29226, 4, 1)):
            raise BudgetError('reviewed retry stopped snapshot is inconsistent')
        active = state.get('active') or {}
        begins = [e for e in state.get('history', []) if e.get('event') == 'begin']
        failures = [e for e in state.get('history', []) if e.get('event') == 'failure']
        if (state.get('version') != 1 or state.get('status') != 'failed' or state.get('config_hash') != config_hash or
                state.get('search_ns') != 612018194623 or state.get('confirm_ns') != 0 or
                state.get('confirm_started') is not False or state.get('host_protections') != 0 or
                active.get('phase') != 'search' or active.get('boot_id') != boot() or len(begins) != 2 or len(failures) != 2 or
                begins[-1].get('start_ns') != active.get('start_ns') or
                failures[0].get('at_ns', 0)-begins[0].get('start_ns', 0) != 612018194623 or
                failures[-1].get('at_ns', 0)-active.get('start_ns', 0) != 302284608837 or
                state.get('error') != "ControllerError('unexpected stage/target in sample stream')" or
                state.get('plan') != {'current': 500, 'lower': 400, 'upper': 600, 'uncertain_repeats': 0, 'done': False} or
                state.get('stage_index') != 5):
            raise BudgetError('reviewed retry active budget or plan mismatch')
        if time.strftime('%Y-%m-%d', time.gmtime()) != state.get('preflight', {}).get('utc_day'):
            raise BudgetError('UTC day changed; original fixture cannot be fabricated')
        self._check_prior_processes()
        listener = state['preflight']['remote']['listener']
        if listener != {'comm': 'sshd', 'pid': 881, 'start_ticks': 686, 'nofile': [1024, 524288]}:
            raise BudgetError('reviewed retry SSH listener baseline differs')
        new = copy.deepcopy(state)
        checkpoint = copy.deepcopy(state['reviewed_checkpoint'])
        checkpoint.update({'previous_budget_sha256': budget_sha, 'previous_config_hash': config_hash,
                           'previous_journal_sha256': journal_sha, 'previous_snapshot_sha256': snapshot_sha,
                           'previous_capacity_sha256': old_config['capacity_sha256'], 'new_capacity_sha256': self.config['capacity_sha256'],
                           'previous_runtime_hashes': state['preflight']['runtime_hashes'], 'original_listener': listener,
                           'parent_retry_oracle_sha256': checks[oracle_path], 'retry_manifest_sha256': file_sha(manifest_path),
                           'retry_manifest': manifest, 'retry_archive_dir': str(self.archive_dir),
                           'accounted_search_ns': 914302803460, 'replay_expected': [363619, 334393, 29226, 0]})
        new.update({'status': 'reviewed_search_ready', 'active': None, 'search_ns': 914302803460,
                    'config_hash': new_config_hash, 'reviewed_checkpoint': checkpoint,
                    'plan': {'current': 500, 'lower': 400, 'upper': 600, 'uncertain_repeats': 0, 'done': False},
                    'stage_index': 6, 'preflight': None,
                    'transition_evidence': {'verified': True, **snapshot, 'journal_sha256': journal_sha,
                                            'verification': verification}})
        new['retry_previous_error'] = new.pop('error')
        preflight = self.adapter.preflight(new)
        if not isinstance(preflight, dict) or preflight.get('verified') is not True:
            raise BudgetError('reviewed retry remote preflight was not proven')
        forecast = self.adapter.preflight_stage(500, 'search', new)
        if not forecast.get('ok'):
            raise BudgetError('reviewed retry initial search forecast insufficient: '+repr(forecast))
        checkpoint['new_runtime_hashes'] = preflight['runtime_hashes']
        checkpoint['preflight_500'] = forecast
        new['preflight'] = preflight
        new['history'].append({'event': 'reviewed_retry_checkpoint_migration', 'at_ns': clock(),
                               'previous_budget_sha256': budget_sha, 'search_ns': 914302803460,
                               'old_config_hash': config_hash, 'new_config_hash': new_config_hash,
                               'journal_sha256': journal_sha, 'snapshot_sha256': snapshot_sha,
                               'retry_manifest_sha256': checkpoint['retry_manifest_sha256'],
                               'old_max_attempts': 30, 'new_max_attempts': 512,
                               'old_max_pending': 125000, 'new_max_pending': self.config['max_pending'],
                               'resume_rate': 500, 'upper': 600, 'stage_index': 6})
        return new
