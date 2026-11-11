#!/usr/bin/env python3
"""Exclusive C54 SSH tunnel/guard session, derived from the prior task session.

NOT a formal entry point by itself. The owning controller must have prepared
an exclusive task root and must supervise this process and its sample stream.
This program never launches a load generator or the test application.
"""
import json
import os
import select
import shlex
import signal
import subprocess
import sys
import threading
import time
from pathlib import Path

from sustained_deadline import proc_info
from sustained_remote_spec import RemoteSpec

SSH = '/usr/bin/ssh'  # Direct child PID is required for identity verification.


def local_process_info(pid):
    info = proc_info(pid)
    path = Path('/proc')/str(pid)
    stat = (path/'stat').read_text().rsplit(')', 1)[1].split()
    status = (path/'status').read_text().splitlines()
    limits = (path/'limits').read_text().splitlines()
    info.update({'pid': pid, 'fd_count': len(list((path/'fd').iterdir())),
                 'rss_kib': next((int(x.split()[1]) for x in status if x.startswith('VmRSS:')), 0),
                 'cpu_ticks': int(stat[11])+int(stat[12]),
                 'nofile_line': next(x for x in limits if x.startswith('Max open files'))})
    return info


def stop_owned_unit(spec, command, unit, description, *, service):
    """Stop only a unit whose description, task root and dependency match."""
    query = ('systemctl show '+shlex.quote(unit)+
             ' -p ActiveState -p Description -p BindsTo -p BoundBy -p WorkingDirectory -p ExecStart')
    raw = command(query)
    data = dict(line.split('=', 1) for line in raw.splitlines() if '=' in line)
    active = data.get('ActiveState')
    if active in ('inactive', 'failed'):
        return
    if active not in ('active', 'activating', 'deactivating', 'reloading'):
        raise RuntimeError('unit state unknown: '+unit)
    proven = data.get('Description') == description
    if service:
        proven = proven and data.get('WorkingDirectory') == spec.root and spec.watcher in data.get('BindsTo', '').split()
    else:
        bound_by = data.get('BoundBy')
        proven = (proven and spec.root+'/bin/watch.py' in data.get('ExecStart', '') and
                  bound_by is not None and set(bound_by.split()).issubset({spec.service}))
    if not proven:
        raise RuntimeError('unit ownership not proven: '+unit)
    command('systemctl stop '+shlex.quote(unit), timeout=25)
    after = command(query)
    state = dict(line.split('=', 1) for line in after.splitlines() if '=' in line).get('ActiveState')
    if state not in ('inactive', 'failed'):
        raise RuntimeError('unit stop could not be verified: '+unit)


class Session:
    def __init__(self, config):
        self.d = Path(config['local_session_dir'])
        if not self.d.is_absolute() or not self.d.is_dir() or self.d.is_symlink():
            raise ValueError('new private local session directory required')
        stat = self.d.stat()
        if stat.st_uid != os.getuid() or stat.st_mode & 0o077:
            raise ValueError('local session directory must be private and task owned')
        if config['host'] != 'lax-racknerd':
            raise ValueError('unapproved remote host')
        self.host = config['host']
        self.spec = RemoteSpec(config['remote_root'], config['service'], config['watch'],
                               config['application'], config['catalog'], config['web'])
        self.base = [SSH, '-S', 'none', '-o', 'ControlMaster=no', '-o', 'BatchMode=yes',
                     '-o', 'ConnectTimeout=8', self.host]
        self.parent = os.getppid()
        self.parent_start = proc_info(self.parent)['start_ticks']
        self.parentfd = os.pidfd_open(self.parent)
        self.parentpoll = select.poll()
        self.parentpoll.register(self.parentfd, select.POLLIN)
        self.deadline = time.monotonic()+6900
        self.abort = threading.Event()
        self.monitor_last = 0.0
        self.monitor_error = None
        self.master = self.monitor = self.monitor_thread = None
        self.masterfd = self.monitorfd = None
        self.watch_start_attempted = False
        self.files = []
        self.log = (self.d/'session-events.jsonl').open('x', buffering=1, encoding='utf-8')
        self.sock = self.d/'f'  # -S f with cwd avoids AF_UNIX path length limits.

    def emit(self, event, **fields):
        row = {'time_ns': time.time_ns(), 'event': event, **fields}
        self.log.write(json.dumps(row, sort_keys=True)+'\n')
        self.log.flush()

    def safe_emit(self, event, **fields):
        try:
            self.emit(event, **fields)
        except OSError as exc:
            print('session evidence failure: '+repr(exc), file=sys.stderr, flush=True)

    def independent(self, command, timeout=12, check=True):
        try:
            p = subprocess.run(self.base+[command], capture_output=True, text=True, timeout=timeout)
        except BaseException as exc:
            self.emit('command_exception', command=command, returncode=None, stderr=repr(exc))
            raise
        self.emit('command_result', command=command, returncode=p.returncode,
                  stdout=p.stdout, stderr=p.stderr)
        if check and p.returncode:
            raise RuntimeError('independent SSH failed: '+p.stderr[-1000:])
        return p

    def cleanup_command(self, command, timeout=12):
        try:
            p = subprocess.run(self.base+[command], capture_output=True, text=True, timeout=timeout)
            self.safe_emit('cleanup_command', command=command, returncode=p.returncode,
                           stdout=p.stdout, stderr=p.stderr)
            if p.returncode:
                raise RuntimeError('cleanup SSH returned '+str(p.returncode))
            return p.stdout
        except BaseException as exc:
            self.safe_emit('cleanup_command_error', command=command, returncode=None, stderr=repr(exc))
            raise

    def cleanup_unit(self, unit, description, *, service):
        if self.watch_start_attempted:
            stop_owned_unit(self.spec, self.cleanup_command, unit, description, service=service)

    @staticmethod
    def signal_child(process, pidfd, sig):
        if process is not None and process.poll() is None:
            if pidfd is None:
                raise RuntimeError('cannot safely signal child without pidfd')
            try:
                signal.pidfd_send_signal(pidfd, sig)
            except ProcessLookupError:
                pass

    def check(self):
        if self.parentpoll.poll(0):
            raise RuntimeError('owning_controller_exited')
        if time.monotonic() >= self.deadline:
            raise RuntimeError('session_deadline')
        if self.abort.is_set():
            raise RuntimeError('remote_monitor_failed: '+str(self.monitor_error))
        if self.monitor_last and time.monotonic()-self.monitor_last > 4:
            raise RuntimeError('remote_monitor_stale')
        if self.master is not None and self.master.poll() is not None:
            raise RuntimeError('tunnel_master_exited')

    def read_monitor(self):
        try:
            with (self.d/'remote-samples.jsonl').open('x', buffering=1, encoding='utf-8') as out:
                for line in self.monitor.stdout:
                    row = json.loads(line)
                    row['observed_local_unix_ns'] = time.time_ns()
                    out.write(json.dumps(row, sort_keys=True)+'\n')
                    out.flush()
                    self.monitor_last = time.monotonic()
                    if row.get('fatal'):
                        raise RuntimeError(str(row['fatal']))
        except BaseException as exc:
            self.monitor_error = repr(exc)
        finally:
            self.abort.set()

    def run(self):
        result = 'failed'
        try:
            if self.sock.exists():
                raise RuntimeError('socket already exists')
            root, service, watcher = self.spec.root, self.spec.service, self.spec.watcher
            inactive = (f'! systemctl is-active --quiet {shlex.quote(service)} && '
                        f'! systemctl is-active --quiet {shlex.quote(watcher)} && '
                        f'test ! -e {shlex.quote(root+"/STOPPED")}')
            self.independent(inactive)
            offset = int(self.independent('stat -c %s '+shlex.quote(root+'/logs/watch.jsonl')).stdout.strip())
            self.watch_start_attempted = True
            self.independent(shlex.join(self.spec.watch_args()))
            watch_props = self.independent('systemctl show '+shlex.quote(watcher), timeout=10)
            mismatch = self.spec.check_watch_properties(watch_props.stdout)
            if mismatch:
                raise RuntimeError('watch configuration mismatch: '+json.dumps(mismatch, sort_keys=True))
            fresh_code = ('import json,sys,time;from pathlib import Path;p=Path(sys.argv[1]);'
                          'f=p.open("rb");f.seek(max(0,p.stat().st_size-131072));'
                          'rows=f.read().splitlines();r=json.loads(rows[-1]) if rows else {};'
                          'print(json.dumps({"size":p.stat().st_size,"age":time.time()-r.get("time",0),'
                          '"stop_reason":r.get("stop_reason")}))')
            end = time.monotonic()+25
            while True:
                self.check()
                if time.monotonic() >= end:
                    raise RuntimeError('watch_first_sample_timeout')
                self.independent('systemctl is-active --quiet '+shlex.quote(watcher)+' && test ! -e '+shlex.quote(root+'/STOPPED'), timeout=6)
                sample = json.loads(self.independent(shlex.join(['/usr/bin/python3', '-c', fresh_code,
                                                                  root+'/logs/watch.jsonl', str(offset)]), timeout=6).stdout)
                if sample.get('stop_reason'):
                    raise RuntimeError('watch_first_sample_reports_stop')
                if sample['size'] > offset and sample['age'] < 8:
                    break
                time.sleep(.5)
            debug = (self.d/'tunnel-debug.log').open('x', encoding='utf-8')
            self.files.append(debug)
            self.master = subprocess.Popen([SSH, '-vv', '-o', 'BatchMode=yes', '-o', 'ExitOnForwardFailure=yes',
                                            '-M', '-S', 'f', '-N', '-L', '127.0.0.1:14210:127.0.0.1:4210', self.host],
                                           stdout=debug, stderr=debug, cwd=self.d, start_new_session=True)
            self.masterfd = os.pidfd_open(self.master.pid)
            master_info = local_process_info(self.master.pid)
            self.emit('master_start', pid=self.master.pid, start_ticks=master_info['start_ticks'], returncode=None,
                      stderr_path=str(self.d/'tunnel-debug.log'))
            for _ in range(100):
                self.check()
                if self.sock.exists():
                    p = subprocess.run([SSH, '-S', 'f', '-O', 'check', self.host], cwd=self.d,
                                       capture_output=True, text=True, timeout=3)
                    self.emit('master_check', returncode=p.returncode, stdout=p.stdout, stderr=p.stderr)
                    if p.returncode == 0:
                        break
                time.sleep(.1)
            else:
                raise RuntimeError('tunnel_socket_unavailable')
            probe_cmd = shlex.join(['/usr/bin/python3', root+'/bin/probe-ancestry.py'])
            p = subprocess.run([SSH, '-S', 'f', '-o', 'ControlMaster=no', '-o', 'BatchMode=yes',
                                self.host, probe_cmd], cwd=self.d, capture_output=True, text=True, timeout=10)
            self.emit('ancestry_probe', returncode=p.returncode, stdout=p.stdout, stderr=p.stderr)
            if p.returncode:
                raise RuntimeError('forward_ancestry_probe_failed')
            ancestry = json.loads(p.stdout)
            chain = ancestry['chain']
            tracks = [entry for entry in chain if entry['owns_ssh_transport']]
            if len(tracks) != 2 or len(chain) < 4 or chain[1] != tracks[0] or chain[2] != tracks[1]:
                raise RuntimeError('cannot_prove_forwarding_worker_and_parent')
            worker = tracks[0]
            p = self.independent(shlex.join(['/usr/bin/python3', root+'/bin/adjust-worker.py', json.dumps(ancestry)]), timeout=10)
            adjustment = json.loads(p.stdout)
            if not adjustment.get('hard_limit_preserved'):
                raise RuntimeError('worker_hard_limit_readback_missing')
            monitor_err = (self.d/'remote-monitor-stderr.log').open('x', encoding='utf-8')
            self.files.append(monitor_err)
            monitor_command = shlex.join(['/usr/bin/python3', root+'/bin/remote-monitor.py', root, service, watcher,
                                          root+'/logs', json.dumps(worker), str(adjustment['after'][0]['nofile'][1])])
            self.monitor = subprocess.Popen(self.base+[monitor_command], stdout=subprocess.PIPE, stderr=monitor_err,
                                            text=True, bufsize=1, start_new_session=True)
            self.monitorfd = os.pidfd_open(self.monitor.pid)
            self.monitor_thread = threading.Thread(target=self.read_monitor, daemon=True)
            self.monitor_thread.start()
            for _ in range(100):
                self.check()
                if self.monitor_last:
                    break
                time.sleep(.05)
            else:
                raise RuntimeError('remote_monitor_no_samples')
            ready = {'time_ns': time.time_ns(), 'master': master_info, 'worker': worker,
                     'adjustment': adjustment, 'service': service, 'watch': watcher}
            (self.d/'session-ready.json').write_text(json.dumps(ready, sort_keys=True)+'\n')
            self.emit('ready', **ready)
            last_local = 0.0
            while not (self.d/'stop-requested').exists():
                self.check()
                if time.monotonic()-last_local >= 5:
                    current = local_process_info(self.master.pid)
                    if current['start_ticks'] != master_info['start_ticks'] or current['nofile_line'] != master_info['nofile_line']:
                        raise RuntimeError('local SSH master identity or limits changed')
                    with (self.d/'local-ssh-samples.jsonl').open('a', encoding='utf-8') as out:
                        out.write(json.dumps({'time_ns': time.time_ns(), 'master': current}, sort_keys=True)+'\n')
                        out.flush()
                    last_local = time.monotonic()
                time.sleep(.2)
            self.check()
            result = 'completed'
        except BaseException as exc:
            self.safe_emit('session_error', error=repr(exc))
        finally:
            for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
                signal.signal(sig, signal.SIG_IGN)
            # Even a broken evidence sink cannot skip a safety stop.
            service_safe = True
            for unit, desc, is_service in ((self.spec.service, self.spec.owner+'-service', True),
                                           (self.spec.watcher, self.spec.owner+'-watch', False)):
                if not is_service and not service_safe:
                    self.safe_emit('watch_cleanup_skipped', reason='service ownership or stopped state unproven')
                    continue
                try:
                    self.cleanup_unit(unit, desc, service=is_service)
                except BaseException as exc:
                    result = 'failed'
                    if is_service:
                        service_safe = False
                    self.safe_emit('unit_cleanup_error', unit=unit, error=repr(exc))
            if self.monitor is not None:
                try:
                    self.signal_child(self.monitor, self.monitorfd, signal.SIGTERM)
                    try:
                        self.monitor.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        self.signal_child(self.monitor, self.monitorfd, signal.SIGKILL)
                        self.monitor.wait(timeout=5)
                    if self.monitor_thread:
                        self.monitor_thread.join(timeout=3)
                    self.safe_emit('monitor_exit', returncode=self.monitor.returncode,
                                   stderr=(self.d/'remote-monitor-stderr.log').read_text())
                except BaseException as exc:
                    result = 'failed'
                    self.safe_emit('monitor_cleanup_error', error=repr(exc))
            if self.master is not None:
                try:
                    p = subprocess.run([SSH, '-S', 'f', '-O', 'exit', self.host], cwd=self.d,
                                       capture_output=True, text=True, timeout=5)
                    self.safe_emit('master_control_exit', returncode=p.returncode, stdout=p.stdout, stderr=p.stderr)
                except BaseException as exc:
                    result = 'failed'
                    self.safe_emit('master_control_error', error=repr(exc))
                try:
                    try:
                        self.master.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        self.signal_child(self.master, self.masterfd, signal.SIGTERM)
                        try:
                            self.master.wait(timeout=5)
                        except subprocess.TimeoutExpired:
                            self.signal_child(self.master, self.masterfd, signal.SIGKILL)
                            self.master.wait(timeout=5)
                    self.safe_emit('master_exit', returncode=self.master.returncode)
                except BaseException as exc:
                    result = 'failed'
                    self.safe_emit('master_cleanup_error', error=repr(exc))
            self.safe_emit('session_finished', result=result)
            self.log.close()
            for f in self.files:
                f.close()
            for fd in (self.masterfd, self.monitorfd, self.parentfd):
                if fd is not None:
                    os.close(fd)
        return 0 if result == 'completed' else 1


if __name__ == '__main__':
    if len(sys.argv) != 2:
        raise SystemExit('usage: sustained_session.py private-session-config.json')
    def interrupted(signum, _frame):
        raise RuntimeError('session interrupted by signal '+str(signum))
    for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(sig, interrupted)
    with open(sys.argv[1], encoding='utf-8') as f:
        config = json.load(f)
    sys.exit(Session(config).run())
