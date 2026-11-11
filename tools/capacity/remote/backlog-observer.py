#!/usr/bin/env python3
"""Read-only, one bounded closed-source count per 15s, inside service cgroup."""
import json
import os
import resource
import signal
import sqlite3
import sys
import time
from pathlib import Path

path, output = sys.argv[1:]
running = True

def stop(_sig, _frame):
    global running
    running = False

signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)
with open(output, 'x', buffering=1) as out:
    while running:
        began = time.monotonic()
        row = {'time':time.time(), 'pid':os.getpid()}
        try:
            db = sqlite3.connect(Path(path).as_uri()+'?mode=ro', uri=True, timeout=.25)
            try:
                db.execute('PRAGMA query_only=ON')
                row['closed_backlog'] = db.execute('SELECT count(*) FROM daily_reports WHERE day < (SELECT closed_before_day FROM report_window WHERE id=1)').fetchone()[0]
            finally:
                db.close()
        except Exception as e:
            row['error'] = type(e).__name__+': '+str(e)
        row['query_seconds'] = time.monotonic()-began
        row['maxrss_kib'] = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
        row['cpu_user_seconds'] = resource.getrusage(resource.RUSAGE_SELF).ru_utime
        row['cpu_system_seconds'] = resource.getrusage(resource.RUSAGE_SELF).ru_stime
        print(json.dumps(row), file=out, flush=True)
        end = time.monotonic()+max(0, 15-(time.monotonic()-began))
        while running and time.monotonic() < end:
            time.sleep(min(.25, max(0, end-time.monotonic())))
