"""Conservative C54 local/remote resource preflight, not a product limit.

Constants are based on the local 10k-stock/1.5M-task synthetic sizing sample:
~709 compressed bytes/task and ~2.99KB retained heap per pending job. The
forecasts intentionally include margins but are not worst-case proofs: retries,
large reports, DB growth and GC/RSS can exceed them. During any real run the
adapter must also monitor actual disk, memory, journal growth and lag.
"""
from collections import deque

GIB = 1 << 30
MIB = 1 << 20
FORECAST_BYTES_PER_TASK = 2048
PENDING_HEAP_PER_TASK = 3072


class SustainedCPUCheck:
    """Conservative tool-path signal, not a claimed product CPU limit."""
    def __init__(self):
        self.samples = deque()

    def observe(self, seconds, ticks, hz, cores):
        if seconds < 0 or ticks < 0 or hz <= 0 or cores <= 0:
            raise ValueError('invalid CPU observation')
        if self.samples and (seconds < self.samples[-1][0] or ticks < self.samples[-1][1]):
            raise ValueError('non-monotonic process CPU observation')
        if self.samples and seconds == self.samples[-1][0]:
            return False
        self.samples.append((seconds, ticks))
        while len(self.samples) > 1 and seconds-self.samples[0][0] > 70:
            self.samples.popleft()
        span = seconds-self.samples[0][0]
        if span < 55:
            return False
        return (ticks-self.samples[0][1])/(hz*span*cores) >= .95


def forecast(rate, phase, *, journal_bytes, max_journal_bytes, local_free,
             remote_free, local_mem_available, pending_limit, seed_bytes, local_owned_anon=0):
    values = (rate, journal_bytes, max_journal_bytes, local_free, remote_free,
              local_mem_available, pending_limit, seed_bytes, local_owned_anon)
    if any(type(x) is not int or x < 0 for x in values) or not 1 <= rate <= 100000 or not 1 <= pending_limit <= 1000000:
        raise ValueError('invalid resource forecast input')
    if phase == 'search':
        # Reserve for the next screening stage AND a confirmation at that
        # same rate. The next stage must be re-evaluated using actual sizes.
        tasks = rate * (180 + 1200 + 10)
    elif phase == 'confirmation':
        tasks = rate * (1200 + 5)
    else:
        raise ValueError('invalid phase')
    journal_growth = tasks*FORECAST_BYTES_PER_TASK
    journal_tail = 128*MIB  # Buffered/in-flight writes are not a strict quota.
    # Two downloaded snapshots and two reconstructed DBs, 1GiB for DB/WAL
    # growth, and 3GiB machine reserve. The original seed already exists.
    local_other = 4*seed_bytes + GIB + 3*GIB
    # A staging seed and live DB plus WAL/growth allocation, preserving the
    # original remote 10GiB minimum. The seed estimate is not a growth bound.
    remote_other = 2*seed_bytes + 2*GIB + 10*GIB
    # Pending jobs + model state at roughly twice the measured live heap,
    # plus explicit network/process overhead and a 512MiB host reserve.
    mem_need = 2*(pending_limit*PENDING_HEAP_PER_TASK + 128*MIB) + 256*MIB + 512*MIB
    # MemAvailable already excludes the active driver's anonymous resident
    # memory. Reserve only the remaining estimated process footprint, while
    # always leaving the full host reserve. Never credit file-backed RSS or
    # another process. This remains an estimate, not an allocation guarantee.
    mem_additional = max(0, mem_need-512*MIB-local_owned_anon) + 512*MIB
    estimates = {'tasks': tasks, 'journal_growth_bytes': journal_growth,
                 'journal_tail_bytes': journal_tail, 'local_other_bytes': local_other,
                 'remote_other_bytes': remote_other, 'memory_need_bytes': mem_need,
                 'driver_owned_anon_bytes': local_owned_anon,
                 'memory_additional_bytes': mem_additional}
    issues = []
    if journal_bytes + journal_growth + journal_tail > max_journal_bytes:
        issues.append('journal_budget_insufficient')
    if journal_growth + journal_tail + local_other > local_free:
        issues.append('local_disk_insufficient')
    if remote_other > remote_free:
        issues.append('remote_disk_insufficient')
    if mem_additional > local_mem_available:
        issues.append('local_memory_insufficient')
    return {'ok': not issues, 'issues': issues, 'estimates': estimates,
            'limitations': 'preflight estimate, not a filesystem quota or a measured capacity bound'}
