"""Fixed Shanghai calendar dates for capacity fixtures, never host-local dates."""
from datetime import datetime
from zoneinfo import ZoneInfo

BUSINESS_TIMEZONE = ZoneInfo('Asia/Shanghai')


def business_day(instant=None):
    if instant is None:
        instant = datetime.now(BUSINESS_TIMEZONE)
    if instant.tzinfo is None:
        raise ValueError('business day requires an aware instant')
    return instant.astimezone(BUSINESS_TIMEZONE).date().isoformat()
