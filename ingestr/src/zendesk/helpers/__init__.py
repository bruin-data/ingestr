# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Zendesk source helpers"""

from typing import List, Tuple

from dlt.common import pendulum
from dlt.common.time import timedelta


def make_date_ranges(
    start: pendulum.DateTime, end: pendulum.DateTime, step: timedelta
) -> List[Tuple[pendulum.DateTime, pendulum.DateTime]]:
    """Make tuples of (start, end) date ranges between the given `start` and `end` dates.
    The last range in the resulting list will be capped to the value of `end` argument so it may be smaller than `step`

    Example usage, create 1 week ranges between January 1st 2023 and today:
    >>> make_date_ranges(pendulum.DateTime(2023, 1, 1).as_tz('UTC'), pendulum.today(), timedelta(weeks=1))
    """
    ranges = []
    while True:
        end_time = min(start + step, end)
        ranges.append((start, end_time))
        if end_time == end:
            break
        start = end_time
    return ranges
