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
    def __init__(self, config, archive_dir, adapter):
        self.config = dict(config)
        self.archive_dir = Path(archive_dir)
        self.adapter = adapter

    def _check_prior_processes(self):
        # Refuse even a recycled PID rather than guessing ownership. No other
        # controller or task tunnel may be alive during the migration.
        for pid in (3184171, 3184172, 3184214, 3184226, 3184364, 3184406, 3184407, 3184415):
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
            if (any(x.endswith('/sustained_controller.py') or x == 'tools/capacity/sustained_controller.py' for x in args) or
                any(x.endswith('/ranking-capacity') for x in args) and any('sustained' in x for x in args) or
                any(Path(x).name == 'ssh' for x in args) and any('14210:127.0.0.1:4210' in x for x in args)):
                raise BudgetError('another controller, generator or task tunnel exists: '+proc.name)

    def migrate(self, state, raw, new_config_hash, clock, boot):
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
