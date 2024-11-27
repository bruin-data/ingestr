""" Wrappers over different abstract types
"""
from inspect import isawaitable
from typing import Optional

from .bucket import AbstractBucket
from .rate import RateItem


class BucketAsyncWrapper(AbstractBucket):
    """BucketAsyncWrapper is a wrapping over any bucket
    that turns a async/synchronous bucket into an async one
    """

    def __init__(self, bucket: AbstractBucket):
        assert isinstance(bucket, AbstractBucket)
        self.bucket = bucket

    async def put(self, item: RateItem):
        result = self.bucket.put(item)

        while isawaitable(result):
            result = await result

        return result

    async def count(self):
        result = self.bucket.count()

        while isawaitable(result):
            result = await result

        return result

    async def leak(self, current_timestamp: Optional[int] = None) -> int:
        result = self.bucket.leak(current_timestamp)

        while isawaitable(result):
            result = await result

        assert isinstance(result, int)
        return result

    async def flush(self) -> None:
        result = self.bucket.flush()

        while isawaitable(result):
            result = await result

        return None

    async def peek(self, index: int) -> Optional[RateItem]:
        item = self.bucket.peek(index)

        while isawaitable(item):
            item = await item

        assert item is None or isinstance(item, RateItem)
        return item

    async def waiting(self, item: RateItem) -> int:
        wait = super().waiting(item)

        if isawaitable(wait):
            wait = await wait

        assert isinstance(wait, int)
        return wait

    @property
    def failing_rate(self):
        return self.bucket.failing_rate

    @property
    def rates(self):
        return self.bucket.rates
