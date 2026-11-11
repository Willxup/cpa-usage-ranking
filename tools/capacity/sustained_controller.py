#!/usr/bin/env python3
"""C54 sustained search/confirmation controller.

Dry-run is the default. Remote execution requires the explicit execute flag;
local mock results are never capacity measurements.
"""
import argparse
import hashlib
import json
import os
import re
import signal
import subprocess
import sys
import time
sys.dont_write_bytecode = True
from pathlib import Path

from sustained_process import SampleTimeout
from sustained_state import BudgetError, NS


class ControllerError(RuntimeError):
    pass


class HostProtection(ControllerError):
    host_protection = True


class ToolLimit(ControllerError):
    pass


class GoAssessment:
    def __init__(self, executable, command_log):
        self.executable = str(executable)
        self.log = Path(command_log)

    def __call__(self, request):
        try:
            result = subprocess.run([self.executable, 'assess-sustained'], input=json.dumps(request),
                                    capture_output=True, text=True, timeout=30)
        except BaseException as exc:
            self._record({'returncode': None, 'stderr': repr(exc)})
            raise
        self._record({'returncode': result.returncode, 'stderr': result.stderr,
                      'stdout': result.stdout, 'mode': request['mode']})
        if result.returncode:
            raise ControllerError('Go assessment failed: '+result.stderr[-1000:])
        return json.loads(result.stdout)

    def _record(self, row):
        with self.log.open('a', encoding='utf-8') as f:
            f.write(json.dumps({'time_ns': time.time_ns(), **row}, sort_keys=True)+'\n')
            f.flush()


class Controller:
    """Actually issues stage commands through the injected driver interface.

    Adapter contracts are strict: preflight, prepare_phase, start_driver, guard,
    transition, finalize and cleanup. transition/finalize return independently verified
    evidence dictionaries. Formal execution must not substitute this contract
    for an unimplemented remote protection chain.
    """
    def __init__(self, budget, adapter, assess, events, *, handoff_after_transition=False):
        self.budget, self.adapter, self.assess = budget, adapter, assess
        self.handoff_after_transition = handoff_after_transition
        self.events = Path(events)
        self.driver = None
        self.last_cache = None
        self.cache = []
        self.cache_start_ns = None

    def emit(self, event, **fields):
        with self.events.open('a', encoding='utf-8') as f:
            f.write(json.dumps({'time_ns': time.time_ns(), 'event': event, **fields}, sort_keys=True)+'\n')
            f.flush()

    def guard(self, collect_cache=False):
        g = self.adapter.guard()
        if not isinstance(g, dict) or not g.get('healthy'):
            if isinstance(g, dict) and g.get('host_protection'):
                raise HostProtection('host protection: '+repr(g))
            raise ControllerError('safety guard unavailable, stale, or stopped: '+repr(g))
        if g.get('cache_failure') or g.get('data_failure'):
            raise ControllerError('cache or correctness evidence failed: '+repr(g))
        if g.get('tool_failure'):
            raise ToolLimit('generator/transport path invalid: '+str(g['tool_failure']))
        gen = g.get('cache_generation')
        if gen:
            observed = g.get('observed_unix_ns')
            if not isinstance(observed, int) or observed <= 0:
                raise ControllerError('cache observation lacks actual timestamp')
            # A previously observed remote sample cannot establish the
            # confirmation baseline, even if read locally after stage start.
            if collect_cache and self.cache_start_ns is not None and observed < self.cache_start_ns:
                return g
        if gen and gen != self.last_cache:
            old = self.last_cache
            self.last_cache = gen
            self.emit('cache_observed', generation=gen, previous=old, observed_unix_ns=observed)
            if collect_cache and old is not None:
                self.cache.append({'generation': gen, 'observed_unix_ns': observed})
        return g

    def next_sample(self, collect_cache=False):
        self.guard(collect_cache)
        s = self.driver.next_sample(timeout=5)
        if not isinstance(s, dict) or not isinstance(s.get('time_unix_ns'), int):
            raise ControllerError('invalid driver sample')
        problem = self.adapter.observe_sample(s)
        if problem:
            raise ToolLimit('generator or transport path invalid: '+str(problem))
        return s

    def check_resources(self, rate, phase):
        result = self.adapter.preflight_stage(rate, phase, self.budget.state)
        self.emit('resource_preflight', phase=phase, rate=rate, result=result)
        if not isinstance(result, dict) or result.get('ok') is not True:
            raise ToolLimit('local or remote resource forecast insufficient: '+repr(result))

    def await_paused_driver(self):
        # Large clean journals may take longer than one sample interval to
        # replay. This is preparation, not active load. The bootstrap pidfd
        # watchdog and remote safety session are already running.
        end = time.monotonic()+600
        while time.monotonic() < end:
            self.guard(False)
            try:
                sample = self.driver.next_sample(timeout=5)
            except SampleTimeout:
                continue
            if sample.get('target_rate') != 0:
                raise ControllerError('driver did not start paused')
            return sample
        raise ControllerError('paused journal replay exceeded preparation timeout')

    def stage(self, name, rate, seconds, confirmation=False):
        self.check_resources(rate, 'confirmation' if confirmation else 'search')
        self.guard(False)
        self.driver.send({'operation': 'stage', 'stage': name, 'rate': rate})
        before = None
        # The driver writes an exact old-stage boundary sample before it
        # applies the command. A missing boundary is not guessed from a timer.
        for _ in range(10):
            # Never count a cache change straddling the stage boundary: its
            # publication may have preceded confirmation even if first read
            # afterwards. Establish a fresh in-confirmation baseline below.
            s = self.next_sample(False)
            if s.get('boundary') == 'before_stage':
                before = s
                break
            if s.get('stage') == name:
                raise ControllerError('stage boundary sample lost')
        if before is None:
            raise ControllerError('stage boundary sample unavailable')
        start = before['time_unix_ns']
        if confirmation:
            self.last_cache = None
            self.cache = []
            self.cache_start_ns = start
        samples = []
        while True:
            s = self.next_sample(confirmation)
            if s.get('stage') != name or s.get('target_rate') != rate:
                raise ControllerError('unexpected stage/target in sample stream')
            if s.get('stage_start_unix_ns') != start:
                raise ControllerError('stage start does not match boundary')
            if s['time_unix_ns'] <= (samples[-1]['time_unix_ns'] if samples else start):
                raise ControllerError('non-monotonic samples')
            samples.append(s)
            if s['time_unix_ns'] - start >= seconds*NS:
                break
        if confirmation:
            cache = [v for v in self.cache if start <= v['observed_unix_ns'] <= samples[-1]['time_unix_ns']]
            req = {'mode': 'confirmation', 'before': before, 'samples': samples, 'rate': rate,
                   'start_unix_ns': start, 'cache': cache, 'paused': False, 'signals': {}}
        else:
            search, _ = self.budget.elapsed()
            # Reserve ten seconds for a clean stop and the 20-minute phase.
            req = {'mode': 'stage', 'before': before, 'samples': samples, 'rate': rate,
                   'plan': self.budget.state['plan'], 'search_elapsed_ns': search+10*NS,
                   'signals': {}}
        result = self.assess(req)
        self.emit('assessment', phase='confirmation' if confirmation else 'search', result=result)
        return result

    def run(self):
        error = None
        final_result = None
        try:
            preflight = self.adapter.preflight(self.budget.state)
            if not isinstance(preflight, dict) or preflight.get('verified') is not True:
                raise ControllerError('task preflight did not prove its safety prerequisites')
            self.budget.state['preflight'] = preflight
            self.budget.save()
            if self.budget.state['status'] == 'new':
                self.budget.state['plan'] = {'current': 100, 'lower': 0, 'upper': 0,
                                             'uncertain_repeats': 0, 'done': False}
                self.budget.state['stage_index'] = 0
                self.adapter.prepare_phase('search')
                self.budget.mark_loading('search')
                self.driver = self.adapter.start_driver('search', self.budget.clock()+7200*NS, resume=False)
                self.await_paused_driver()
                deadline = self.budget.begin('search')
                self.driver.arm(deadline)  # before the first arrival command
                while True:
                    p = self.budget.state['plan']
                    name = 'search-%03d' % self.budget.state['stage_index']
                    result = self.stage(name, p['current'], 180)
                    self.budget.state['plan'] = result['plan']
                    self.budget.state['stage_index'] += 1
                    self.budget.save()
                    step = result['step']
                    if step['action'] == 'stage':
                        if self.budget.state['active']['deadline_ns'] - self.budget.clock() < 181*NS:
                            raise BudgetError('insufficient search time for another complete stage')
                        continue
                    stopped = self.driver.stop()
                    self.driver.close()
                    self.driver = None
                    if step['action'] == 'stop':
                        self.budget.end_clean(True, 'stopped')
                        final_result = {'verdict': 'search_stopped', 'reason': step['reason']}
                        evidence = self.adapter.finalize(final_result, stopped)
                        if not isinstance(evidence, dict) or evidence.get('verified') is not True:
                            raise ControllerError('final stopped database and ledger were not verified')
                        self.budget.state['final_evidence'] = evidence
                        self.budget.save()
                        return final_result
                    if step['action'] != 'transition_to_confirmation' or step.get('rate', 0) <= 0:
                        raise ControllerError('unrecognized search step')
                    self.budget.state['confirm_rate'] = step['rate']
                    self.budget.end_clean(True, 'transition_stopped')
                    transition_evidence = self.adapter.transition(stopped)
                    self.budget.transition_ready(transition_evidence)
                    self.emit('planned_transition', evidence=transition_evidence)
                    if self.handoff_after_transition:
                        return {'verdict': 'transition_ready', 'reason': 'clean planned handoff; confirmation not started'}
                    break
            if self.budget.state['status'] != 'transition_ready':
                raise ControllerError('no clean transition to confirmation')
            rate = self.budget.state['confirm_rate']
            self.check_resources(rate, 'confirmation')
            self.adapter.prepare_phase('confirmation')
            self.budget.mark_loading('confirmation')
            self.driver = self.adapter.start_driver('confirmation', self.budget.clock()+7200*NS, resume=True)
            self.await_paused_driver()  # restore before spending continuous budget
            deadline = self.budget.begin('confirmation')
            self.driver.arm(deadline)  # independent deadline before activation
            self.cache = []
            result = self.stage('confirmation', rate, 1200, confirmation=True)
            final_result = result['confirmation']
            stopped = self.driver.stop()
            self.driver.close()
            self.driver = None
            self.budget.end_clean(True, 'confirmation_stopped')
            evidence = self.adapter.finalize(final_result, stopped)
            self.budget.confirmation_result(final_result, evidence)
            return final_result
        except BaseException as exc:
            error = exc
            try:
                if self.driver:
                    self.driver.terminate()
            finally:
                try:
                    self.budget.fail(repr(exc), host_protection=bool(getattr(exc, 'host_protection', False)))
                except BaseException:
                    pass
                try:
                    self.adapter.abort(repr(exc))
                except BaseException as abort_exc:
                    try:
                        self.emit('abort_evidence_failed', error=repr(abort_exc))
                    except BaseException:
                        pass
            raise
        finally:
            cleanup_error = None
            if self.driver:
                try:
                    self.driver.close()
                except BaseException as exc:
                    cleanup_error = exc
                self.driver = None
            try:
                self.adapter.cleanup()
            except BaseException as exc:
                cleanup_error = cleanup_error or exc
            if cleanup_error is not None:
                try:
                    self.budget.fail('cleanup failed: '+repr(cleanup_error))
                except BaseException:
                    pass
                if error is None:
                    raise cleanup_error


def validate_config(config):
    from sustained_remote_adapter import sha256_file
    from sustained_remote_spec import RemoteSpec, validate_remote_assets

    required = {'host', 'task_id', 'local_task_dir', 'remote_root', 'service', 'watch',
                'application_remote', 'catalog_remote', 'web_remote', 'capacity_binary',
                'catalog_local', 'capacity_sha256', 'catalog_sha256', 'application_sha256',
                'baseline_containers_sha256'}
    optional = {'max_pending', 'max_in_flight', 'max_journal_bytes'}
    if not isinstance(config, dict) or set(config)-required-optional or required-set(config):
        raise ValueError('config contains missing or unknown fields')
    token = config['task_id']
    if (config['host'] != 'lax-racknerd' or not isinstance(token, str) or
            not re.fullmatch(r'[0-9a-f]{16,32}', token) or
            config['service'] != 'ranking-c54-'+token+'.service' or
            config['watch'] != 'ranking-c54-'+token+'-watch.service' or
            Path(config['remote_root']).name != 'task-'+token):
        raise ValueError('host or task-scoped service identity is invalid')
    allowed = Path('/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/sustained-20261113-r1')
    local = Path(config['local_task_dir'])
    if (not local.is_absolute() or not local.is_dir() or local.is_symlink() or
            local == allowed or not local.resolve().is_relative_to(allowed) or
            local.stat().st_uid != os.getuid() or local.stat().st_mode & 0o077):
        raise ValueError('local task directory must be a private C54 task subdirectory')
    validate_remote_assets(config['application_remote'], config['catalog_remote'], config['web_remote'])
    spec = RemoteSpec(config['remote_root'], config['service'], config['watch'],
                      config['application_remote'], config['catalog_remote'], config['web_remote'])
    remote = Path(spec.root)
    if remote == allowed or not remote.is_relative_to(allowed):
        raise ValueError('remote task root is outside the authorized C54 root')
    for name in ('capacity_sha256', 'catalog_sha256', 'application_sha256', 'baseline_containers_sha256'):
        if not isinstance(config[name], str) or not re.fullmatch(r'[0-9a-f]{64}', config[name]):
            raise ValueError('invalid SHA-256: '+name)
    for path, hash_name in ((config['capacity_binary'], 'capacity_sha256'), (config['catalog_local'], 'catalog_sha256')):
        if not Path(path).is_absolute() or not Path(path).is_file() or sha256_file(path) != config[hash_name]:
            raise ValueError('local file or hash mismatch: '+path)
    for key, default, maximum in (('max_pending', 200000, 200000), ('max_in_flight', 4096, 4096),
                                  ('max_journal_bytes', 4 << 30, 4 << 30)):
        value = config.get(key, default)
        minimum = 1 << 20 if key == 'max_journal_bytes' else 1
        if type(value) is not int or not minimum <= value <= maximum:
            raise ValueError('invalid local safety bound: '+key)
    return spec


def result_exit_code(result, plan):
    # A confirmed lower candidate without an observed failure is useful but
    # cannot be reported as a completed bounded capacity search.
    return 0 if result.get('verdict') == 'confirmed' and plan.get('upper', 0) > 0 else 2


def main(argv=None):
    from sustained_state import Budget
    from sustained_remote_spec import RATES
    from sustained_remote_adapter import runtime_hashes

    parser = argparse.ArgumentParser(description='C54 sustained controller; dry-run unless explicitly enabled')
    parser.add_argument('--config', required=True, help='private fixed JSON config')
    parser.add_argument('--execute-remote', action='store_true', help='run the authorized remote test')
    parser.add_argument('--handoff-after-transition', action='store_true', help='stop at a clean planned transition')
    args = parser.parse_args(argv)
    config = json.loads(Path(args.config).read_text())
    spec = validate_config(config)
    encoded = json.dumps(config, sort_keys=True, separators=(',', ':')).encode()
    config_hash = hashlib.sha256(encoded).hexdigest()
    if not args.execute_remote:
        print(json.dumps({'mode': 'dry_run_no_remote_no_load', 'config_sha256': config_hash,
                          'host': config['host'], 'task_id': config['task_id'],
                          'service': spec.service, 'watch': spec.watcher,
                          'runtime_hashes': runtime_hashes(),
                          'capacity_sha256': config['capacity_sha256'], 'catalog_sha256': config['catalog_sha256'],
                          'application_sha256': config['application_sha256'],
                          'baseline_containers_sha256': config['baseline_containers_sha256'],
                          'initial_rate': 100, 'stage_seconds': 180, 'search_limit_seconds': 2400,
                          'total_limit_seconds': 3600, 'single_confirmation_seconds': 1200,
                          'forward': '127.0.0.1:14210 -> SSH -> 127.0.0.1:4210',
                          'test_rate_environment': dict(RATES),
                          'warning': 'no service, watch, SSH tunnel, or load was started'}, sort_keys=True))
        return 0
    from sustained_remote_adapter import RemoteAdapter
    local = Path(config['local_task_dir'])
    # One anchor for the entire authorized C54 task, not one per run folder or
    # task ID. A different config cannot silently reset spent time or protection.
    anchor = Path('/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54/sustained-20261113-r1/formal-budget-state.json')
    try:
        budget = Budget(anchor, config_hash)
    except BaseException as exc:
        print('C54 budget unavailable; no load started: '+repr(exc), file=sys.stderr, flush=True)
        return 130 if isinstance(exc, KeyboardInterrupt) else 1
    try:
        adapter = RemoteAdapter(config)
        result = Controller(budget, adapter, GoAssessment(config['capacity_binary'], adapter.run_dir/'assessment-commands.jsonl'),
                            adapter.run_dir/'controller-events.jsonl',
                            handoff_after_transition=args.handoff_after_transition).run()
        plan = budget.state.get('plan', {})
        print(json.dumps({'result': result, 'state': budget.state['status'], 'budget_path': str(budget.path),
                          'search_plan': plan, 'no_observed_upper_bound': plan.get('upper', 0) == 0,
                          'search_active_seconds': budget.state['search_ns']/NS,
                          'confirmation_active_seconds': budget.state['confirm_ns']/NS}, sort_keys=True))
        return result_exit_code(result, plan)
    except BaseException as exc:
        if not budget.state.get('status', '').startswith('failed'):
            try:
                budget.fail(repr(exc), host_protection=bool(getattr(exc, 'host_protection', False)))
            except BaseException:
                pass
        print('C54 controller failed closed: '+repr(exc), file=sys.stderr, flush=True)
        return 130 if isinstance(exc, KeyboardInterrupt) else 1
    finally:
        budget.close()


if __name__ == '__main__':
    def interrupted(signum, _frame):
        for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            signal.signal(sig, signal.SIG_IGN)
        raise KeyboardInterrupt('controller signal '+str(signum))
    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, interrupted)
    sys.exit(main())
