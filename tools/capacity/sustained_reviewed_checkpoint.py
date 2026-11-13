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
from business_day import business_day
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
CONFIRM_BUDGET_SHA = 'aef1d32c67ba9ee27c6a024a0c62ccdfa1d6d358f1b95e4e878911fa206d9abd'
CONFIRM_JOURNAL_SHA = '190c540a1b691be86e8d3f4183ceaf362c22cdd42fe8fe6ab5586d48ed6018f1'
CONFIRM_SNAPSHOT_SHA = '7d594c6162e37d7a314b6a9431ef5fe8b31dc26315e9c3c5f0cbb5898f7db5f2'
CONFIRM_CONFIG_HASH = 'ef9b5227da220e555079b1dd2d09bacd11b2af3e80ffafa75da796ea8e72cd5b'
PENDING_RESERVED_SHA = '76f82e013e26318ee23f7161e3e40187ac927f7e95dcede9c85f4e115199af9d'
CONFIRM_PREFLIGHT_WAIT = 180  # idle-only; no formal arrivals or budget reset


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
        if kind not in ('original', 'retry', 'confirmation', 'pending'):
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
                    3244196, 3244210, 3246156, 3285924, 3285927, 3285966, 3285978,
                    3286037, 3286038, 3286079, 3299724, 3339803, 3353045):
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
            if (any(Path(x).name in ('sustained_controller.py', 'sustained_session.py', 'sustained_deadline.py',
                                      'wait-for-confirmation-memory.py', 'launch-confirmation-v2.sh') for x in args) or
                any(Path(x).name.startswith('ranking-capacity') for x in args) and
                any(x in ('sustained', 'verify-sustained', 'summarize-sustained') for x in args) or
                any(Path(x).name == 'ssh' for x in args) and any('14210:127.0.0.1:4210' in x for x in args)):
                raise BudgetError('another controller, generator, watchdog, session or task tunnel exists: '+proc.name)

    def migrate(self, state, raw, new_config_hash, clock, boot):
        if 'utc_day' in (state.get('preflight') or {}):
            raise BudgetError('legacy UTC-day state cannot be reused; rebuild Shanghai fixtures')
        if self.kind == 'retry':
            return self._migrate_retry(state, raw, new_config_hash, clock, boot)
        if self.kind in ('confirmation', 'pending'):
            return self._migrate_confirmation(state, raw, new_config_hash, clock, boot)
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
        if business_day() != state.get('preflight', {}).get('business_day'):
            raise BudgetError('Shanghai business day changed; original fixture cannot be fabricated')
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
        if business_day() != state.get('preflight', {}).get('business_day'):
            raise BudgetError('Shanghai business day changed; original fixture cannot be fabricated')
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

    def _migrate_confirmation(self, state, raw, new_config_hash, clock, boot):
        # Only this clean, verified planned transition failed before the one
        # confirmation was begun, solely because a local forecast was low.
        # It is not permission to recover any active/unknown/confirmed failure.
        pending = self.kind == 'pending'
        reserved_state = state
        if pending:
            if hashlib.sha256(raw).hexdigest() != PENDING_RESERVED_SHA or file_sha(self.archive_dir/'budget-before-reservation.json') != CONFIRM_BUDGET_SHA:
                raise BudgetError('pending reservation or original budget hash differs')
            original = json.loads((self.archive_dir/'budget-before-reservation.json').read_text())
            reservation = state.get('pending_limit_migration_reservation', {})
            if (reservation.get('approval') != 'c54-721cb71e50f60575-pending-125000-to-100000' or
                    reservation.get('previous_budget_sha256') != CONFIRM_BUDGET_SHA or
                    reservation.get('previous_journal_sha256') != CONFIRM_JOURNAL_SHA or
                    reservation.get('previous_config_hash') != CONFIRM_CONFIG_HASH or
                    reservation.get('previous_snapshot_sha256') != CONFIRM_SNAPSHOT_SHA or
                    (reservation.get('previous_max_pending'), reservation.get('next_max_pending'), reservation.get('max_attempts'),
                     reservation.get('existing_pending'), reservation.get('search_ns'), reservation.get('confirm_started'),
                     reservation.get('confirm_rate')) != (125000, 100000, 512, 16330, 1095787699798, False, 500)):
                raise BudgetError('pending reservation does not match the explicit approval')
            expected = copy.deepcopy(original)
            expected['status'] = 'pending_limit_migration_reserved'
            expected['pending_limit_migration_reservation'] = reservation
            events = state.get('history', [])
            if not events or events[-1].get('event') != 'approved_pending_limit_migration_reserved':
                raise BudgetError('pending reservation history missing')
            expected['history'].append(events[-1])
            if expected != state:
                raise BudgetError('pending reservation changed unrelated budget fields')
            state = original
            prior_archive = TASK/'retry-resume-20261113-01'
        else:
            if hashlib.sha256(raw).hexdigest() != CONFIRM_BUDGET_SHA:
                raise BudgetError('confirmation transition budget hash differs')
            prior_archive = self.archive_dir
        if (self.config.get('task_id') != '721cb71e50f60575' or Path(self.config.get('local_task_dir', '')) != TASK or
                self.archive_dir.is_symlink() or not self.archive_dir.is_dir() or
                not self.archive_dir.resolve().is_relative_to(TASK.resolve()) or
                self.archive_dir.stat().st_uid != os.getuid() or self.archive_dir.stat().st_mode & 0o077):
            raise BudgetError('confirmation transition archive or task differs')
        adapter_dir = TASK/'adapter-8qvek47s'
        checks = {
            prior_archive/'confirmation-transition-budget.json': CONFIRM_BUDGET_SHA,
            prior_archive/'confirmation-transition-journal.jsonl.gz': CONFIRM_JOURNAL_SHA,
            TASK/'sustained.jsonl.gz': CONFIRM_JOURNAL_SHA,
            adapter_dir/'transition-snapshot.sqlite': CONFIRM_SNAPSHOT_SHA,
            adapter_dir/'abort-snapshot.sqlite': CONFIRM_SNAPSHOT_SHA,
            adapter_dir/'controller-events.jsonl': '198f1aba47c7d23fa54b7bfebbaa78ca5b1675341f1b2bf899886c09cebfc617',
            adapter_dir/'commands.jsonl': 'e20cb879da49a318d6f4973adf68fcc7863f6e32b28ccb2c127925f7aaec0bd1',
            adapter_dir/'driver-01-search/process-events.jsonl': 'bdaff50e04de031a8a515c1fa797cbcb40b73df542a2aa76075bed45d0963f91',
            adapter_dir/'driver-01-search/deadline.jsonl': '9f6531decf984c32ab6dd24da90599bc5bb4f20f9622b03d0dcd327d3faecaa4',
            adapter_dir/'driver-01-search/deadline-1.jsonl': 'db9d91f0364069a55a3cfb35a5d4a332217a685f50baa0a9eb004629f369c735',
            adapter_dir/'verify-transition-3.stdout': '40007481fd6eac9d9d5fc329a719ca5791ceb6bda6d26c5c83e6de0a26061e91',
            adapter_dir/'summary-transition-4.stdout': 'a1121b543a48a636e0b3f37ab4bfb27cda2d65be6a06b084d8f69bb71ee4c113',
            TASK/'session-xb45svmp/session-events.jsonl': 'defebcbaab266ce416a5c1971af0d8e0bc0b9ed3309e9a995b7ffb1dd7059f41',
            prior_archive/'retry-controller.rc': '4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865',
            prior_archive/'retry-controller.stderr': '1f1bfc9e12f04991ac17fd263de1334a2720d8b91546c3b6212eb2d27b5643db',
            prior_archive/'config-retry.json': '4a601676bb22bb93768c34459bf3d34a7f4d6ab454f40d8e71d371f48746b6c0',
            prior_archive/'retry-migration.json': 'c8b7f6e39305ff8ad49baa8449c85cfcc58a2d930be7de44e0bad183c233b295',
            TASK/'seed.sqlite': '70f4535010e15ebeaa9763ce4b2d9cb0d65a8cb9f81bd9b06e7f99c32d64ff61',
            TASK/'fixture.json': '33084756ddd509800083285fd70d196ff7be525e79048e1829cf541a8f3589c4',
        }
        if pending:
            checks[self.archive_dir/'journal-before-pending-migration.jsonl.gz'] = CONFIRM_JOURNAL_SHA
        for path, expected in checks.items():
            if file_sha(path) != expected:
                raise BudgetError('confirmation transition evidence hash mismatch: '+str(path))
        old_config = json.loads((prior_archive/'config-retry.json').read_text())
        if canonical_config(old_config) != CONFIRM_CONFIG_HASH or canonical_config(self.config) != new_config_hash:
            raise BudgetError('confirmation configuration hash changed')
        pending_manifest = None
        if pending:
            if (set(self.config) != set(old_config) | {'pending_migration'} or
                    any(self.config.get(k) != v for k, v in old_config.items()
                        if k not in ('capacity_binary', 'capacity_sha256', 'max_pending')) or
                    self.config.get('max_pending') != 100000 or self.config.get('max_attempts') != 512 or
                    Path(self.config['capacity_binary']).parent != self.archive_dir or
                    Path(self.config['pending_migration']) != self.archive_dir/'pending-migration.json' or
                    file_sha(self.config['capacity_binary']) != self.config['capacity_sha256']):
                raise BudgetError('unapproved configuration change during pending migration')
            pending_manifest = json.loads((self.archive_dir/'pending-migration.json').read_text())
            expected_manifest = {'approval': 'c54-721cb71e50f60575-pending-125000-to-100000',
                'previous_journal_sha256': CONFIRM_JOURNAL_SHA,
                'previous_journal_bytes': (TASK/'sustained.jsonl.gz').stat().st_size,
                'previous_budget_sha256': CONFIRM_BUDGET_SHA, 'reserved_budget_sha256': PENDING_RESERVED_SHA,
                'previous_config_hash': CONFIRM_CONFIG_HASH, 'next_config_hash': new_config_hash,
                'previous_binary_sha256': old_config['capacity_sha256'], 'next_binary_sha256': self.config['capacity_sha256'],
                'previous_max_pending': 125000, 'next_max_pending': 100000, 'max_attempts': 512,
                'produced': 456569, 'confirmed': 440239, 'pending': 16330}
            if pending_manifest != expected_manifest:
                raise BudgetError('pending migration manifest differs from exact approved boundary')
        elif (new_config_hash != CONFIRM_CONFIG_HASH or old_config != self.config or
                file_sha(Path(self.config['capacity_binary'])) != self.config['capacity_sha256'] or
                self.config.get('capacity_sha256') != 'a56cfa16fba285e31e4d364138d6569838ecea682e8096e0964a5291302b3f1b' or
                self.config.get('max_attempts') != 512 or self.config.get('max_pending') != 125000):
            raise BudgetError('confirmation configuration or binary changed')
        expected_plan = {'current': 550, 'done': True, 'lower': 500, 'upper': 600,
                         'reason': 'next search resource forecast insufficient', 'uncertain_repeats': 0}
        if (state.get('version') != 1 or state.get('status') != 'failed' or state.get('active') is not None or
                state.get('config_hash') != CONFIRM_CONFIG_HASH or state.get('search_ns') != 1095787699798 or
                state.get('confirm_ns') != 0 or state.get('confirm_started') is not False or
                state.get('host_protections') != 0 or state.get('plan') != expected_plan or
                state.get('stage_index') != 7 or state.get('confirm_rate') != 500 or
                not str(state.get('error', '')).startswith('ResourceForecastLimit(') or
                "'issues': ['local_memory_insufficient']" not in state.get('error', '')):
            raise BudgetError('confirmation failure was not the specified inactive forecast failure')
        evidence = state.get('transition_evidence', {})
        verification = evidence.get('verification', {})
        if (evidence.get('verified') is not True or evidence.get('journal_sha256') != CONFIRM_JOURNAL_SHA or
                evidence.get('snapshot_sha256') != CONFIRM_SNAPSHOT_SHA or
                evidence.get('snapshot_path') != str(adapter_dir/'transition-snapshot.sqlite') or
                evidence.get('pending_at_transition') != 16330 or evidence.get('source_state', {}).get('open_handles') != [] or
                (verification.get('produced'), verification.get('confirmed'), verification.get('pending'),
                 verification.get('unconfirmed_commits'), verification.get('ambiguous_registrations')) != (456569, 440239, 16330, 16, 0)):
            raise BudgetError('clean transition proof or debt does not match')
        history = state.get('history', [])
        if (len(history) < 2 or history[-2].get('event') != 'end' or history[-2].get('phase') != 'search' or
                history[-2].get('search_ns') != 1095787699798 or history[-1].get('event') != 'failure'):
            raise BudgetError('confirmation transition budget history incomplete')
        if business_day() != state.get('preflight', {}).get('business_day'):
            raise BudgetError('Shanghai business day changed; original fixture cannot be fabricated')
        events = [json.loads(line) for line in (adapter_dir/'controller-events.jsonl').read_text().splitlines()]
        assessments = [x for x in events if x.get('event') == 'assessment' and x.get('phase') == 'search']
        transitions = [x for x in events if x.get('event') == 'planned_transition']
        if (len(assessments) != 1 or assessments[0].get('result', {}).get('stage', {}).get('verdict') != 'candidate' or
                assessments[0]['result']['stage'].get('target_rate') != 500 or
                len(transitions) != 1 or transitions[0].get('evidence') != evidence):
            raise BudgetError('500 candidate or clean transition evidence missing')
        process = [json.loads(line) for line in (adapter_dir/'driver-01-search/process-events.jsonl').read_text().splitlines()]
        if (len([e for e in process if e.get('event') == 'driver_exit' and e.get('returncode') == 0]) != 1 or
                len([e for e in process if e.get('event') == 'deadline_exit' and e.get('returncode') == 0]) != 2):
            raise BudgetError('driver or independent deadlines did not exit cleanly')
        self._check_prior_processes()
        listener = state['preflight']['remote']['listener']
        if listener != {'comm': 'sshd', 'pid': 881, 'start_ticks': 686, 'nofile': [1024, 524288]}:
            raise BudgetError('original SSH listener baseline differs')
        new = copy.deepcopy(reserved_state if pending else state)
        checkpoint = new['reviewed_checkpoint']
        if pending:
            checkpoint['previous_capacity_sha256'] = old_config['capacity_sha256']
            checkpoint['new_capacity_sha256'] = self.config['capacity_sha256']
            checkpoint['pending_manifest_sha256'] = file_sha(self.archive_dir/'pending-migration.json')
            checkpoint['pending_manifest'] = pending_manifest
            new['config_hash'] = new_config_hash
        checkpoint['previous_confirmation_forecast_budget_sha256'] = CONFIRM_BUDGET_SHA
        checkpoint['previous_confirmation_runtime_hashes'] = state['preflight']['runtime_hashes']
        new['status'] = 'transition_ready'
        new['preflight'] = None
        new['previous_confirmation_forecast_error'] = new.pop('error')
        preflight = self.adapter.preflight(new)
        if not isinstance(preflight, dict) or preflight.get('verified') is not True:
            raise BudgetError('confirmation remote preflight was not proven')
        if preflight.get('remote', {}).get('listener') != listener or self.adapter._snapshot_state() != evidence['source_state']:
            raise BudgetError('confirmation stopped database or listener changed')
        remote_hash = self.adapter.commands.ssh(['/usr/bin/sha256sum', '--', self.adapter.spec.db], timeout=120).stdout.split()
        if not remote_hash or remote_hash[0] != CONFIRM_SNAPSHOT_SHA:
            raise BudgetError('confirmation remote DB content changed')
        # The task is stopped with no active load. A momentary local memory
        # forecast miss may clear without changing any limit; wait only a
        # bounded interval, then rerun the remote identity/snapshot checks.
        end = time.monotonic()+(0 if pending else CONFIRM_PREFLIGHT_WAIT)
        waited = False
        while True:
            forecast = self.adapter.preflight_stage(500, 'confirmation', new)
            if forecast.get('ok'):
                if waited:
                    preflight = self.adapter.preflight(new)
                    if (not preflight.get('verified') or preflight.get('remote', {}).get('listener') != listener or
                            self.adapter._snapshot_state() != evidence['source_state']):
                        raise BudgetError('confirmation remote preflight changed while waiting')
                    remote_hash = self.adapter.commands.ssh(['/usr/bin/sha256sum', '--', self.adapter.spec.db], timeout=120).stdout.split()
                    if not remote_hash or remote_hash[0] != CONFIRM_SNAPSHOT_SHA:
                        raise BudgetError('confirmation DB changed while waiting')
                    forecast = self.adapter.preflight_stage(500, 'confirmation', new)
                    if forecast.get('ok'):
                        break
                else:
                    break
            issues = set(forecast.get('issues', [])) if isinstance(forecast, dict) else set()
            if issues != {'local_memory_insufficient'} or time.monotonic() >= end:
                raise BudgetError('confirmation resource forecast insufficient: '+repr(forecast))
            waited = True
            time.sleep(min(5, max(0, end-time.monotonic())))
        checkpoint['confirmation_runtime_hashes'] = preflight['runtime_hashes']
        checkpoint['confirmation_forecast'] = forecast
        new['preflight'] = preflight
        new['history'].append({'event': 'reviewed_clean_confirmation_transition', 'at_ns': clock(),
                               'previous_budget_sha256': CONFIRM_BUDGET_SHA, 'journal_sha256': CONFIRM_JOURNAL_SHA,
                               'snapshot_sha256': CONFIRM_SNAPSHOT_SHA, 'search_ns': 1095787699798,
                               'confirm_ns': 0, 'confirm_rate': 500, 'previous_forecast': 'local_memory_insufficient',
                               **({'reserved_budget_sha256': PENDING_RESERVED_SHA, 'old_config_hash': CONFIRM_CONFIG_HASH,
                                   'new_config_hash': new_config_hash, 'old_max_pending': 125000, 'new_max_pending': 100000,
                                   'pending_manifest_sha256': checkpoint['pending_manifest_sha256']} if pending else {})})
        return new
