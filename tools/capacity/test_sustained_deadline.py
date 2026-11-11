import json
import os
import select
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from sustained_deadline import now_ns, proc_info

SCRIPT = Path(__file__).with_name('sustained_deadline.py')


class DeadlineTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir=os.environ['TMPDIR'])
        self.driver = None
        self.watch = None

    def tearDown(self):
        if self.watch and self.watch.poll() is None:
            self.watch.kill()
            self.watch.wait(timeout=5)
        if self.driver and self.driver.poll() is None:
            self.driver.kill()
            self.driver.wait(timeout=5)
        for process in (self.driver, self.watch):
            if process:
                for stream in (process.stdout, process.stderr):
                    if stream:
                        stream.close()
        self.tmp.cleanup()

    def start(self, source, delay=0.7, bad_ticks=False):
        self.driver = subprocess.Popen([sys.executable, '-u', '-c', source],
                                       stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       text=True, start_new_session=True)
        self.assertEqual(self.driver.stdout.readline().strip(), 'ready')
        child = proc_info(self.driver.pid)
        owner = proc_info(os.getpid())
        log = str(Path(self.tmp.name) / 'deadline.jsonl')
        self.watch = subprocess.Popen([
            sys.executable, str(SCRIPT), str(self.driver.pid),
            str(child['start_ticks'] + int(bad_ticks)), str(os.getpid()),
            str(owner['start_ticks']), str(now_ns() + int(delay * 1e9)), log, '0.2',
        ], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        return log

    def test_deadline_terminates_unresponsive_owned_process(self):
        log = self.start('import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); print("ready",flush=True); time.sleep(60)')
        self.assertEqual(json.loads(self.watch.stdout.readline())['event'], 'ready')
        self.assertEqual(self.watch.wait(timeout=5), 2)
        self.assertEqual(self.driver.wait(timeout=5), -signal.SIGKILL)
        events = [json.loads(line) for line in Path(log).read_text().splitlines()]
        signals = [row for row in events if row['event'] == 'signal']
        self.assertEqual([row['signal'] for row in signals], ['SIGTERM', 'SIGKILL'])
        self.assertLess(signals[0]['boottime_ns'], events[0]['deadline_ns'])
        self.assertLessEqual(signals[1]['boottime_ns'], events[0]['deadline_ns']+100_000_000)
        self.assertEqual(events[-1]['reason'], 'deadline')

    def test_normal_exit_disarms_without_signal(self):
        log = self.start('import time; print("ready",flush=True); time.sleep(60)', delay=10)
        self.assertEqual(json.loads(self.watch.stdout.readline())['event'], 'ready')
        self.driver.terminate()
        self.driver.wait(timeout=5)
        self.assertEqual(self.watch.wait(timeout=5), 0)
        events = [json.loads(line)['event'] for line in Path(log).read_text().splitlines()]
        self.assertEqual(events, ['ready', 'driver_exited'])

    def test_parent_exit_stops_owned_driver(self):
        log = str(Path(self.tmp.name)/'orphan-deadline.jsonl')
        code = '''import json,os,subprocess,sys,time
from sustained_deadline import now_ns,proc_info
child=subprocess.Popen([sys.executable,"-u","-c","import time;print('ready',flush=True);time.sleep(60)"],stdout=subprocess.PIPE,text=True,start_new_session=True)
child.stdout.readline()
a,b=proc_info(child.pid),proc_info(os.getpid())
w=subprocess.Popen([sys.executable,sys.argv[1],str(child.pid),str(a['start_ticks']),str(os.getpid()),str(b['start_ticks']),str(now_ns()+20000000000),sys.argv[2],"0.2"],stdout=subprocess.PIPE,text=True,start_new_session=True)
line=w.stdout.readline()
print(json.dumps({'pid':child.pid,'ready':json.loads(line)['event']}),flush=True)
os._exit(0)
'''
        env = os.environ.copy()
        env['PYTHONPATH'] = str(SCRIPT.parent)
        parent = subprocess.Popen([sys.executable, '-c', code, str(SCRIPT), log], stdout=subprocess.PIPE,
                                  stderr=subprocess.PIPE, text=True, env=env, start_new_session=True)
        try:
            row = json.loads(parent.stdout.readline())
            self.assertEqual(row['ready'], 'ready')
            try:
                fd = os.pidfd_open(row['pid'])
            except ProcessLookupError:
                fd = None  # Already exited before observation.
            self.assertEqual(parent.wait(timeout=5), 0)
            if fd is not None:
                try:
                    poll = select.poll()
                    poll.register(fd, select.POLLIN)
                    if not poll.poll(5000):
                        signal.pidfd_send_signal(fd, signal.SIGKILL)
                        self.fail('driver survived its parent')
                finally:
                    os.close(fd)
            end = time.monotonic()+3
            while time.monotonic() < end:
                events = [json.loads(line) for line in Path(log).read_text().splitlines()]
                if any(e.get('reason') == 'parent_exited' for e in events):
                    break
                time.sleep(.02)
            self.assertTrue(any(e.get('reason') == 'parent_exited' for e in events))
        finally:
            if parent.poll() is None:
                parent.kill()
                parent.wait(timeout=5)
            parent.stdout.close()
            parent.stderr.close()

    def test_watchdog_signal_stops_owned_driver(self):
        log = self.start('import time; print("ready",flush=True); time.sleep(60)', delay=10)
        self.assertEqual(json.loads(self.watch.stdout.readline())['event'], 'ready')
        self.watch.terminate()
        self.assertEqual(self.watch.wait(timeout=5), 2)
        self.assertEqual(self.driver.wait(timeout=5), -signal.SIGTERM)
        events = [json.loads(line) for line in Path(log).read_text().splitlines()]
        self.assertIn('watchdog_error', events[-1]['reason'])

    def test_wrong_start_ticks_never_signals(self):
        self.start('import time; print("ready",flush=True); time.sleep(60)', bad_ticks=True)
        self.assertEqual(self.watch.wait(timeout=5), 1)
        self.assertIsNone(self.driver.poll())
        self.assertIn('identity', self.watch.stderr.read())


if __name__ == '__main__':
    unittest.main()
