""" Implement this class to create
a workable bucket for Limiter to use
"""
import asyncio
import logging
from abc import ABC
from abc import abstractmethod
from collections import defaultdict
from inspect import isawaitable
from inspect import iscoroutine
from threading import Thread
from typing import Awaitable
from typing import Dict
from typing import List
from typing import Optional
from typing import Type
from typing import Union

from .clock import AbstractClock
from .rate import Rate
from .rate import RateItem

logger = logging.getLogger("pyrate_limiter")


class AbstractBucket(ABC):
    """Base bucket interface
    Assumption: len(rates) always > 0
    TODO: allow empty rates
    """

    rates: List[Rate]
    failing_rate: Optional[Rate] = None

    @abstractmethod
    def put(self, item: RateItem) -> Union[bool, Awaitable[bool]]:
        """Put an item (typically the current time) in the bucket
        return true if successful, otherwise false
        """

    @abstractmethod
    def leak(
        self,
        current_timestamp: Optional[int] = None,
    ) -> Union[int, Awaitable[int]]:
        """leaking bucket - removing items that are outdated"""

    @abstractmethod
    def flush(self) -> Union[None, Awaitable[None]]:
        """Flush the whole bucket
        - Must remove `failing-rate` after flushing
        """

    @abstractmethod
    def count(self) -> Union[int, Awaitable[int]]:
        """Count number of items in the bucket"""

    @abstractmethod
    def peek(self, index: int) -> Union[Optional[RateItem], Awaitable[Optional[RateItem]]]:
        """Peek at the rate-item at a specific index in latest-to-earliest order
        NOTE: The reason we cannot peek from the start of the queue(earliest-to-latest) is
        we can't really tell how many outdated items are still in the queue
        """

    def waiting(self, item: RateItem) -> Union[int, Awaitable[int]]:
        """Calculate time until bucket become availabe to consume an item again"""
        if self.failing_rate is None:
            return 0

        assert item.weight > 0, "Item's weight must > 0"

        if item.weight > self.failing_rate.limit:
            return -1

        bound_item = self.peek(self.failing_rate.limit - item.weight)

        if bound_item is None:
            # NOTE: No waiting, bucket is immediately ready
            return 0

        def _calc_waiting(inner_bound_item: RateItem) -> int:
            nonlocal item
            assert self.failing_rate is not None  # NOTE: silence mypy
            lower_time_bound = item.timestamp - self.failing_rate.interval
            upper_time_bound = inner_bound_item.timestamp
            return upper_time_bound - lower_time_bound

        async def _calc_waiting_async() -> int:
            nonlocal item, bound_item

            while isawaitable(bound_item):
                bound_item = await bound_item

            if bound_item is None:
                # NOTE: No waiting, bucket is immediately ready
                return 0

            assert isinstance(bound_item, RateItem)
            return _calc_waiting(bound_item)

        if isawaitable(bound_item):
            return _calc_waiting_async()

        assert isinstance(bound_item, RateItem)
        return _calc_waiting(bound_item)


class Leaker(Thread):
    """Responsible for scheduling buckets' leaking at the background either
    through a daemon task(for sync buckets) or a task using asyncio.Task
    """

    daemon = True
    name = "PyrateLimiter's Leaker"
    sync_buckets: Optional[Dict[int, AbstractBucket]] = None
    async_buckets: Optional[Dict[int, AbstractBucket]] = None
    clocks: Optional[Dict[int, AbstractClock]] = None
    leak_interval: int = 10_000
    aio_leak_task: Optional[asyncio.Task] = None

    def __init__(self, leak_interval: int):
        self.sync_buckets = defaultdict()
        self.async_buckets = defaultdict()
        self.clocks = defaultdict()
        self.leak_interval = leak_interval
        super().__init__()

    def register(self, bucket: AbstractBucket, clock: AbstractClock):
        """Register a new bucket with its associated clock"""
        assert self.sync_buckets is not None
        assert self.clocks is not None
        assert self.async_buckets is not None

        try_leak = bucket.leak(0)
        bucket_id = id(bucket)

        if iscoroutine(try_leak):
            try_leak.close()
            self.async_buckets[bucket_id] = bucket
        else:
            self.sync_buckets[bucket_id] = bucket

        self.clocks[bucket_id] = clock

    def deregister(self, bucket_id: int) -> bool:
        """Deregister a bucket"""
        if self.sync_buckets and bucket_id in self.sync_buckets:
            del self.sync_buckets[bucket_id]
            assert self.clocks
            del self.clocks[bucket_id]
            return True

        if self.async_buckets and bucket_id in self.async_buckets:
            del self.async_buckets[bucket_id]
            assert self.clocks
            del self.clocks[bucket_id]

            if not self.async_buckets and self.aio_leak_task:
                self.aio_leak_task.cancel()
                self.aio_leak_task = None

            return True

        return False

    async def _leak(self, buckets: Dict[int, AbstractBucket]) -> None:
        assert self.clocks

        while buckets:
            for bucket_id, bucket in list(buckets.items()):
                clock = self.clocks[bucket_id]
                now = clock.now()

                while isawaitable(now):
                    now = await now

                assert isinstance(now, int)
                leak = bucket.leak(now)

                while isawaitable(leak):
                    leak = await leak

                assert isinstance(leak, int)

            await asyncio.sleep(self.leak_interval / 1000)

    def leak_async(self):
        if self.async_buckets and not self.aio_leak_task:
            self.aio_leak_task = asyncio.create_task(self._leak(self.async_buckets))

    def run(self) -> None:
        """ Override the original method of Thread
        Not meant to be called directly
        """
        assert self.sync_buckets
        asyncio.run(self._leak(self.sync_buckets))

    def start(self) -> None:
        """ Override the original method of Thread
        Call to run leaking sync buckets
        """
        if self.sync_buckets and not self.is_alive():
            super().start()


class BucketFactory(ABC):
    """Asbtract BucketFactory class.
    It is reserved for user to implement/override this class with
    his own bucket-routing/creating logic
    """

    _leaker: Optional[Leaker] = None
    _leak_interval: int = 10_000

    @property
    def leak_interval(self) -> int:
        """Retrieve leak-interval from inner Leaker task"""
        if not self._leaker:
            return self._leak_interval
        return self._leaker.leak_interval

    @leak_interval.setter
    def leak_interval(self, value: int):
        """Set leak-interval for inner Leaker task"""
        if self._leaker:
            self._leaker.leak_interval = value
        self._leak_interval = value

    @abstractmethod
    def wrap_item(
        self,
        name: str,
        weight: int = 1,
    ) -> Union[RateItem, Awaitable[RateItem]]:
        """Add the current timestamp to the receiving item using any clock backend
        - Turn it into a RateItem
        - Can return either a coroutine or a RateItem instance
        """

    @abstractmethod
    def get(self, item: RateItem) -> Union[AbstractBucket, Awaitable[AbstractBucket]]:
        """Get the corresponding bucket to this item"""

    def create(
        self,
        clock: AbstractClock,
        bucket_class: Type[AbstractBucket],
        *args,
        **kwargs,
    ) -> AbstractBucket:
        """Creating a bucket dynamically"""
        bucket = bucket_class(*args, **kwargs)
        self.schedule_leak(bucket, clock)
        return bucket

    def schedule_leak(self, new_bucket: AbstractBucket, associated_clock: AbstractClock) -> None:
        """Schedule all the buckets' leak, reset bucket's failing rate"""
        assert new_bucket.rates, "Bucket rates are not set"

        if not self._leaker:
            self._leaker = Leaker(self.leak_interval)

        self._leaker.register(new_bucket, associated_clock)
        self._leaker.start()
        self._leaker.leak_async()

    def get_buckets(self) -> List[AbstractBucket]:
        """Iterator over all buckets in the factory
        """
        if not self._leaker:
            return []

        buckets = []

        if self._leaker.sync_buckets:
            for _, bucket in self._leaker.sync_buckets.items():
                buckets.append(bucket)

        if self._leaker.async_buckets:
            for _, bucket in self._leaker.async_buckets.items():
                buckets.append(bucket)

        return buckets

    def dispose(self, bucket: Union[int, AbstractBucket]) -> bool:
        """Delete a bucket from the factory"""
        if isinstance(bucket, AbstractBucket):
            bucket = id(bucket)

        assert isinstance(bucket, int), "not valid bucket id"

        if not self._leaker:
            return False

        return self._leaker.deregister(bucket)
