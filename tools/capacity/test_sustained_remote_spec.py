import shlex
import unittest

from sustained_remote_spec import RemoteSpec, RATES


class SpecTest(unittest.TestCase):
    def setUp(self):
        self.spec = RemoteSpec('/root/task/new', 'ranking-c54-sus-1.service',
                               'ranking-c54-sus-1-watch.service', '/root/task/bin/ranking',
                               '/root/task/bin/models.json', '/root/task/web-dist')

    def valid_readback(self):
        env = ' '.join(shlex.quote(k+'='+v) for k, v in self.spec.environment.items())
        return '\n'.join(['ActiveState=active', 'CPUQuotaPerSecUSec=2s', 'CPUWeight=50',
                          'MemoryHigh=805306368', 'MemoryMax=1073741824', 'MemorySwapMax=0',
                          'OOMPolicy=stop', 'RuntimeMaxUSec=45min', 'TimeoutStopUSec=10s',
                          'WorkingDirectory='+self.spec.root, 'Description='+self.spec.owner+'-service',
                          'BindsTo='+self.spec.watcher, 'After=network.target '+self.spec.watcher,
                          'Environment='+env])

    def test_search_and_confirmation_unit_keep_original_bounds(self):
        search = self.spec.service_args('search')
        confirmation = self.spec.service_args('confirmation')
        resumed = self.spec.service_args('resumed-search')
        for item in ('CPUQuota=200%', 'CPUWeight=50', 'MemoryHigh=768M', 'MemoryMax=1G',
                     'MemorySwapMax=0', 'OOMPolicy=stop', 'RuntimeMaxSec=45min',
                     'TimeoutStopSec=10s', 'BindsTo='+self.spec.watcher, 'After='+self.spec.watcher):
            self.assertIn(item, search)
            self.assertIn(item, confirmation)
            self.assertIn(item, resumed)
        self.assertIn('direct-paced-copy.py', search[-1])
        self.assertIn('/usr/bin/env -i ', search[-1])
        self.assertNotIn('direct-paced-copy.py', confirmation[-1])
        self.assertNotIn('direct-paced-copy.py', resumed[-1])
        self.assertIn('backlog-resumed-search.jsonl', resumed[-1])
        self.assertIn('test ! -s /root/task/new/data/ranking.sqlite-wal', resumed[-1])
        self.assertIn('test ! -s /root/task/new/data/ranking.sqlite-wal', confirmation[-1])
        watch = self.spec.watch_args()
        self.assertIn('RuntimeMaxSec=2h', watch)
        self.assertIn('MemoryMax=64M', watch)
        self.assertIn('CPUQuota=10%', watch)
        self.assertEqual({k: v for k, v in self.spec.environment.items() if k in RATES}, RATES)
        self.assertNotIn('RANKING_SHUTDOWN_TIMEOUT', self.spec.environment)
        self.assertEqual(self.spec.environment['RANKING_HTTP_ADDR'], '127.0.0.1:4210')

    def test_watch_readback_requires_exact_original_bounds(self):
        raw = '\n'.join(['ActiveState=active', 'RuntimeMaxUSec=2h', 'MemoryMax=67108864',
                         'CPUQuotaPerSecUSec=100ms', 'Description='+self.spec.owner+'-watch'])
        self.assertEqual(self.spec.check_watch_properties(raw), {})
        self.assertIn('RuntimeMaxUSec', self.spec.check_watch_properties(raw.replace('RuntimeMaxUSec=2h', 'RuntimeMaxUSec=3h')))
        self.assertIn('MemoryMax', self.spec.check_watch_properties(raw.replace('MemoryMax=67108864', 'MemoryMax=134217728')))

    def test_readback_fails_on_changed_limits_or_unapproved_environment(self):
        good = self.valid_readback()
        self.assertEqual(self.spec.check_service_properties(good), {})
        for old, new in (('MemoryMax=1073741824', 'MemoryMax=2147483648'),
                         ('CPUWeight=50', 'CPUWeight=100'),
                         ('OOMPolicy=stop', 'OOMPolicy=continue'),
                         ('RuntimeMaxUSec=45min', 'RuntimeMaxUSec=1h'),
                         ('BindsTo='+self.spec.watcher, 'BindsTo='),
                         ('RANKING_REPORT_GLOBAL_BURST=100000', 'RANKING_REPORT_GLOBAL_BURST=100001')):
            with self.subTest(change=old):
                self.assertTrue(self.spec.check_service_properties(good.replace(old, new)))
        self.assertIn('Environment', self.spec.check_service_properties(good+' RANKING_SHUTDOWN_TIMEOUT=5s'))
        self.assertIn('Environment', self.spec.check_service_properties(good+' SQLITE_TMPDIR=/unapproved'))

    def test_rejects_unowned_units_or_unsafe_paths(self):
        with self.assertRaises(ValueError):
            RemoteSpec('/root/task', 'sshd.service', self.spec.watcher, '/bin/x', '/bin/y', '/bin/z')
        with self.assertRaises(ValueError):
            RemoteSpec('/root/task/../other', self.spec.service, self.spec.watcher, '/bin/x', '/bin/y', '/bin/z')
        with self.assertRaises(ValueError):
            self.spec.service_args('arbitrary')


if __name__ == '__main__':
    unittest.main()
