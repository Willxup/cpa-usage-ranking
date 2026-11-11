# Derived from the original C54 guard; only task paths and unit name vary.
import datetime,json,os,re,shutil,subprocess,sys,time,urllib.request
from pathlib import Path
if len(sys.argv)!=3:raise SystemExit('usage: watch.py TASK_ROOT TEST_SERVICE.service')
R=Path(sys.argv[1]).resolve()
unit=sys.argv[2]
if not Path(sys.argv[1]).is_absolute() or not re.fullmatch(r'ranking-c54(?:-[a-z0-9-]+)?\.service',unit):raise SystemExit('invalid task root or service unit')
C=Path('/sys/fs/cgroup/system.slice')/unit
original=json.loads((R/'baseline-containers.json').read_text())
if not isinstance(original,dict) or len(original)!=7:raise SystemExit('exactly seven baseline containers required')
last_gen=None; low=0; pressure=0; io_pressure=0

def container_state():
 fmt='{{.Name}} {{.State.Running}} {{.RestartCount}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}'
 out=subprocess.check_output(['docker','inspect','-f',fmt,*original],text=True,timeout=5)
 return {a[0].lstrip('/'):a[1:] for l in out.splitlines() if (a:=l.split())}

def cfile(name):
 try:return (C/name).read_text().strip()
 except OSError:return None

def psi(kind):
 lines=Path('/proc/pressure/'+kind).read_text().splitlines()
 return {l.split()[0]:float(l.split()[1].split('=')[1]) for l in lines}

with (R/'logs/watch.jsonl').open('a',buffering=1) as log:
 for tick in range(7200):
  now=time.time();mem={l.split(':')[0]:int(l.split()[1]) for l in Path('/proc/meminfo').read_text().splitlines()}
  p=psi('memory');ip=psi('io');row={'time':now,'available_mib':mem['MemAvailable']/1024,'disk_free_gib':shutil.disk_usage(R).free/2**30,'memory_psi':p,'io_psi':ip}
  for name in ['memory.current','memory.peak','memory.events','memory.stat','cpu.stat']:
   value=cfile(name)
   if value is not None:
    row[name]=int(value) if name in ['memory.current','memory.peak'] else {l.split()[0]:int(l.split()[1]) for l in value.splitlines()}
  active='memory.current' in row
  low=low+1 if row['available_mib']<512 else 0
  pressure=pressure+1 if p.get('full',0)>1 else 0
  io_pressure=io_pressure+1 if ip.get('full',0)>10 else 0
  reason=None
  if active:
   if low>=5:reason='host available memory below 512MiB'
   elif pressure>=5:reason='sustained host memory pressure'
   elif io_pressure>=10:reason='sustained host IO pressure'
   elif row['disk_free_gib']<10:reason='disk reserve below 10GiB'
   elif row.get('memory.events',{}).get('oom',0)>0:reason='test cgroup OOM'
  if tick%10==0:
   try:
    states=container_state();row['containers']=states
    if any(states.get(n)!=v for n,v in original.items()):reason='existing container state changed'
   except Exception as e:reason='container state monitoring failed';row['monitor_error']=type(e).__name__
  if active:
   try:
    with urllib.request.urlopen('http://127.0.0.1:4210/v1/leaderboards/usage/all',timeout=.5) as response:
     data=json.load(response);gen=data['generated_at'];row['cache_generated_at']=gen
     if gen!=last_gen:
      parsed=datetime.datetime.fromisoformat(gen.replace('Z','+00:00'));row['cache_publish_upper_seconds']=time.time()-parsed.timestamp();last_gen=gen
   except Exception:pass # Startup 503/connection refusal is not a fabricated cache sample.
  if reason:
   row['stop_reason']=reason;log.write(json.dumps(row)+'\n')
   subprocess.run(['systemctl','stop',unit],timeout=15)
   (R/'STOPPED').write_text(reason+'\n');break
  log.write(json.dumps(row)+'\n');time.sleep(max(0,1-(time.time()-now)))
