"""A bucket using PostgreSQL as backend
"""
from __future__ import annotations

from contextlib import contextmanager
from typing import Awaitable
from typing import List
from typing import Optional
from typing import TYPE_CHECKING
from typing import Union

from ..abstracts import AbstractBucket
from ..abstracts import Rate
from ..abstracts import RateItem

if TYPE_CHECKING:
    from psycopg_pool import ConnectionPool


class Queries:
    CREATE_BUCKET_TABLE = """
    CREATE TABLE IF NOT EXISTS {table} (
        name VARCHAR,
        weight SMALLINT,
        item_timestamp TIMESTAMP
    )
    """
    CREATE_INDEX_ON_TIMESTAMP = """
    CREATE INDEX IF NOT EXISTS {index} ON {table} (item_timestamp)
    """
    COUNT = """
    SELECT COUNT(*) FROM {table}
    """
    PUT = """
    INSERT INTO {table} (name, weight, item_timestamp) VALUES (%s, %s, TO_TIMESTAMP(%s))
    """
    FLUSH = """
    DELETE FROM {table}
    """
    PEEK = """
    SELECT name, weight, (extract(EPOCH FROM item_timestamp) * 1000) as item_timestamp
    FROM {table}
    ORDER BY item_timestamp DESC
    LIMIT 1
    OFFSET {offset}
    """
    LEAK = """
    DELETE FROM {table} WHERE item_timestamp < TO_TIMESTAMP({timestamp})
    """
    LEAK_COUNT = """
    SELECT COUNT(*) FROM {table} WHERE item_timestamp < TO_TIMESTAMP({timestamp})
    """


class PostgresBucket(AbstractBucket):
    table: str
    pool: ConnectionPool

    def __init__(self, pool: ConnectionPool, table: str, rates: List[Rate]):
        self.table = table.lower()
        self.pool = pool
        assert rates
        self.rates = rates
        self._full_tbl = f'ratelimit___{self.table}'
        self._create_table()

    @contextmanager
    def _get_conn(self):
        with self.pool.connection() as conn:
            yield conn

    def _create_table(self):
        with self._get_conn() as conn:
            conn.execute(Queries.CREATE_BUCKET_TABLE.format(table=self._full_tbl))
            index_name = f'timestampIndex_{self.table}'
            conn.execute(Queries.CREATE_INDEX_ON_TIMESTAMP.format(table=self._full_tbl, index=index_name))

    def put(self, item: RateItem) -> Union[bool, Awaitable[bool]]:
        """Put an item (typically the current time) in the bucket
        return true if successful, otherwise false
        """
        if item.weight == 0:
            return True

        with self._get_conn() as conn:
            for rate in self.rates:
                bound = f"SELECT TO_TIMESTAMP({item.timestamp / 1000}) - INTERVAL '{rate.interval} milliseconds'"
                query = f'SELECT COUNT(*) FROM {self._full_tbl} WHERE item_timestamp >= ({bound})'
                conn = conn.execute(query)
                count = int(conn.fetchone()[0])

                if rate.limit - count < item.weight:
                    self.failing_rate = rate
                    return False

            self.failing_rate = None

            query = Queries.PUT.format(table=self._full_tbl)
            arguments = [(item.name, item.weight, item.timestamp / 1000)] * item.weight
            conn.executemany(query, tuple(arguments))

        return True

    def leak(
        self,
        current_timestamp: Optional[int] = None,
    ) -> Union[int, Awaitable[int]]:
        """leaking bucket - removing items that are outdated"""
        assert current_timestamp is not None, "current-time must be passed on for leak"
        lower_bound = current_timestamp - self.rates[-1].interval

        if lower_bound <= 0:
            return 0

        count = 0

        with self._get_conn() as conn:
            conn = conn.execute(Queries.LEAK_COUNT.format(table=self._full_tbl, timestamp=lower_bound / 1000))
            result = conn.fetchone()

            if result:
                conn.execute(Queries.LEAK.format(table=self._full_tbl, timestamp=lower_bound / 1000))
                count = int(result[0])

        return count

    def flush(self) -> Union[None, Awaitable[None]]:
        """Flush the whole bucket
        - Must remove `failing-rate` after flushing
        """
        with self._get_conn() as conn:
            conn.execute(Queries.FLUSH.format(table=self._full_tbl))
            self.failing_rate = None

        return None

    def count(self) -> Union[int, Awaitable[int]]:
        """Count number of items in the bucket"""
        count = 0
        with self._get_conn() as conn:
            conn = conn.execute(Queries.COUNT.format(table=self._full_tbl))
            result = conn.fetchone()
            assert result
            count = int(result[0])

        return count

    def peek(self, index: int) -> Union[Optional[RateItem], Awaitable[Optional[RateItem]]]:
        """Peek at the rate-item at a specific index in latest-to-earliest order
        NOTE: The reason we cannot peek from the start of the queue(earliest-to-latest) is
        we can't really tell how many outdated items are still in the queue
        """
        item = None

        with self._get_conn() as conn:
            conn = conn.execute(Queries.PEEK.format(table=self._full_tbl, offset=index))
            result = conn.fetchone()
            if result:
                name, weight, timestamp = result[0], int(result[1]), int(result[2])
                item = RateItem(name=name, weight=weight, timestamp=timestamp)

        return item
