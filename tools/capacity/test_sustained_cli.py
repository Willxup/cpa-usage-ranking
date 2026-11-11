"""Real subprocess/HTTP wiring check; not a performance test."""
import json
import os
import signal
import subprocess
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from sustained_deadline import now_ns
from sustained_process import LocalDriver


class FakeHandler(BaseHTTPRequestHandler):
    ids = {}
    lock = threading.Lock()

    def log_message(self, *_):
        pass

    def answer(self, status, obj):
        data = json.dumps(obj).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        self.rfile.read(int(self.headers['Content-Length']))
        key = self.headers.get('Authorization')
        with self.lock:
            if key not in self.ids:
                self.ids[key] = 21 + len(self.ids)
            client = self.ids[key]
        self.answer(201, {'client_id': client})

    def do_PUT(self):
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        self.answer(200, {'revision': str(body['revision'])})


def await_sample(path, predicate, proc, timeout=12):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        if proc.poll() is not None:
            raise AssertionError('driver exited early: ' + str(proc.returncode))
        if path.exists():
            for line in path.read_text().splitlines():
                try:
                    row = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if predicate(row):
                    return row
        time.sleep(.05)
    raise AssertionError('driver sample timed out')


@unittest.skipUnless(os.environ.get('CAPACITY_TEST_BIN'), 'requires explicitly built Go CLI')
class ActualCLITest(unittest.TestCase):
    def test_commands_samples_signal_clean_gzip_replay(self):
        binary = os.environ['CAPACITY_TEST_BIN']
        root = Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory(dir=os.environ['TMPDIR']) as temp:
            d = Path(temp)
            db, fixture, journal = d/'seed.sqlite', d/'fixture.json', d/'journal.jsonl.gz'
            seed = subprocess.run([binary, 'seed-sustained', '--db', str(db), '--fixture', str(fixture),
                                   '--models', str(root/'config/models.json'), '--clients', '20',
                                   '--history-days', '90', '--backlog', '20', '--model-seed', '8675309'],
                                  capture_output=True, text=True, timeout=60)
            self.assertEqual(seed.returncode, 0, seed.stderr)
            FakeHandler.ids = {}
            server = ThreadingHTTPServer(('127.0.0.1', 0), FakeHandler)
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            processes = []
            local_driver = third_driver = fourth_driver = None
            try:
                def start(name, resume=False):
                    samples = d/(name+'-samples.jsonl')
                    stdout, stderr = (d/(name+'-out')).open('w+'), (d/(name+'-err')).open('w+')
                    argv = [binary, 'sustained', '--fixture', str(fixture), '--models', str(root/'config/models.json'),
                            '--model-seed', '8675309', '--arrival-seed', '20261113', '--max-pending', '1000',
                            '--max-in-flight', '20', '--max-journal-bytes', str(16 << 20), '--active-budget', '1h',
                            '--journal', str(journal), '--samples', str(samples),
                            '--url', 'http://127.0.0.1:'+str(server.server_port)]
                    if resume:
                        argv.append('--resume')
                    p = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=stdout, stderr=stderr,
                                         text=True, start_new_session=True)
                    processes.append((p, stdout, stderr))
                    await_sample(samples, lambda s: s['target_rate'] == 0, p)
                    return p, samples, stdout, stderr

                def command(p, item):
                    p.stdin.write(json.dumps(item)+'\n')
                    p.stdin.flush()

                p, samples, out, err = start('first')
                command(p, {'operation': 'stage', 'stage': 'smoke', 'rate': 5})
                first = await_sample(samples, lambda s: s['stage'] == 'smoke' and s['produced'] >= 25, p)
                self.assertGreater(first['stage_start_unix_ns'], 0)
                self.assertLess(first['max_arrival_lag_ms'], 5000)
                os.kill(p.pid, signal.SIGINT)
                self.assertEqual(p.wait(timeout=10), 1)
                out.seek(0)
                self.assertEqual(json.load(out)['reason'], 'context_canceled')
                err.seek(0)
                self.assertIn('context canceled', err.read())
                local_driver = LocalDriver(binary, fixture, root/'config/models.json', journal, d/'second',
                                           'http://127.0.0.1:'+str(server.server_port), resume=True,
                                           max_pending=1000, max_in_flight=20, max_journal_bytes=16 << 20)
                local_driver.start(now_ns()+60_000_000_000)  # paused replay watchdog
                self.assertEqual(local_driver.next_sample()['target_rate'], 0)
                local_driver.arm(now_ns()+25_000_000_000)  # independent active deadline
                local_driver.send({'operation': 'resume'})
                while True:
                    second = local_driver.next_sample()
                    if second['target_rate'] == 5 and second['produced'] > first['produced']:
                        break
                self.assertGreaterEqual(second['confirmed'], first['confirmed'])
                self.assertEqual(local_driver.stop()['reason'], 'requested_stop')
                self.assertIn('driver_exited', local_driver.watch_log.read_text())
                self.assertIn('driver_exited', (local_driver.directory/'deadline-1.jsonl').read_text())
                # A killed independent timer must stop the live driver even if
                # the controller is blocked elsewhere, not leave load running.
                third_driver = LocalDriver(binary, fixture, root/'config/models.json', journal, d/'third',
                                           'http://127.0.0.1:'+str(server.server_port), resume=True,
                                           max_pending=1000, max_in_flight=20, max_journal_bytes=16 << 20)
                third_driver.start(now_ns()+60_000_000_000)
                self.assertEqual(third_driver.next_sample()['target_rate'], 0)
                third_driver.arm(now_ns()+25_000_000_000)
                third_driver.send({'operation': 'resume'})
                signal.pidfd_send_signal(third_driver.watches[1]['pidfd'], signal.SIGKILL)
                third_driver.proc.wait(timeout=10)
                end = time.monotonic()+3
                while not third_driver.liveness_error and time.monotonic() < end:
                    time.sleep(.02)
                self.assertIsNotNone(third_driver.liveness_error)
                third_driver.close()
                third_driver = None
                lost = {'value': False}
                fourth_driver = LocalDriver(binary, fixture, root/'config/models.json', journal, d/'fourth',
                                            'http://127.0.0.1:'+str(server.server_port), resume=True,
                                            max_pending=1000, max_in_flight=20, max_journal_bytes=16 << 20,
                                            external_guard=lambda: 'mock guard stale' if lost['value'] else None)
                fourth_driver.start(now_ns()+60_000_000_000)
                self.assertEqual(fourth_driver.next_sample()['target_rate'], 0)
                fourth_driver.arm(now_ns()+25_000_000_000)
                fourth_driver.send({'operation': 'resume'})
                lost['value'] = True
                fourth_driver.proc.wait(timeout=10)
                self.assertEqual(fourth_driver.liveness_error, 'mock guard stale')
            finally:
                if fourth_driver:
                    fourth_driver.close()
                if third_driver:
                    third_driver.close()
                if local_driver:
                    local_driver.close()
                for p, out, err in processes:
                    if p.poll() is None:
                        p.kill()
                        p.wait(timeout=5)
                    if p.stdin:
                        p.stdin.close()
                    out.close()
                    err.close()
                server.shutdown()
                server.server_close()
                thread.join(timeout=5)


if __name__ == '__main__':
    unittest.main()
