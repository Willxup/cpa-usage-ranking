"""Guarded C54 seed/snapshot transfer and offline verifier subprocess."""
import os
import select
import signal
import subprocess
import time
from pathlib import Path

from sustained_deadline import proc_info


def signal_owned_group(p, pidfd, identity, sig):
    if p.poll() is not None:
        return
    poll = select.poll()
    poll.register(pidfd, select.POLLIN)
    current = proc_info(p.pid)
    if (poll.poll(0) or current['start_ticks'] != identity['start_ticks'] or
            current['ppid'] != os.getpid() or current['pgrp'] != p.pid or current['session'] != p.pid):
        raise RuntimeError('task subprocess group identity changed')
    os.killpg(p.pid, sig)


def guarded_command(argv, directory, label, guard, record, *, timeout):
    guard()
    directory = Path(directory)
    stdout_path, stderr_path = directory/(label+'.stdout'), directory/(label+'.stderr')
    with stdout_path.open('x', encoding='utf-8') as out, stderr_path.open('x', encoding='utf-8') as err:
        p = subprocess.Popen(argv, stdout=out, stderr=err, text=True, start_new_session=True)
        pidfd = os.pidfd_open(p.pid)
        identity = proc_info(p.pid)
        reason = None
        try:
            record({'event': 'guarded_command_start', 'argv': argv, 'pid': p.pid,
                    'start_ticks': identity['start_ticks'], 'returncode': None,
                    'stdout_path': str(stdout_path), 'stderr_path': str(stderr_path)})
            end = time.monotonic()+timeout
            while p.poll() is None:
                if time.monotonic() >= end:
                    raise TimeoutError(label+' timed out')
                guard()
                time.sleep(.5)
            guard()
        except BaseException as exc:
            reason = repr(exc)
            if p.poll() is None:
                signal_owned_group(p, pidfd, identity, signal.SIGTERM)
                try:
                    p.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    signal_owned_group(p, pidfd, identity, signal.SIGKILL)
                    p.wait(timeout=5)
            raise
        finally:
            out.flush()
            err.flush()
            try:
                record({'event': 'guarded_command_exit', 'argv': argv, 'pid': p.pid,
                        'returncode': p.poll(), 'stderr_path': str(stderr_path),
                        'stdout_path': str(stdout_path), 'error': reason})
            finally:
                os.close(pidfd)
        if p.returncode:
            raise RuntimeError(label+' returned '+str(p.returncode)+'; stderr: '+str(stderr_path))
    return stdout_path, stderr_path
