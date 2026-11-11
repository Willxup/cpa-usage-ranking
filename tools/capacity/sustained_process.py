"""Local Go-driver process and independent pidfd deadline; no remote actions."""
import json
import os
import select
import signal
import subprocess
import sys
import threading
import time
from pathlib import Path

from sustained_deadline import proc_info


class ProcessError(RuntimeError):
    pass


class SampleTimeout(ProcessError):
    pass


class LocalDriver:
    def __init__(self, executable, fixture, catalog, journal, directory, url, *,
                 resume=False, model_seed=8675309, arrival_seed=20261113,
                 max_pending=200000, max_in_flight=4096, max_journal_bytes=4 << 30,
                 external_guard=None):
        self.directory = Path(directory)
        self.directory.mkdir(mode=0o700, parents=True, exist_ok=False)
        self.journal = Path(journal)
        self.samples = self.directory/'samples.jsonl'
        self.stdout_path = self.directory/'stdout.json'
        self.stderr_path = self.directory/'stderr.log'
        self.event_path = self.directory/'process-events.jsonl'
        self.watch_log = self.directory/'deadline.jsonl'
        self.watch_err_path = self.directory/'deadline-stderr.log'
        self.args = [str(executable), 'sustained', '--fixture', str(fixture), '--models', str(catalog),
                     '--journal', str(journal), '--samples', str(self.samples), '--url', url,
                     '--model-seed', str(model_seed), '--arrival-seed', str(arrival_seed),
                     '--max-pending', str(max_pending), '--max-in-flight', str(max_in_flight),
                     '--max-journal-bytes', str(max_journal_bytes), '--active-budget', '1h']
        if resume:
            self.args.append('--resume')
        self.proc = self.watch = None
        self.watches = []
        self.sample_file = None
        self.sample_buffer = b''
        self.offset = 0
        self.pidfd = None
        self.files = []
        self.liveness_stop = threading.Event()
        self.liveness_error = None
        self.liveness_thread = None
        self.external_guard = external_guard

    def event(self, kind, **fields):
        line = json.dumps({'time_ns': time.time_ns(), 'event': kind, **fields}, sort_keys=True)
        with self.event_path.open('a', encoding='utf-8') as f:
            f.write(line+'\n')
            f.flush()

    def start(self, deadline_ns):
        try:
            out = self.stdout_path.open('x', encoding='utf-8')
            err = self.stderr_path.open('x', encoding='utf-8')
            self.files.extend((out, err))
            self.proc = subprocess.Popen(self.args, stdin=subprocess.PIPE, stdout=out, stderr=err,
                                         text=True, start_new_session=True)
            self.pidfd = os.pidfd_open(self.proc.pid)
            child = proc_info(self.proc.pid)
            self.start_ticks = child['start_ticks']
            self.event('driver_start', pid=self.proc.pid, start_ticks=child['start_ticks'],
                       returncode=None, stderr_path=str(self.stderr_path))
            self.arm(deadline_ns)
        except BaseException:
            self.terminate()
            raise

    def resident_anon_bytes(self):
        """Credit only anonymous RAM of this still-live, pidfd-pinned driver."""
        if self.proc is None or self.pidfd is None:
            raise ProcessError('driver memory identity unavailable')
        if select.select([self.pidfd], [], [], 0)[0]:
            raise ProcessError('driver exited during memory observation')
        child = proc_info(self.proc.pid)
        if (child['start_ticks'] != self.start_ticks or child['ppid'] != os.getpid() or
                child['uid'] != os.getuid()):
            raise ProcessError('driver memory identity changed')
        lines = (Path('/proc')/str(self.proc.pid)/'status').read_text().splitlines()
        values = [line.split()[1:] for line in lines if line.startswith('RssAnon:')]
        if len(values) != 1 or len(values[0]) != 2 or values[0][1] != 'kB':
            raise ProcessError('driver anonymous memory unavailable')
        resident = int(values[0][0])*1024
        if resident < 0 or select.select([self.pidfd], [], [], 0)[0]:
            raise ProcessError('driver exited or invalid memory observation')
        return resident

    def arm(self, deadline_ns):
        # A bootstrap watchdog protects the paused replay process; a second,
        # independently armed watchdog enforces the persisted active budget.
        if self.proc is None or self.proc.poll() is not None:
            raise ProcessError('cannot arm deadline for an exited driver')
        index = len(self.watches)
        log = self.watch_log if index == 0 else self.directory/('deadline-%d.jsonl' % index)
        error_path = self.watch_err_path if index == 0 else self.directory/('deadline-%d-stderr.log' % index)
        err = error_path.open('x', encoding='utf-8')
        self.files.append(err)
        child, parent = proc_info(self.proc.pid), proc_info(os.getpid())
        watchdog = Path(__file__).with_name('sustained_deadline.py')
        args = [sys.executable, str(watchdog), str(self.proc.pid), str(child['start_ticks']),
                str(os.getpid()), str(parent['start_ticks']), str(deadline_ns), str(log), '5']
        proc = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=err, text=True, start_new_session=True)
        fd = os.pidfd_open(proc.pid)
        self.watches.append({'process': proc, 'pidfd': fd, 'log': log, 'stderr': error_path})
        if self.watch is None:
            self.watch = proc
        readable, _, _ = select.select([proc.stdout], [], [], 8)
        if not readable:
            raise ProcessError('deadline watchdog did not become ready')
        line = proc.stdout.readline()
        if not line or json.loads(line).get('event') != 'ready' or proc.poll() is not None:
            raise ProcessError('deadline watchdog failed before driver activation')
        self.event('deadline_started', pid=proc.pid, deadline_ns=deadline_ns,
                   returncode=None, stderr_path=str(error_path))
        if self.liveness_thread is None:
            self.liveness_thread = threading.Thread(target=self._watch_liveness, daemon=True)
            self.liveness_thread.start()

    def _watch_liveness(self):
        while not self.liveness_stop.wait(.2):
            if self.proc is None or self.proc.poll() is not None:
                return
            reason = None
            if any(w['process'].poll() is not None for w in list(self.watches)):
                reason = 'deadline watchdog exited while driver was active'
            elif self.external_guard is not None:
                try:
                    reason = self.external_guard()
                except BaseException as exc:
                    reason = 'external guard check failed: '+repr(exc)
            if reason:
                self.liveness_error = str(reason)
                self._signal(signal.SIGTERM)
                try:
                    self.proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self._signal(signal.SIGKILL)
                return

    def check(self):
        if self.liveness_error:
            raise ProcessError(self.liveness_error)
        if self.proc is None or self.proc.poll() is not None:
            raise ProcessError('driver exited unexpectedly: '+str(None if self.proc is None else self.proc.returncode))
        if not self.watches or any(w['process'].poll() is not None for w in self.watches):
            raise ProcessError('independent deadline exited unexpectedly')

    def send(self, command):
        self.check()
        try:
            self.proc.stdin.write(json.dumps(command, separators=(',', ':'))+'\n')
            self.proc.stdin.flush()
            self.event('stdin_command', command=command, returncode=0, stderr='')
        except BaseException as exc:
            self.event('stdin_command', command=command, returncode=1, stderr=repr(exc))
            raise

    def next_sample(self, timeout=5):
        deadline = time.monotonic()+timeout
        while time.monotonic() < deadline:
            self.check()
            if self.sample_file is None and self.samples.exists():
                self.sample_file = self.samples.open('rb')
            if self.sample_file:
                block = self.sample_file.read(65536)
                if block:
                    self.sample_buffer += block
                    if len(self.sample_buffer) > 2 << 20:
                        raise ProcessError('sample stream record exceeds bound')
                if b'\n' in self.sample_buffer:
                    line, self.sample_buffer = self.sample_buffer.split(b'\n', 1)
                    return json.loads(line)
            time.sleep(.05)
        raise SampleTimeout('driver sample unavailable or stale')

    def _signal(self, sig):
        if self.proc is not None and self.proc.poll() is None and self.pidfd is not None:
            try:
                signal.pidfd_send_signal(self.pidfd, sig)
                try:
                    self.event('driver_signal', signal=signal.Signals(sig).name, returncode=0, stderr='')
                except OSError:
                    pass  # Evidence failure must not prevent emergency stop.
            except ProcessLookupError:
                pass

    def stop(self):
        self.send({'operation': 'stop'})
        try:
            code = self.proc.wait(timeout=12)
        except subprocess.TimeoutExpired:
            self.terminate()
            raise ProcessError('clean driver stop timed out')
        for f in self.files:
            f.flush()
        stderr = self.stderr_path.read_text()
        self.event('driver_exit', returncode=code, stderr=stderr)
        for w in self.watches:
            try:
                watch_code = w['process'].wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.terminate()
                raise ProcessError('deadline watchdog did not exit after driver')
            self.event('deadline_exit', returncode=watch_code, stderr=w['stderr'].read_text())
            if watch_code:
                raise ProcessError('deadline was triggered or failed')
        if code:
            raise ProcessError('driver stop returned nonzero')
        result = json.loads(self.stdout_path.read_text())
        if result.get('reason') != 'requested_stop' or result['sample']['in_flight'] != 0:
            raise ProcessError('driver did not report a clean planned stop')
        return result

    def terminate(self):
        if self.proc is not None and self.proc.poll() is None:
            self._signal(signal.SIGTERM)
            try:
                self.proc.wait(timeout=6)
            except subprocess.TimeoutExpired:
                self._signal(signal.SIGKILL)
                self.proc.wait(timeout=6)
        for w in self.watches:
            proc = w['process']
            try:
                proc.wait(timeout=6)
            except subprocess.TimeoutExpired:
                try:
                    signal.pidfd_send_signal(w['pidfd'], signal.SIGTERM)
                except ProcessLookupError:
                    pass
                proc.wait(timeout=6)
        if self.proc is not None:
            try:
                for f in self.files:
                    f.flush()
                self.event('driver_cleanup', returncode=self.proc.returncode,
                           stderr=self.stderr_path.read_text() if self.stderr_path.exists() else '')
            except OSError:
                pass

    def close(self):
        self.terminate()
        self.liveness_stop.set()
        if self.liveness_thread:
            self.liveness_thread.join(timeout=6)
        if self.sample_file:
            self.sample_file.close()
        if self.proc is not None and self.proc.stdin:
            self.proc.stdin.close()
        for w in self.watches:
            if w['process'].stdout:
                w['process'].stdout.close()
            os.close(w['pidfd'])
        self.watches.clear()
        if self.pidfd is not None:
            os.close(self.pidfd)
            self.pidfd = None
        for f in self.files:
            f.close()
