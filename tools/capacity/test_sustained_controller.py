"""Virtual-clock control-flow tests using real Go classification; NOT performance data."""
import copy
import os
import tempfile
import unittest
from pathlib import Path

from sustained_controller import Controller, ControllerError, GoAssessment
from sustained_state import Budget, NS


class Clock:
    def __init__(self):
        self.ns = NS

    def __call__(self):
        return self.ns

    def tick(self):
        self.ns += NS

    def unix(self):
        return 1_700_000_000*NS+self.ns


class FakeDriver:
    def __init__(self, adapter):
        self.a = adapter
        self.s = adapter.sample
        self.boundary = None
        self.pending_debt = 0
        self.n = 0
        self.closed = False
        self.terminated = False
        self.samples_seen = 0

    def snapshot(self):
        s = copy.deepcopy(self.s)
        s['time_unix_ns'] = self.a.clock.unix()
        s['pending'] = s['produced']-s['confirmed']
        return s

    def arm(self, deadline):
        self.a.active_deadlines.append(deadline)

    def send(self, command):
        if command['operation'] != 'stage':
            raise AssertionError(command)
        self.boundary = self.snapshot()
        self.boundary['boundary'] = 'before_stage'
        self.s['stage'] = command['stage']
        self.s['target_rate'] = command['rate']
        self.s['stage_start_unix_ns'] = self.boundary['time_unix_ns']
        self.pending_debt = 5*command['rate']
        self.n = 0
        self.a.commands.append(copy.deepcopy(command))

    def next_sample(self, timeout=5):
        self.samples_seen += 1
        if self.a.timeout_after and self.samples_seen > self.a.timeout_after:
            raise ControllerError('simulated sample timeout')
        if self.boundary:
            value, self.boundary = self.boundary, None
            return value
        if not self.s['target_rate']:
            return self.snapshot()
        self.a.clock.tick()
        self.n += 1
        rate = self.s['target_rate']
        old_pending = self.s['produced']-self.s['confirmed']
        self.s['arrival_produced'] += rate
        self.s['produced'] += rate+self.pending_debt
        self.s['debt_produced'] += self.pending_debt
        pending = old_pending+self.pending_debt
        self.pending_debt = 0
        mode = self.a.mode(self.s['stage'], rate)
        if mode == 'failed':
            pending += rate//2
            self.s['oldest_pending_ms'] += 1000
        elif mode == 'uncertain':
            self.s['oldest_pending_ms'] = 1000
        else:
            pending = max(0, pending-max(1, rate//4))
            self.s['oldest_pending_ms'] = max(0, self.s['oldest_pending_ms']-1000)
        self.s['confirmed'] = self.s['produced']-pending
        self.s['active_seconds'] += 1
        return self.snapshot()

    def stop(self):
        self.s['target_rate'] = 0
        self.a.stops += 1
        return {'reason': 'requested_stop', 'sample': self.snapshot()}

    def terminate(self):
        self.terminated = True
        self.a.terminated = True
        self.s['target_rate'] = 0

    def close(self):
        self.closed = True


class FakeAdapter:
    def __init__(self, clock, mode=None, insufficient_cache=False, fail_after=None):
        self.clock = clock
        self.mode = mode or (lambda stage, rate: 'failed' if stage != 'confirmation' and rate >= 325 else 'candidate')
        self.insufficient_cache, self.fail_after = insufficient_cache, fail_after
        self.calls = self.stops = self.transitions = self.finalizations = 0
        self.timeout_after = None
        self.cleaned = self.terminated = self.aborted = False
        self.commands = []
        self.active_deadlines = []
        self.sample = {'stage': '', 'target_rate': 0, 'produced': 0, 'arrival_produced': 0,
                       'debt_produced': 0, 'confirmed': 0, 'pending': 0, 'debt_remaining': 0,
                       'in_flight': 0, 'written_unfinished': 0,
                       'oldest_pending_ms': 0, 'active_seconds': 0}
        self.confirm_clock = None

    def preflight(self, state):
        self.preflighted = True
        return {'verified': True, 'kind': 'mock-only'}

    def prepare_phase(self, phase):
        pass

    def preflight_stage(self, rate, phase, state):
        return {'ok': True, 'kind': 'mock-only'}

    def observe_sample(self, sample):
        return None

    def start_driver(self, phase, deadline, resume):
        if phase == 'confirmation':
            if not resume:
                raise AssertionError('confirmation dropped prior journal')
            self.confirm_clock = self.clock.ns
        return FakeDriver(self)

    def guard(self):
        self.calls += 1
        if self.fail_after and self.calls > self.fail_after:
            return {'healthy': False, 'reason': 'simulated_stale_watch'}
        generation = 0
        if self.confirm_clock:
            generation = (self.clock.ns-self.confirm_clock)//(300*NS)
            if self.insufficient_cache:
                generation = min(generation, 2)
        return {'healthy': True, 'cache_generation': str(generation),
                'observed_unix_ns': self.clock.unix()}

    def transition(self, stopped):
        self.transitions += 1
        return {'verified': True, 'same_database': 'mock-only',
                'retained_pending': self.sample['produced']-self.sample['confirmed']}

    def finalize(self, result, stopped):
        self.finalizations += 1
        return {'verified': True, 'kind': 'mock-only-not-remote-performance'}

    def abort(self, reason):
        self.aborted = True

    def cleanup(self):
        self.cleaned = True


@unittest.skipUnless(os.environ.get('CAPACITY_TEST_BIN'), 'requires built Go assessment bridge')
class ControllerTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.path = Path(self.temp.name)
        self.clock = Clock()
        self.budget = Budget(self.path/'budget.json', 'test-config', self.clock, lambda: 'test-boot')
        self.assess = GoAssessment(os.environ['CAPACITY_TEST_BIN'], self.path/'go-commands.jsonl')

    def tearDown(self):
        self.budget.close()
        self.temp.cleanup()

    def run_controller(self, adapter):
        return Controller(self.budget, adapter, self.assess, self.path/'events.jsonl').run()

    def test_real_decision_bridge_drives_increase_overload_bisect_confirm(self):
        adapter = FakeAdapter(self.clock)
        result = self.run_controller(adapter)
        self.assertEqual([x['rate'] for x in adapter.commands], [100, 200, 400, 300, 350, 325, 300])
        self.assertEqual(result['verdict'], 'confirmed')
        self.assertEqual((adapter.transitions, adapter.stops, adapter.finalizations), (1, 2, 1))
        self.assertTrue(adapter.cleaned)
        self.assertEqual(len(adapter.active_deadlines), 2)
        self.assertEqual(self.budget.state['status'], 'completed')
        self.assertGreater(self.budget.state['transition_evidence']['retained_pending'], 0)
        self.assertGreaterEqual(self.budget.state['confirm_ns'], 1200*NS)
        self.assertLessEqual(sum(self.budget.elapsed()), 3600*NS)

    def test_reviewed_resume_replays_original_debt_without_resetting_search_budget(self):
        adapter = FakeAdapter(self.clock, mode=lambda stage, rate: 'failed' if stage != 'confirmation' and rate > 400 else 'candidate')
        adapter.sample.update({'produced': 188920, 'confirmed': 163994, 'pending': 24926,
                               'arrival_produced': 181420, 'debt_produced': 7500,
                               'oldest_pending_ms': 1000000, 'active_seconds': 611})
        self.budget.state.update({'status': 'reviewed_search_ready', 'search_ns': 612018194623,
                                  'reviewed_checkpoint': {'verified': True},
                                  'plan': {'current': 600, 'lower': 400, 'upper': 0,
                                           'uncertain_repeats': 0, 'done': False}, 'stage_index': 4})
        result = self.run_controller(adapter)
        self.assertEqual(adapter.commands[0]['rate'], 600)
        self.assertNotIn(100, [c['rate'] for c in adapter.commands])
        self.assertEqual(result['verdict'], 'confirmed')
        self.assertGreater(self.budget.state['search_ns'], 612018194623)
        self.assertTrue(self.budget.state['confirm_started'])
        self.assertEqual(sum(c['stage'] == 'confirmation' for c in adapter.commands), 1)
        self.assertIn('reviewed_replay', (self.path/'events.jsonl').read_text())

    def test_retry_reviewed_checkpoint_restarts_500_with_new_stage_then_confirms(self):
        adapter = FakeAdapter(self.clock, mode=lambda stage, rate: 'failed' if stage != 'confirmation' and rate >= 550 else 'candidate')
        adapter.sample.update({'produced': 363619, 'confirmed': 334393, 'pending': 29226,
                               'arrival_produced': 350619, 'debt_produced': 13000,
                               'oldest_pending_ms': 1000000, 'active_seconds': 913})
        self.budget.state.update({'status': 'reviewed_search_ready', 'search_ns': 914302803460,
                                  'reviewed_checkpoint': {'replay_expected': [363619, 334393, 29226, 0],
                                                          'retry_manifest_sha256': 'approved-mock'},
                                  'plan': {'current': 500, 'lower': 400, 'upper': 600,
                                           'uncertain_repeats': 0, 'done': False}, 'stage_index': 6})
        result = self.run_controller(adapter)
        self.assertEqual([(c['stage'], c['rate']) for c in adapter.commands],
                         [('search-006', 500), ('search-007', 550), ('confirmation', 500)])
        self.assertEqual(result['verdict'], 'confirmed')
        self.assertGreater(self.budget.state['search_ns'], 914302803460)
        self.assertEqual(sum(c['stage'] == 'confirmation' for c in adapter.commands), 1)

    def test_retry_reviewed_search_does_not_probe_below_500(self):
        adapter = FakeAdapter(self.clock, mode=lambda stage, rate: 'failed' if stage != 'confirmation' and rate >= 500 else 'candidate')
        adapter.sample.update({'produced': 363619, 'confirmed': 334393, 'pending': 29226,
                               'arrival_produced': 350619, 'debt_produced': 13000,
                               'oldest_pending_ms': 1000000, 'active_seconds': 913})
        self.budget.state.update({'status': 'reviewed_search_ready', 'search_ns': 914302803460,
                                  'reviewed_checkpoint': {'replay_expected': [363619, 334393, 29226, 0],
                                                          'retry_manifest_sha256': 'approved-mock'},
                                  'plan': {'current': 500, 'lower': 400, 'upper': 600,
                                           'uncertain_repeats': 0, 'done': False}, 'stage_index': 6})
        result = self.run_controller(adapter)
        self.assertEqual([(c['stage'], c['rate']) for c in adapter.commands],
                         [('search-006', 500), ('confirmation', 400)])
        self.assertEqual(result['verdict'], 'confirmed')
        self.assertEqual(self.budget.state['plan']['upper'], 500)
        self.assertIn('reviewed_range_confirmation', (self.path/'events.jsonl').read_text())

    def test_next_stage_forecast_limit_preserves_confirmation_without_upper(self):
        adapter = FakeAdapter(self.clock, mode=lambda stage, rate: 'candidate')
        adapter.preflight_stage = lambda rate, phase, state: (
            {'ok': False, 'issues': ['journal_budget_insufficient']} if phase == 'search' and rate == 200
            else {'ok': True})
        result = self.run_controller(adapter)
        self.assertEqual([c['rate'] for c in adapter.commands], [100, 100])
        self.assertEqual(result['verdict'], 'confirmed')
        self.assertEqual(self.budget.state['plan']['upper'], 0)
        self.assertIn('forecast_confirmation_fallback', (self.path/'events.jsonl').read_text())

    def test_planned_handoff_reopens_same_budget_and_preserves_debt(self):
        adapter = FakeAdapter(self.clock)
        controller = Controller(self.budget, adapter, self.assess, self.path/'events.jsonl', handoff_after_transition=True)
        result = controller.run()
        self.assertEqual(result['verdict'], 'transition_ready')
        search_spent = self.budget.state['search_ns']
        pending = adapter.sample['produced']-adapter.sample['confirmed']
        self.assertGreater(pending, 0)
        self.assertFalse(self.budget.state['confirm_started'])
        self.budget.close()
        self.budget = Budget(self.path/'budget.json', 'test-config', self.clock, lambda: 'test-boot')
        self.assertEqual(self.budget.state['search_ns'], search_spent)
        self.assertEqual(adapter.sample['produced']-adapter.sample['confirmed'], pending)
        result = self.run_controller(adapter)
        self.assertEqual(result['verdict'], 'confirmed')
        self.assertEqual(self.budget.state['search_ns'], search_spent)
        self.assertEqual(sum(x['stage'] == 'confirmation' for x in adapter.commands), 1)

    def test_uncertainty_repeats_once_then_confirms_candidate(self):
        adapter = FakeAdapter(self.clock, mode=lambda stage, rate: 'uncertain' if stage != 'confirmation' and rate == 200 else 'candidate')
        result = self.run_controller(adapter)
        self.assertEqual([x['rate'] for x in adapter.commands], [100, 200, 200, 100])
        self.assertEqual(result['verdict'], 'confirmed')
        self.assertEqual(self.budget.state['plan']['reason'], 'unresolved uncertain stage')

    def test_preconfirmation_cache_observation_cannot_be_confirmation_baseline(self):
        adapter = FakeAdapter(self.clock)
        controller = Controller(self.budget, adapter, self.assess, self.path/'events.jsonl')
        controller.cache_start_ns = 1000
        adapter.guard = lambda: {'healthy': True, 'cache_generation': 'old', 'observed_unix_ns': 999}
        controller.guard(True)
        self.assertIsNone(controller.last_cache)
        adapter.guard = lambda: {'healthy': True, 'cache_generation': 'first', 'observed_unix_ns': 1001}
        controller.guard(True)
        self.assertEqual(controller.last_cache, 'first')
        self.assertEqual(controller.cache, [])
        adapter.guard = lambda: {'healthy': True, 'cache_generation': 'second', 'observed_unix_ns': 1002}
        controller.guard(True)
        self.assertEqual(controller.cache, [{'generation': 'second', 'observed_unix_ns': 1002}])

    def test_missing_cache_publications_does_not_confirm_or_repeat(self):
        adapter = FakeAdapter(self.clock, insufficient_cache=True)
        result = self.run_controller(adapter)
        self.assertEqual(result['verdict'], 'unconfirmed')
        self.assertEqual(self.budget.state['status'], 'stopped_unconfirmed')
        self.assertEqual(sum(x['stage'] == 'confirmation' for x in adapter.commands), 1)
        self.assertTrue(adapter.cleaned)

    def test_stale_guard_stops_driver_and_preserves_unknown_active_state(self):
        adapter = FakeAdapter(self.clock, fail_after=12)
        with self.assertRaises(ControllerError):
            self.run_controller(adapter)
        self.assertTrue(adapter.terminated)
        self.assertTrue(adapter.aborted)
        self.assertTrue(adapter.cleaned)
        self.assertIsNotNone(self.budget.state['active'])
        self.assertEqual(self.budget.state['status'], 'failed')

    def test_stopped_sample_exposes_driver_failure_instead_of_stage_mismatch(self):
        adapter = FakeAdapter(self.clock)
        controller = Controller(self.budget, adapter, self.assess, self.path/'events.jsonl')
        driver = FakeDriver(adapter)
        original = driver.next_sample
        def stopped_sample(timeout=5):
            sample = original(timeout)
            if sample.get('boundary') != 'before_stage':
                sample['target_rate'] = 0
            return sample
        driver.next_sample = stopped_sample
        controller.driver = driver
        with self.assertRaisesRegex(ControllerError, 'driver stopped during active stage'):
            controller.stage('search-000', 100, 180)

    def test_sample_timeout_and_cancel_stop_and_cleanup(self):
        for failure in ('timeout', 'cancel'):
            with self.subTest(failure=failure):
                if failure == 'cancel':
                    # A new independent state is needed after fail-closed stop.
                    self.budget.close()
                    self.budget = Budget(self.path/'cancel-budget.json', 'test-config', self.clock, lambda: 'test-boot')
                adapter = FakeAdapter(self.clock)
                if failure == 'timeout':
                    adapter.timeout_after = 3
                else:
                    adapter.guard = lambda: (_ for _ in ()).throw(KeyboardInterrupt('operator cancel'))
                with self.assertRaises((ControllerError, KeyboardInterrupt)):
                    self.run_controller(adapter)
                self.assertTrue(adapter.aborted)
                self.assertTrue(adapter.cleaned)
                self.assertEqual(self.budget.state['status'], 'failed')

    def test_resource_forecast_failure_stops_before_any_stage(self):
        adapter = FakeAdapter(self.clock)
        adapter.preflight_stage = lambda rate, phase, state: {'ok': False, 'issues': ['local_disk_insufficient']}
        with self.assertRaisesRegex(ControllerError, 'resource forecast insufficient'):
            self.run_controller(adapter)
        self.assertEqual(adapter.commands, [])
        self.assertTrue(adapter.aborted)
        self.assertTrue(adapter.cleaned)
        self.assertEqual(self.budget.state['status'], 'failed')

    def test_transition_evidence_failure_never_arms_confirmation(self):
        adapter = FakeAdapter(self.clock)
        def fail_transition(_):
            raise OSError('simulated evidence write failure')
        adapter.transition = fail_transition
        with self.assertRaises(OSError):
            self.run_controller(adapter)
        self.assertTrue(adapter.aborted)
        self.assertTrue(adapter.cleaned)
        self.assertFalse(self.budget.state['confirm_started'])
        self.assertEqual(self.budget.state['status'], 'failed')

    def test_failed_confirmation_never_repeats(self):
        adapter = FakeAdapter(self.clock, mode=lambda stage, rate: 'failed' if stage == 'confirmation' or rate >= 325 else 'candidate')
        result = self.run_controller(adapter)
        self.assertEqual(result['verdict'], 'failed')
        self.assertEqual(sum(x['stage'] == 'confirmation' for x in adapter.commands), 1)
        self.assertEqual(self.budget.state['status'], 'stopped_unconfirmed')
        self.assertTrue(adapter.cleaned)

    def test_host_protection_is_recorded_and_cleans_up(self):
        adapter = FakeAdapter(self.clock)
        adapter.guard = lambda: {'healthy': False, 'host_protection': True}
        with self.assertRaises(ControllerError):
            self.run_controller(adapter)
        self.assertEqual(self.budget.state['host_protections'], 1)
        self.assertTrue(adapter.aborted)
        self.assertTrue(adapter.cleaned)

    def test_cleanup_failure_invalidates_even_a_confirmed_assessment(self):
        adapter = FakeAdapter(self.clock)
        def cleanup_failure():
            raise OSError('cleanup verification failed')
        adapter.cleanup = cleanup_failure
        with self.assertRaisesRegex(OSError, 'cleanup verification failed'):
            self.run_controller(adapter)
        self.assertEqual(self.budget.state['status'], 'failed')
        self.assertEqual(sum(x['stage'] == 'confirmation' for x in adapter.commands), 1)

    def test_no_upper_bound_is_not_reported_as_a_measured_failure(self):
        adapter = FakeAdapter(self.clock, mode=lambda stage, rate: 'candidate')
        result = self.run_controller(adapter)
        self.assertEqual(result['verdict'], 'search_stopped')
        self.assertEqual(self.budget.state['plan']['upper'], 0)
        self.assertIn('without an upper bound', result['reason'])
        self.assertTrue(adapter.cleaned)


if __name__ == '__main__':
    unittest.main()
