#!/usr/bin/env python3
"""Independent fail-closed deadline for one *verified* local driver PID.

Linux pidfds pin identities. Never signal a process group, service, SSH, or a
PID discovered by name. The parent must wait for the JSON ready line before
sending a stage. The watchdog exits only when the driver exits; parent death,
a boot-time deadline, or an internal error trigger TERM then KILL of that PID.
"""
import json
import os
import select
import signal
import sys
import time
from pathlib import Path


def proc_info(pid):
    fields = (Path('/proc') / str(pid) / 'stat').read_text().rsplit(')', 1)[1].split()
    status = (Path('/proc') / str(pid) / 'status').read_text().splitlines()
    return {
        'state': fields[0], 'ppid': int(fields[1]), 'pgrp': int(fields[2]),
        'session': int(fields[3]), 'start_ticks': int(fields[19]),
        'uid': int(next(x for x in status if x.startswith('Uid:')).split()[1]),
    }


def now_ns():
    return time.clock_gettime_ns(time.CLOCK_BOOTTIME)


def run(args):
    if len(args) != 7:
        raise ValueError('expected driver pid/ticks, parent pid/ticks, deadline ns, log, grace seconds')
    pid, ticks, parent, parent_ticks, deadline = map(int, args[:5])
    path, grace = args[5], float(args[6])
    if pid <= 1 or parent != os.getppid() or not 0 < grace <= 15:
        raise ValueError('invalid process identity or grace')
    soft_deadline = deadline-int(grace*1_000_000_000)
    if soft_deadline <= now_ns():
        raise ValueError('insufficient time before hard deadline')
    with open(path, 'x', encoding='utf-8', buffering=1) as log:
        def emit(event, **kw):
            line = json.dumps({'event': event, 'boottime_ns': now_ns(), **kw}, sort_keys=True)
            log.write(line + '\n')
            log.flush()
            os.fsync(log.fileno())
            return line

        childfd = os.pidfd_open(pid)
        parentfd = os.pidfd_open(parent)
        poll = select.poll()
        poll.register(childfd, select.POLLIN)
        poll.register(parentfd, select.POLLIN)
        child = proc_info(pid)
        owner = proc_info(parent)
        if (child['start_ticks'] != ticks or child['ppid'] != parent or
                child['pgrp'] != pid or child['session'] != pid or
                child['uid'] != os.getuid() or owner['start_ticks'] != parent_ticks or
                owner['uid'] != os.getuid() or poll.poll(0)):
            raise RuntimeError('driver or parent identity could not be proven')
        def interrupted(signum, _frame):
            raise RuntimeError('watchdog interrupted by signal '+str(signum))

        for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            signal.signal(sig, interrupted)
        reason = None
        try:
            ready = emit('ready', driver_pid=pid, driver_start_ticks=ticks,
                         parent_pid=parent, parent_start_ticks=parent_ticks, deadline_ns=deadline)
            print(ready, flush=True)
            while True:
                timeout = max(0, min(250, (soft_deadline - now_ns()) // 1_000_000))
                events = poll.poll(timeout)
                if any(fd == childfd for fd, _ in events):
                    emit('driver_exited')
                    return 0
                if any(fd == parentfd for fd, _ in events):
                    reason = 'parent_exited'
                    break
                if now_ns() >= soft_deadline:
                    reason = 'deadline'
                    break
        except BaseException as exc:
            reason = 'watchdog_error:' + repr(exc)
        for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            signal.signal(sig, signal.SIG_IGN)
        # A pidfd refers only to the originally validated driver, even if its
        # numeric PID is later recycled. Never fall back to os.kill(pid).
        def log_best_effort(event, **fields):
            try:
                emit(event, **fields)
            except OSError as exc:
                print('deadline evidence failure: ' + repr(exc), file=sys.stderr, flush=True)

        try:
            signal.pidfd_send_signal(childfd, signal.SIGTERM)
            log_best_effort('signal', signal='SIGTERM', reason=reason)
        except ProcessLookupError:
            log_best_effort('driver_already_exited', reason=reason)
            return 0
        driver_poll = select.poll()
        driver_poll.register(childfd, select.POLLIN)
        # TERM begins before the hard deadline; KILL is no later than that
        # deadline even if the Go event loop is blocked or ignores TERM.
        wait_ms = max(0, min(int(grace*1000), (deadline-now_ns())//1_000_000))
        if not driver_poll.poll(wait_ms):
            try:
                signal.pidfd_send_signal(childfd, signal.SIGKILL)
                log_best_effort('signal', signal='SIGKILL', reason=reason)
            except ProcessLookupError:
                log_best_effort('driver_already_exited', reason=reason)
        return 2


if __name__ == '__main__':
    try:
        sys.exit(run(sys.argv[1:]))
    except BaseException as exc:
        if isinstance(exc, SystemExit):
            raise
        print('deadline watchdog failed: ' + repr(exc), file=sys.stderr, flush=True)
        sys.exit(1)
