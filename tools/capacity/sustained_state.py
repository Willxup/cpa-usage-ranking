"""Fail-closed persistent C54 active-time ledger; no implicit crash recovery."""
import fcntl
import json
import os
import time
import uuid
from pathlib import Path

NS = 1_000_000_000
SEARCH_LIMIT = 2400 * NS
TOTAL_LIMIT = 3600 * NS
CONFIRM_MIN = 1200 * NS
# Startup, final sampling, and orderly stop all consume the same budget.
CONFIRM_RESERVE = 1210 * NS


def boot_clock_ns():
    return time.clock_gettime_ns(time.CLOCK_BOOTTIME)


def boot_id():
    return Path('/proc/sys/kernel/random/boot_id').read_text().strip()


class BudgetError(RuntimeError):
    pass


class Budget:
    def __init__(self, path, config_hash, clock=boot_clock_ns, boot=boot_id):
        self.path = Path(path)
        self.clock, self.boot = clock, boot
        self.lock = open(str(self.path)+'.lock', 'a+', encoding='utf-8')
        try:
            os.chmod(self.lock.name, 0o600)
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            if self.path.exists():
                self.state = json.loads(self.path.read_text())
                if self.state.get('version') != 1 or self.state.get('config_hash') != config_hash:
                    raise BudgetError('state/config mismatch; no automatic restart')
                if self.state.get('active') is not None:
                    # The deadline watchdog stops the local driver, but the
                    # exact previous active interval/ledger may be unknown.
                    self.state['status'] = 'failed_unknown_active_interval'
                    self.state['error'] = 'unclean prior process; do not resume formal load'
                    self.save()
                    raise BudgetError(self.state['error'])
                if self.state.get('status') != 'transition_ready':
                    raise BudgetError('state is not a clean planned transition')
            else:
                self.state = {'version': 1, 'config_hash': config_hash, 'status': 'new',
                              'search_ns': 0, 'confirm_ns': 0, 'confirm_started': False,
                              'host_protections': 0, 'active': None, 'history': []}
                self.save()
            self.validate()
        except BaseException:
            self.close()
            raise

    def close(self):
        if getattr(self, 'lock', None) is not None:
            self.lock.close()
            self.lock = None

    def save(self):
        data = (json.dumps(self.state, sort_keys=True, separators=(',', ':'))+'\n').encode()
        temp = self.path.with_name('.'+self.path.name+'.'+uuid.uuid4().hex+'.tmp')
        fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        try:
            with os.fdopen(fd, 'wb') as out:
                out.write(data)
                out.flush()
                os.fsync(out.fileno())
            os.replace(temp, self.path)
            parent = os.open(self.path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(parent)
            finally:
                os.close(parent)
        finally:
            try:
                temp.unlink()
            except FileNotFoundError:
                pass

    def validate(self):
        s = self.state
        if (type(s.get('search_ns')) is not int or type(s.get('confirm_ns')) is not int or
                min(s['search_ns'], s['confirm_ns']) < 0 or s['search_ns'] > SEARCH_LIMIT or
                s['search_ns'] + s['confirm_ns'] > TOTAL_LIMIT or
                type(s.get('host_protections')) is not int or s['host_protections'] < 0 or
                type(s.get('confirm_started')) is not bool or not isinstance(s.get('history'), list)):
            raise BudgetError('invalid or unknown persisted budget')
        if ((s['confirm_ns'] and not s['confirm_started']) or
                (s['status'] == 'new' and (s['search_ns'] or s['confirm_ns'] or s['confirm_started'] or s['history'])) or
                (s['status'] == 'transition_ready' and s['confirm_started'])):
            raise BudgetError('persisted budget state is internally inconsistent')

    def elapsed(self):
        s = self.state
        search, confirm = s['search_ns'], s['confirm_ns']
        active = s['active']
        if active is not None:
            if active['boot_id'] != self.boot():
                raise BudgetError('clock epoch changed; active budget unknown')
            delta = self.clock() - active['start_ns']
            if delta < 0:
                raise BudgetError('active clock moved backwards')
            if active['phase'] == 'search':
                search += delta
            elif active['phase'] == 'confirmation':
                confirm += delta
            else:
                raise BudgetError('unknown active phase')
        return search, confirm

    def mark_loading(self, phase):
        s = self.state
        if s['active'] is not None or s['status'] not in ('new', 'transition_ready'):
            raise BudgetError('cannot start paused driver from this state')
        if phase == 'search' and (s['status'] != 'new' or s['confirm_started']):
            raise BudgetError('search driver cannot be restarted here')
        if phase == 'confirmation' and (s['status'] != 'transition_ready' or s['confirm_started']):
            raise BudgetError('confirmation driver cannot be started here')
        if phase not in ('search', 'confirmation'):
            raise BudgetError('unknown loading phase')
        s['status'] = 'loading_'+phase
        s['history'].append({'event': 'paused_driver_loading', 'phase': phase, 'at_ns': self.clock()})
        self.save()

    def begin(self, phase):
        self.validate()
        s = self.state
        if s['active'] is not None or s['status'] not in ('new', 'transition_ready', 'loading_'+phase):
            raise BudgetError('cannot begin from this state')
        if s['host_protections'] >= 2:
            raise BudgetError('two host protections already occurred')
        total = s['search_ns'] + s['confirm_ns']
        if phase == 'search':
            if s['status'] not in ('new', 'loading_search') or s['confirm_started']:
                raise BudgetError('search after a planned transition or confirmation is prohibited')
            allowance = min(SEARCH_LIMIT-s['search_ns'], TOTAL_LIMIT-total-CONFIRM_RESERVE)
            if allowance < 181*NS:
                raise BudgetError('insufficient search time while reserving confirmation')
        elif phase == 'confirmation':
            if s['status'] not in ('transition_ready', 'loading_confirmation'):
                raise BudgetError('confirmation requires a clean search transition')
            if s['confirm_started'] or s['confirm_ns']:
                raise BudgetError('the only confirmation has already been attempted')
            if TOTAL_LIMIT-total < CONFIRM_RESERVE:
                raise BudgetError('insufficient remaining continuous confirmation budget')
            allowance = min(CONFIRM_RESERVE, TOTAL_LIMIT-total)
            s['confirm_started'] = True
        else:
            raise BudgetError('unknown phase')
        start = self.clock()
        s['active'] = {'phase': phase, 'start_ns': start, 'deadline_ns': start+allowance,
                       'boot_id': self.boot()}
        s['status'] = 'active_'+phase
        s['history'].append({'event': 'begin', 'phase': phase, 'start_ns': start,
                             'deadline_ns': start+allowance})
        self.save()  # Persist before ANY stage command can start arrivals.
        return start+allowance

    def end_clean(self, ledger_closed, next_status):
        s = self.state
        if s['active'] is None or not ledger_closed:
            raise BudgetError('clean closed journal is required before transition')
        phase = s['active']['phase']
        if next_status not in ('transition_stopped', 'confirmation_stopped', 'stopped'):
            raise BudgetError('invalid final state')
        if next_status == 'transition_stopped' and phase != 'search':
            raise BudgetError('confirmation may not transition or restart')
        if next_status == 'confirmation_stopped' and phase != 'confirmation':
            raise BudgetError('confirmation has not run')
        search, confirm = self.elapsed()
        s['search_ns'], s['confirm_ns'] = search, confirm
        s['history'].append({'event': 'end', 'phase': phase, 'at_ns': self.clock(),
                             'search_ns': search, 'confirm_ns': confirm})
        s['active'] = None
        if search > SEARCH_LIMIT or search+confirm > TOTAL_LIMIT:
            s['status'] = 'failed_budget_exceeded'
            self.save()
            raise BudgetError('active budget exceeded; stop and preserve evidence')
        s['status'] = next_status
        self.save()

    def transition_ready(self, evidence):
        if self.state['status'] != 'transition_stopped' or self.state['active'] is not None:
            raise BudgetError('transition has not stopped cleanly')
        if not isinstance(evidence, dict) or evidence.get('verified') is not True:
            raise BudgetError('same-database transition and ledger must be verified')
        self.state['transition_evidence'] = evidence
        self.state['status'] = 'transition_ready'
        self.save()

    def confirmation_result(self, assessment, evidence):
        if self.state['status'] != 'confirmation_stopped' or self.state['active'] is not None:
            raise BudgetError('confirmation has not stopped cleanly')
        if not isinstance(evidence, dict) or evidence.get('verified') is not True:
            raise BudgetError('final stopped database and ledger not verified')
        if not isinstance(assessment, dict):
            raise BudgetError('missing final assessment')
        self.state['final_assessment'] = assessment
        self.state['final_evidence'] = evidence
        if assessment.get('verdict') == 'confirmed' and self.state['confirm_ns'] >= CONFIRM_MIN:
            self.state['status'] = 'completed'
        else:
            self.state['status'] = 'stopped_unconfirmed'
        self.save()

    def fail(self, reason, host_protection=False):
        s = self.state
        if host_protection:
            s['host_protections'] += 1
        # Retain active/start/boot data when closure cannot be proved. This
        # deliberately prevents a restart instead of guessing elapsed time.
        s['status'] = 'failed'
        s['error'] = str(reason)
        s['history'].append({'event': 'failure', 'reason': str(reason), 'at_ns': self.clock()})
        self.save()
