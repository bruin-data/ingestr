import datetime
from typing import Optional


def isotime(dt: Optional[datetime.datetime]) -> Optional[str]:
    """
    Converts a datetime object to an iso 8601 formatted string.
    """
    if dt is None:
        return None
    return dt.isoformat()
