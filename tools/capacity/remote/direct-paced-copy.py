# Task-only cold preparation: direct, 4 MiB/s, no cached source/destination
# priming. The real service later populates its own cgroup's cache normally.
import mmap
import os
import sys
import time

src, dst = sys.argv[1:]
block = 1024 * 1024
rate = 4 * block
start = time.monotonic()
fd_in = os.open(src, os.O_RDONLY | os.O_DIRECT)
try:
    size = os.fstat(fd_in).st_size
    if not size or size % 4096:
        raise ValueError('fixture size is not a positive multiple of 4096')
    fd_out = os.open(dst, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_DIRECT, 0o600)
    try:
        copied = 0
        with mmap.mmap(-1, block) as buf:
            while True:
                n = os.readv(fd_in, [buf])
                if not n:
                    break
                if n % 4096:
                    raise ValueError('unaligned direct read')
                view = memoryview(buf)[:n]
                try:
                    offset = 0
                    while offset < n:
                        w = os.write(fd_out, view[offset:])
                        if w <= 0 or w % 4096:
                            raise OSError('unaligned or short direct write')
                        offset += w
                finally:
                    view.release()
                copied += n
                if copied % (16 * block) == 0:
                    os.fsync(fd_out)
                time.sleep(max(0, copied / rate - (time.monotonic() - start)))
        os.fsync(fd_out)
        if copied != size:
            raise OSError('direct-copy length mismatch')
    finally:
        os.close(fd_out)
finally:
    os.close(fd_in)
print('direct copy bytes', copied, 'seconds', round(time.monotonic() - start, 2), flush=True)

# Preparation must be quiet before service startup; watchdog remains active.
quiet = 0
for _ in range(180):
    def full(kind):
        with open('/proc/pressure/' + kind) as f:
            line = next(line for line in f if line.startswith('full '))
        return float(line.split()[1].split('=')[1])
    quiet = quiet + 1 if full('io') < 0.5 and full('memory') < 0.5 else 0
    if quiet >= 10:
        print('direct-copy pressure settled', flush=True)
        break
    time.sleep(1)
else:
    raise RuntimeError('preparation pressure did not settle')
