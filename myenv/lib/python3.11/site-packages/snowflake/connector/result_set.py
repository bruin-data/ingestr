#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import inspect
from collections import deque
from concurrent.futures import ALL_COMPLETED, Future, wait
from concurrent.futures.thread import ThreadPoolExecutor
from logging import getLogger
from typing import (
    TYPE_CHECKING,
    Any,
    Callable,
    Deque,
    Iterable,
    Iterator,
    Literal,
    overload,
)

from .constants import IterUnit
from .errors import NotSupportedError
from .options import pandas
from .options import pyarrow as pa
from .result_batch import (
    ArrowResultBatch,
    DownloadMetrics,
    JSONResultBatch,
    ResultBatch,
)
from .telemetry import TelemetryField
from .time_util import get_time_millis

if TYPE_CHECKING:  # pragma: no cover
    from pandas import DataFrame
    from pyarrow import Table

    from snowflake.connector.cursor import SnowflakeCursor

logger = getLogger(__name__)


def result_set_iterator(
    first_batch_iter: Iterator[tuple],
    unconsumed_batches: Deque[Future[Iterator[tuple]]],
    unfetched_batches: Deque[ResultBatch],
    final: Callable[[], None],
    prefetch_thread_num: int,
    **kw: Any,
) -> Iterator[dict | Exception] | Iterator[tuple | Exception] | Iterator[Table]:
    """Creates an iterator over some other iterators.

    Very similar to itertools.chain but we need some keywords to be propagated to
    ``_download`` functions later.

    We need this to have ResultChunks fall out of usage so that they can be garbage
    collected.

    Just like ``ResultBatch`` iterator, this might yield an ``Exception`` to allow users
    to continue iterating through the rest of the ``ResultBatch``.
    """
    is_fetch_all = kw.pop("is_fetch_all", False)
    if is_fetch_all:
        with ThreadPoolExecutor(prefetch_thread_num) as pool:
            logger.debug("beginning to schedule result batch downloads")
            yield from first_batch_iter
            while unfetched_batches:
                logger.debug(
                    f"queuing download of result batch id: {unfetched_batches[0].id}"
                )
                future = pool.submit(unfetched_batches.popleft().create_iter, **kw)
                unconsumed_batches.append(future)
            _, _ = wait(unconsumed_batches, return_when=ALL_COMPLETED)
            i = 1
            while unconsumed_batches:
                logger.debug(f"user began consuming result batch {i}")
                yield from unconsumed_batches.popleft().result()
                logger.debug(f"user began consuming result batch {i}")
                i += 1
        final()
    else:
        with ThreadPoolExecutor(prefetch_thread_num) as pool:
            # Fill up window

            logger.debug("beginning to schedule result batch downloads")

            for _ in range(min(prefetch_thread_num, len(unfetched_batches))):
                logger.debug(
                    f"queuing download of result batch id: {unfetched_batches[0].id}"
                )
                unconsumed_batches.append(
                    pool.submit(unfetched_batches.popleft().create_iter, **kw)
                )

            yield from first_batch_iter

            i = 1
            while unconsumed_batches:
                logger.debug(f"user requesting to consume result batch {i}")

                # Submit the next un-fetched batch to the pool
                if unfetched_batches:
                    logger.debug(
                        f"queuing download of result batch id: {unfetched_batches[0].id}"
                    )
                    future = pool.submit(unfetched_batches.popleft().create_iter, **kw)
                    unconsumed_batches.append(future)

                future = unconsumed_batches.popleft()

                # this will raise an exception if one has occurred
                batch_iterator = future.result()

                logger.debug(f"user began consuming result batch {i}")
                yield from batch_iterator
                logger.debug(f"user finished consuming result batch {i}")

                i += 1
        final()


class ResultSet(Iterable[list]):
    """This class retrieves the results of a query with the historical strategy.

    It pre-downloads the first up to 4 ResultChunks (this doesn't include the 1st chunk
    as that is embedded in the response JSON from Snowflake) upon creating an Iterator
    on it.

    It also reports telemetry data about its ``ResultBatch``es once it's done iterating
    through them.

    Currently we do not support mixing multiple ``ResultBatch`` types and having
    different column definitions types per ``ResultBatch``.
    """

    def __init__(
        self,
        cursor: SnowflakeCursor,
        result_chunks: list[JSONResultBatch] | list[ArrowResultBatch],
        prefetch_thread_num: int,
    ) -> None:
        self.batches = result_chunks
        self._cursor = cursor
        self.prefetch_thread_num = prefetch_thread_num

    def _report_metrics(self) -> None:
        """Report all metrics totalled up.

        This includes TIME_CONSUME_LAST_RESULT, TIME_DOWNLOADING_CHUNKS and
        TIME_PARSING_CHUNKS in that order.
        """
        if self._cursor._first_chunk_time is not None:
            time_consume_last_result = (
                get_time_millis() - self._cursor._first_chunk_time
            )
            self._cursor._log_telemetry_job_data(
                TelemetryField.TIME_CONSUME_LAST_RESULT, time_consume_last_result
            )
        metrics = self._get_metrics()
        if DownloadMetrics.download.value in metrics:
            self._cursor._log_telemetry_job_data(
                TelemetryField.TIME_DOWNLOADING_CHUNKS,
                metrics.get(DownloadMetrics.download.value),
            )
        if DownloadMetrics.parse.value in metrics:
            self._cursor._log_telemetry_job_data(
                TelemetryField.TIME_PARSING_CHUNKS,
                metrics.get(DownloadMetrics.parse.value),
            )

    def _finish_iterating(self) -> None:
        """Used for any cleanup after the result set iterator is done."""

        self._report_metrics()

    def _can_create_arrow_iter(self) -> None:
        # For now we don't support mixed ResultSets, so assume first partition's type
        #  represents them all
        head_type = type(self.batches[0])
        if head_type != ArrowResultBatch:
            raise NotSupportedError(
                f"Trying to use arrow fetching on {head_type} which "
                f"is not ArrowResultChunk"
            )

    def _fetch_arrow_batches(
        self,
    ) -> Iterator[Table]:
        """Fetches all the results as Arrow Tables, chunked by Snowflake back-end."""
        self._can_create_arrow_iter()
        return self._create_iter(iter_unit=IterUnit.TABLE_UNIT, structure="arrow")

    @overload
    def _fetch_arrow_all(self, force_return_table: Literal[False]) -> Table | None: ...

    @overload
    def _fetch_arrow_all(self, force_return_table: Literal[True]) -> Table: ...

    def _fetch_arrow_all(self, force_return_table: bool = False) -> Table | None:
        """Fetches a single Arrow Table from all of the ``ResultBatch``."""
        tables = list(self._fetch_arrow_batches())
        if tables:
            return pa.concat_tables(tables)
        else:
            return self.batches[0].to_arrow() if force_return_table else None

    def _fetch_pandas_batches(self, **kwargs) -> Iterator[DataFrame]:
        """Fetches Pandas dataframes in batches, where batch refers to Snowflake Chunk.

        Thus, the batch size (the number of rows in dataframe) is determined by
        Snowflake's back-end.
        """
        self._can_create_arrow_iter()
        return self._create_iter(
            iter_unit=IterUnit.TABLE_UNIT, structure="pandas", **kwargs
        )

    def _fetch_pandas_all(self, **kwargs) -> DataFrame:
        """Fetches a single Pandas dataframe."""
        concat_args = list(inspect.signature(pandas.concat).parameters)
        concat_kwargs = {k: kwargs.pop(k) for k in dict(kwargs) if k in concat_args}
        dataframes = list(self._fetch_pandas_batches(is_fetch_all=True, **kwargs))
        if dataframes:
            return pandas.concat(
                dataframes,
                ignore_index=True,  # Don't keep in result batch indexes
                **concat_kwargs,
            )
        # Empty dataframe
        return self.batches[0].to_pandas(**kwargs)

    def _get_metrics(self) -> dict[str, int]:
        """Sum up all the chunks' metrics and show them together."""
        overall_metrics: dict[str, int] = {}
        for c in self.batches:
            for n, v in c._metrics.items():
                overall_metrics[n] = overall_metrics.get(n, 0) + v
        return overall_metrics

    def __iter__(self) -> Iterator[tuple]:
        """Returns a new iterator through all batches with default values."""
        return self._create_iter()

    def _create_iter(
        self,
        **kwargs,
    ) -> (
        Iterator[dict | Exception]
        | Iterator[tuple | Exception]
        | Iterator[Table]
        | Iterator[DataFrame]
    ):
        """Set up a new iterator through all batches with first 5 chunks downloaded.

        This function is a helper function to ``__iter__`` and it was introduced for the
        cases where we need to propagate some values to later ``_download`` calls.
        """
        # pop is_fetch_all and pass it to result_set_iterator
        is_fetch_all = kwargs.pop("is_fetch_all", False)

        # add connection so that result batches can use sessions
        kwargs["connection"] = self._cursor.connection

        first_batch_iter = self.batches[0].create_iter(**kwargs)

        # Iterator[Tuple] Futures that have not been consumed by the user
        unconsumed_batches: Deque[Future[Iterator[tuple]]] = deque()

        # batches that have not been fetched
        unfetched_batches = deque(self.batches[1:])
        for num, batch in enumerate(unfetched_batches):
            logger.debug(f"result batch {num + 1} has id: {batch.id}")

        return result_set_iterator(
            first_batch_iter,
            unconsumed_batches,
            unfetched_batches,
            self._finish_iterating,
            self.prefetch_thread_num,
            is_fetch_all=is_fetch_all,
            **kwargs,
        )

    def total_row_index(self) -> int:
        """Returns the total rowcount of the ``ResultSet`` ."""
        total = 0
        for p in self.batches:
            total += p.rowcount
        return total
