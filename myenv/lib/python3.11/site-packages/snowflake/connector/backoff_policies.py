#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import random
from typing import Callable, Iterator

"""This module provides common implementations of backoff policies

All backoff policies must be implemented as generator functions with the behaviour specified below. These generator
functions will be called to create iterators yielding backoff durations.

Args:
    None

Yields:
    int: Next backoff duration in seconds

Example:
    This is an example of a valid backoff policy that always yields a backoff duration of 42 seconds.

    def constant_backoff() -> int:
        while True
            yield 42


Note:
    The functions provided in this module are not backoff policies. They are functions returning backoff policies.
    This is to enable customization of the constants used in backoff computations.
"""

DEFAULT_BACKOFF_FACTOR = 2
DEFAULT_BACKOFF_BASE = 1
DEFAULT_BACKOFF_CAP = 16
DEFAULT_ENABLE_JITTER = True


def mixed_backoff(
    factor: int = DEFAULT_BACKOFF_FACTOR,
    base: int = DEFAULT_BACKOFF_BASE,
    cap: int = DEFAULT_BACKOFF_CAP,
    enable_jitter: bool = DEFAULT_ENABLE_JITTER,
) -> Callable[..., Iterator[int]]:
    """Randomly chooses between exponential and constant backoff. Uses equal jitter.

    Args:
        factor (int): Exponential base for the exponential term.
        base (int): Initial backoff time in seconds. Constant coefficient for the exponential term.
        cap (int): Maximum backoff time in seconds.
        enable_jitter (int): Whether to enable equal jitter on computed durations. For details see
            https://www.awsarchitectureblog.com/2015/03/backoff.html

    Returns:
        Callable: generator function implementing the mixed backoff policy
    """

    def generator():
        cnt = 0
        sleep = base

        yield sleep
        while True:
            cnt += 1

            # equal jitter
            mult_factor = random.choice([-1, 1])
            jitter_amount = 0.5 * sleep * mult_factor if enable_jitter else 0
            sleep = int(
                random.choice(
                    [sleep + jitter_amount, base * factor**cnt + jitter_amount]
                )
            )
            sleep = min(cap, sleep)

            yield sleep

    return generator


def linear_backoff(
    factor: int = DEFAULT_BACKOFF_FACTOR,
    base: int = DEFAULT_BACKOFF_BASE,
    cap: int = DEFAULT_BACKOFF_CAP,
    enable_jitter: bool = DEFAULT_ENABLE_JITTER,
) -> Callable[..., Iterator[int]]:
    """Standard linear backoff. Uses full jitter.

    Args:
        factor (int): Linear increment every iteration.
        base (int): Initial backoff time in seconds.
        cap (int): Maximum backoff time in seconds.
        enable_jitter (int): Whether to enable full jitter on computed durations. For details see
            https://www.awsarchitectureblog.com/2015/03/backoff.html

    Returns:
        Callable: generator function implementing the linear backoff policy
    """

    def generator():
        sleep = base

        yield sleep
        while True:
            sleep += factor
            sleep = min(cap, sleep)

            # full jitter
            yield random.randint(0, sleep) if enable_jitter else sleep

    return generator


def exponential_backoff(
    factor: int = DEFAULT_BACKOFF_FACTOR,
    base: int = DEFAULT_BACKOFF_BASE,
    cap: int = DEFAULT_BACKOFF_CAP,
    enable_jitter: bool = DEFAULT_ENABLE_JITTER,
) -> Callable[..., Iterator[int]]:
    """Standard exponential backoff. Uses full jitter.

    Args:
        factor (int): Exponential base for the exponential term.
        base (int): Initial backoff time in seconds. Constant coefficient for the exponential term.
        cap (int): Maximum backoff time in seconds.
        enable_jitter (int): Whether to enable full jitter on computed durations. For details see
            https://www.awsarchitectureblog.com/2015/03/backoff.html

    Returns:
        Callable: generator function implementing the exponential backoff policy
    """

    def generator():
        sleep = base

        yield sleep
        while True:
            sleep *= factor
            sleep = min(cap, sleep)

            # full jitter
            yield random.randint(0, sleep) if enable_jitter else sleep

    return generator
