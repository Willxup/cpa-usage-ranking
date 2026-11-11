import os
import subprocess
import sys
import unittest
from sustained_deadline import proc_info
from sustained_process import LocalDriver, ProcessError
from sustained_resource import forecast, GIB, MIB, SustainedCPUCheck


class ResourceTest(unittest.TestCase):
    def args(self):
        return dict(journal_bytes=0, max_journal_bytes=4*GIB, local_free=13*GIB,
                    remote_free=14*GIB, local_mem_available=3*GIB, pending_limit=200000,
                    seed_bytes=90*MIB)

    def test_low_rate_fits_but_larger_unsafe_projection_stops(self):
        self.assertTrue(forecast(100, 'search', **self.args())['ok'])
        a = forecast(1600, 'search', **self.args())
        self.assertFalse(a['ok'])
        self.assertIn('journal_budget_insufficient', a['issues'])

    def test_remote_reserve_and_local_memory_are_not_assumed(self):
        args = self.args()
        args.update(remote_free=12*GIB, local_mem_available=1*GIB)
        result = forecast(100, 'confirmation', **args)
        self.assertIn('remote_disk_insufficient', result['issues'])
        self.assertIn('local_memory_insufficient', result['issues'])

    def test_running_driver_memory_is_not_reserved_twice(self):
        args = self.args()
        args.update(pending_limit=150000, local_mem_available=2200*MIB)
        before = forecast(200, 'search', **args)
        self.assertTrue(before['ok'])
        args.update(local_mem_available=1700*MIB, local_owned_anon=500*MIB)
        running = forecast(200, 'search', **args)
        self.assertTrue(running['ok'])
        self.assertEqual(before['estimates']['memory_need_bytes'], running['estimates']['memory_need_bytes'])
        self.assertEqual(running['estimates']['memory_additional_bytes'],
                         before['estimates']['memory_need_bytes']-500*MIB)
        args['local_owned_anon'] = 0
        self.assertIn('local_memory_insufficient', forecast(200, 'search', **args)['issues'])

    def test_owned_memory_never_credits_host_reserve(self):
        args = self.args()
        args.update(local_owned_anon=4*GIB, local_mem_available=511*MIB)
        result = forecast(100, 'confirmation', **args)
        self.assertEqual(result['estimates']['memory_additional_bytes'], 512*MIB)
        self.assertIn('local_memory_insufficient', result['issues'])
        args['local_owned_anon'] = -1
        with self.assertRaises(ValueError):
            forecast(100, 'confirmation', **args)

    def test_only_live_owned_driver_anonymous_memory_is_credited(self):
        proc = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(10)'],
                                start_new_session=True)
        fd = os.pidfd_open(proc.pid)
        driver = LocalDriver.__new__(LocalDriver)
        driver.proc, driver.pidfd = proc, fd
        driver.start_ticks = proc_info(proc.pid)['start_ticks']
        try:
            self.assertGreaterEqual(driver.resident_anon_bytes(), 0)
            driver.start_ticks += 1
            with self.assertRaisesRegex(ProcessError, 'identity changed'):
                driver.resident_anon_bytes()
            proc.terminate()
            proc.wait(timeout=5)
            with self.assertRaisesRegex(ProcessError, 'exited'):
                driver.resident_anon_bytes()
        finally:
            if proc.poll() is None:
                proc.kill()
                proc.wait(timeout=5)
            os.close(fd)

    def test_existing_journal_bytes_survive_stage_change(self):
        args = self.args()
        args['journal_bytes'] = 3*GIB
        result = forecast(800, 'confirmation', **args)
        self.assertIn('journal_budget_insufficient', result['issues'])
        self.assertGreater(result['estimates']['tasks'], 800*1200)

    def test_sustained_cpu_check_requires_a_full_window(self):
        check = SustainedCPUCheck()
        self.assertFalse(check.observe(0, 0, 100, 2))
        self.assertFalse(check.observe(30, 5900, 100, 2))
        self.assertTrue(check.observe(60, 11600, 100, 2))
        with self.assertRaises(ValueError):
            check.observe(59, 11601, 100, 2)

    def test_invalid_inputs_fail_closed(self):
        for rate in (-1, 0, 100001):
            with self.assertRaises(ValueError):
                forecast(rate, 'search', **self.args())
        with self.assertRaises(ValueError):
            forecast(100, 'unknown', **self.args())


if __name__ == '__main__':
    unittest.main()
