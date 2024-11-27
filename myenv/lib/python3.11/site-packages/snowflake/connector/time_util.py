#!/usr/bin/env python
#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import time
from logging import getLogger
from types import TracebackType
from typing import Callable, Iterator

logger = getLogger(__name__)

try:
    from threading import _Timer as Timer
except ImportError:
    from threading import Timer

DEFAULT_MASTER_VALIDITY_IN_SECONDS = 4 * 60 * 60  # seconds


class HeartBeatTimer(Timer):
    """A thread which executes a function every client_session_keep_alive_heartbeat_frequency seconds."""

    def __init__(
        self, client_session_keep_alive_heartbeat_frequency: int, f: Callable
    ) -> None:
        interval = client_session_keep_alive_heartbeat_frequency
        super().__init__(interval, f)
        # Mark this as a daemon thread, so that it won't prevent Python from exiting.
        self.daemon = True

    def run(self) -> None:
        while not self.finished.is_set():
            self.finished.wait(self.interval)
            if not self.finished.is_set():
                try:
                    self.function()
                except Exception as e:
                    logger.debug("failed to heartbeat: %s", e)


def get_time_millis() -> int:
    """Returns the current time in milliseconds."""
    return int(time.time() * 1000)


class TimerContextManager:
    """Context manager class to easily measure execution of a code block.

    Once the context manager finishes, the class should be cast into an int to retrieve
    result.

    Example:

        with TimerContextManager() as measured_time:
            pass
        download_metric = measured_time.get_timing_millis()
    """

    def __init__(self) -> None:
        self._start: int | None = None
        self._end: int | None = None

    def __enter__(self) -> TimerContextManager:
        self._start = get_time_millis()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> None:
        self._end = get_time_millis()

    def get_timing_millis(self) -> int:
        """Get measured timing in milliseconds."""
        if self._start is None or self._end is None:
            raise Exception(
                "Trying to get timing before TimerContextManager has finished"
            )
        return self._end - self._start


class TimeoutBackoffCtx:
    """Base context for handling timeouts and backoffs on retries"""

    def __init__(
        self,
        max_retry_attempts: int | None = None,
        timeout: int | None = None,
        backoff_generator: Iterator | None = None,
    ) -> None:
        self._backoff_generator = backoff_generator

        self._max_retry_attempts = max_retry_attempts
        # in seconds
        self._timeout = timeout

        self._current_retry_count = 0
        self._current_sleep_time = self._advance_backoff()
        self._start_time_millis = None

    @property
    def timeout(self) -> int | None:
        return self._timeout

    @property
    def current_retry_count(self) -> int:
        return int(self._current_retry_count)

    @property
    def current_sleep_time(self) -> int:
        return int(self._current_sleep_time)

    @property
    def remaining_time_millis(self) -> int:
        if self._start_time_millis is None:
            raise TypeError(
                "Start time not recorded in remaining_time_millis, call set_start_time first"
            )

        if self._timeout is None:
            raise TypeError("Timeout is None in remaining_time_millis")

        timeout_millis = self._timeout * 1000
        elapsed_time_millis = get_time_millis() - self._start_time_millis
        return timeout_millis - elapsed_time_millis

    @property
    def should_retry(self) -> bool:
        """Decides whether to retry connection."""
        if self._timeout is not None and self._start_time_millis is None:
            logger.warning(
                "Timeout set in TimeoutBackoffCtx, but start time not recorded"
            )

        timed_out = (
            self.remaining_time_millis < 0 if self._timeout is not None else False
        )
        retry_attempts_exceeded = (
            self._current_retry_count >= self._max_retry_attempts
            if self._max_retry_attempts is not None
            else False
        )
        return not timed_out and not retry_attempts_exceeded

    def _advance_backoff(self) -> int:
        return (
            next(self._backoff_generator) if self._backoff_generator is not None else 0
        )

    def set_start_time(self) -> None:
        self._start_time_millis = get_time_millis()

    def increment(self) -> None:
        """Updates retry count and sleep time for another retry"""
        self._current_retry_count += 1
        self._current_sleep_time = self._advance_backoff()
        logger.debug(f"Update retry count to {self._current_retry_count}")
        logger.debug(f"Update sleep time to {self._current_sleep_time} seconds")
