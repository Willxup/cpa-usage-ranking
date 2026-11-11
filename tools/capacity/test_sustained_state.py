import json
import os
import tempfile
import unittest
from pathlib import Path

from sustained_state import Budget, BudgetError, NS, SEARCH_LIMIT, TOTAL_LIMIT, CONFIRM_RESERVE


class FakeClock:
    def __init__(self):
        self.ns = 1

    def __call__(self):
        return self.ns

    def advance(self, seconds):
        self.ns += seconds*NS


class BudgetTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.path = Path(self.tmp.name)/'state.json'
        self.clock = FakeClock()
        self.budget = None

    def tearDown(self):
        if self.budget:
            self.budget.close()
        self.tmp.cleanup()

    def open(self, config='fixture-catalog-binary-hash'):
        self.budget = Budget(self.path, config, self.clock, lambda: 'boot-a')
        return self.budget

    def test_budget_persists_across_clean_process_transition(self):
        b = self.open()
        deadline = b.begin('search')
        self.assertEqual(deadline-self.clock(), SEARCH_LIMIT-10*NS)
        self.clock.advance(900)
        b.end_clean(True, 'transition_stopped')
        b.transition_ready({'verified': True, 'same_database': 'digest'})
        b.close()
        b = self.open()
        self.assertEqual(b.elapsed(), (900*NS, 0))
        deadline = b.begin('confirmation')
        self.assertEqual(deadline-self.clock(), CONFIRM_RESERVE)
        self.clock.advance(1201)
        b.end_clean(True, 'confirmation_stopped')
        b.confirmation_result({'verdict': 'confirmed'}, {'verified': True})
        self.assertLess(sum(b.elapsed()), TOTAL_LIMIT)
        b.close()
        self.budget = None
        with self.assertRaises(BudgetError):
            self.open()  # no second confirmation, nor another search

    def test_no_restart_after_unknown_active_interval(self):
        b = self.open()
        b.begin('search')
        self.clock.advance(10)
        b.close()
        self.budget = None
        with self.assertRaisesRegex(BudgetError, 'unclean prior process'):
            self.open()
        state = json.loads(self.path.read_text())
        self.assertEqual(state['status'], 'failed_unknown_active_interval')
        self.assertIsNotNone(state['active'])
        with self.assertRaises(BudgetError):
            self.open()

    def test_paused_replay_crash_does_not_auto_resume(self):
        b = self.open()
        b.begin('search')
        self.clock.advance(181)
        b.end_clean(True, 'transition_stopped')
        b.transition_ready({'verified': True, 'same_database': 'digest'})
        b.mark_loading('confirmation')
        self.assertFalse(b.state['confirm_started'])
        b.close()
        self.budget = None
        with self.assertRaisesRegex(BudgetError, 'not a clean planned transition'):
            self.open()

    def test_single_confirmation_and_incomplete_journal_fail_closed(self):
        b = self.open()
        b.begin('search')
        self.clock.advance(181)
        with self.assertRaises(BudgetError):
            b.end_clean(False, 'transition_stopped')
        self.assertIsNotNone(b.state['active'])
        b.end_clean(True, 'transition_stopped')
        b.transition_ready({'verified': True, 'same_database': 'digest'})
        b.begin('confirmation')
        self.clock.advance(5)
        with self.assertRaises(BudgetError):
            b.end_clean(True, 'transition_stopped')
        b.end_clean(True, 'stopped')
        with self.assertRaises(BudgetError):
            b.begin('confirmation')

    def test_search_budget_exceeded(self):
        b = self.open()
        b.begin('search')
        self.clock.advance(SEARCH_LIMIT//NS+1)
        with self.assertRaisesRegex(BudgetError, 'budget exceeded'):
            b.end_clean(True, 'transition_stopped')
        self.assertEqual(b.state['status'], 'failed_budget_exceeded')
        b.close()
        self.budget = None
        with self.assertRaises(BudgetError):
            self.open()

    def test_inconsistent_budget_fails_closed(self):
        b = self.open()
        b.state['confirm_ns'] = NS
        with self.assertRaisesRegex(BudgetError, 'internally inconsistent'):
            b.validate()
        with self.assertRaises(BudgetError):
            b.begin('search')

    def test_two_host_protections_prohibit_new_load(self):
        b = self.open()
        b.state['host_protections'] = 2  # Simulate independently recorded prior stops.
        b.save()
        with self.assertRaisesRegex(BudgetError, 'two host protections'):
            b.begin('search')
        self.assertIsNone(b.state['active'])

    def test_config_mismatch_and_lock(self):
        b = self.open()
        with self.assertRaises((OSError, BlockingIOError)):
            Budget(self.path, 'fixture-catalog-binary-hash', self.clock, lambda:'boot-a')
        b.begin('search')
        self.clock.advance(181)
        b.end_clean(True, 'transition_stopped')
        b.transition_ready({'verified': True, 'same_database': 'digest'})
        b.close()
        self.budget = None
        with self.assertRaisesRegex(BudgetError, 'config mismatch'):
            self.open('different-config')


if __name__ == '__main__':
    unittest.main()
