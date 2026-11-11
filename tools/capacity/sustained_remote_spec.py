"""Fixed C54 unit definitions/readback checks; this module starts nothing."""
import re
import shlex
from pathlib import PurePosixPath


RATES = {
    'RANKING_REPORT_GLOBAL_INTERVAL': '10us',
    'RANKING_REPORT_GLOBAL_BURST': '100000',
    'RANKING_REPORT_CLIENT_INTERVAL': '100us',
    'RANKING_REPORT_CLIENT_BURST': '1000',
    'RANKING_REGISTRATION_SOURCE_REQUESTS': '60000',
    'RANKING_REGISTRATION_GLOBAL_REQUESTS': '60000',
    'RANKING_REGISTRATION_HOURLY_CREATIONS': '1000000',
    'RANKING_REGISTRATION_DAILY_CREATIONS': '1000000',
}


def checked_path(value):
    path = PurePosixPath(value)
    if (not path.is_absolute() or '..' in path.parts or str(path) == '/' or
            not re.fullmatch(r'/[A-Za-z0-9_./-]+', str(path))):
        raise ValueError('remote path must be an absolute, shell-safe path without parent traversal')
    return str(path)


def validate_remote_assets(app, catalog, web):
    base = PurePosixPath('/root/.cache/agent-cache/cpa-usage-ranking/stage-i-c54')
    for value, name in ((app, 'ranking'), (catalog, 'models.json'), (web, 'web-dist')):
        path = PurePosixPath(checked_path(value))
        if not path.is_relative_to(base) or path.name != name:
            raise ValueError('remote application assets must be task-scoped '+name)


def checked_unit(value):
    if not re.fullmatch(r'ranking-c54-[a-z0-9-]+\.service', value):
        raise ValueError('unit must have the task-specific C54 prefix')
    return value


class RemoteSpec:
    def __init__(self, root, service, watcher, app, catalog, web):
        self.root, self.app = checked_path(root), checked_path(app)
        self.catalog, self.web = checked_path(catalog), checked_path(web)
        self.service, self.watcher = checked_unit(service), checked_unit(watcher)
        if self.service == self.watcher:
            raise ValueError('service and watch must differ')
        self.db = self.root+'/data/ranking.sqlite'
        self.owner = self.service.removesuffix('.service')
        self.environment = {
            'TMPDIR': self.root+'/tmp', 'GIN_MODE': 'release',
            'RANKING_HTTP_ADDR': '127.0.0.1:4210', 'RANKING_SQLITE_PATH': self.db,
            'RANKING_MODELS_PATH': self.catalog, 'RANKING_WEB_DIR': self.web, **RATES,
        }

    def watch_args(self):
        return ['systemd-run', '--collect', '--unit='+self.watcher.removesuffix('.service'),
                '--service-type=exec', '-p', 'Description='+self.owner+'-watch',
                '-p', 'RuntimeMaxSec=2h', '-p', 'MemoryMax=64M',
                '-p', 'CPUQuota=10%', '/usr/bin/python3', self.root+'/bin/watch.py',
                self.root, self.service]

    def check_watch_properties(self, raw):
        data = dict(line.split('=', 1) for line in raw.splitlines() if '=' in line)
        expected = {'ActiveState': 'active', 'RuntimeMaxUSec': '2h', 'MemoryMax': '67108864',
                    'CPUQuotaPerSecUSec': '100ms', 'Description': self.owner+'-watch'}
        return {k: {'expected': v, 'actual': data.get(k)} for k, v in expected.items() if data.get(k) != v}

    def service_args(self, phase):
        if phase not in ('search', 'resumed-search', 'confirmation'):
            raise ValueError('unknown service phase')
        properties = {
            'CPUQuota': '200%', 'CPUWeight': '50', 'MemoryHigh': '768M',
            'MemoryMax': '1G', 'MemorySwapMax': '0', 'OOMPolicy': 'stop',
            'RuntimeMaxSec': '45min', 'TimeoutStopSec': '10s', 'Description': self.owner+'-service',
            'WorkingDirectory': self.root, 'BindsTo': self.watcher, 'After': self.watcher,
        }
        args = ['systemd-run', '--collect', '--unit='+self.service.removesuffix('.service'),
                '--service-type=exec']
        for key, value in properties.items():
            args.extend(['-p', key+'='+value])
        for key, value in sorted(self.environment.items()):
            args.append('--setenv='+key+'='+value)
        observer = shlex.join(['/usr/bin/python3', self.root+'/bin/backlog-observer.py', self.db,
                               self.root+'/logs/backlog-'+phase+'.jsonl'])
        if phase == 'search':
            # The isolated direct copy is paced and stays in the service cgroup.
            pre = shlex.join(['/usr/bin/python3', self.root+'/bin/direct-paced-copy.py',
                              self.root+'/staging/seed.sqlite', self.db])
            body = 'test ! -e '+shlex.quote(self.db)+' && '+pre
        else:
            # Never rebuild a smaller database or warm it externally.
            body = 'test -f '+shlex.quote(self.db)+' && test ! -s '+shlex.quote(self.db+'-wal')
        # Clear any manager-inherited variables before launching the app;
        # only the explicit paths and approved rate overrides are passed.
        app_command = ['/usr/bin/env', '-i', *[k+'='+v for k, v in sorted(self.environment.items())], self.app]
        body += ' && { '+observer+' & exec '+shlex.join(app_command)+'; }'
        return args+['/bin/bash', '-e', '-c', body]

    def check_service_properties(self, raw):
        data = dict(line.split('=', 1) for line in raw.splitlines() if '=' in line)
        expected = {'ActiveState': 'active', 'CPUQuotaPerSecUSec': '2s', 'CPUWeight': '50',
                    'MemoryHigh': '805306368', 'MemoryMax': '1073741824', 'MemorySwapMax': '0',
                    'OOMPolicy': 'stop', 'RuntimeMaxUSec': '45min', 'TimeoutStopUSec': '10s',
                    'WorkingDirectory': self.root, 'Description': self.owner+'-service'}
        problems = {k: {'expected': v, 'actual': data.get(k)} for k, v in expected.items() if data.get(k) != v}
        for key in ('BindsTo', 'After'):
            if self.watcher not in data.get(key, '').split():
                problems[key] = {'expected_contains': self.watcher, 'actual': data.get(key)}
        try:
            pairs = shlex.split(data.get('Environment', ''))
            if any('=' not in item for item in pairs):
                raise ValueError('malformed Environment')
            actual = dict(item.split('=', 1) for item in pairs)
            if len(actual) != len(pairs) or actual != self.environment:
                problems['Environment'] = {'expected': self.environment, 'actual': actual}
        except ValueError as exc:
            problems['Environment'] = {'error': str(exc)}
        return problems
