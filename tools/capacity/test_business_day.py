import os
import time
import unittest
from datetime import datetime
from unittest import mock

from business_day import business_day


class BusinessDayTest(unittest.TestCase):
    def test_shanghai_midnight_independent_of_host_timezone(self):
        for zone in ('UTC', 'America/Los_Angeles', 'Pacific/Auckland'):
            try:
                with mock.patch.dict(os.environ, {'TZ': zone}):
                    time.tzset()
                    for instant, expected in (
                        ('2026-11-15T15:59:59+00:00', '2026-11-15'),
                        ('2026-11-15T16:00:00+00:00', '2026-11-16'),
                        ('2026-11-30T16:00:00+00:00', '2026-12-01'),
                        ('2026-12-31T16:00:00+00:00', '2027-01-01'),
                    ):
                        self.assertEqual(business_day(datetime.fromisoformat(instant)), expected)
            finally:
                time.tzset()

    def test_naive_instant_is_rejected(self):
        with self.assertRaises(ValueError):
            business_day(datetime(2026, 12, 31))
