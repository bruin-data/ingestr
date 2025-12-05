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

from datetime import date, datetime, timezone
from typing import Optional


def date_predicate(column: str, start_date: date, end_date: Optional[date]) -> str:
    """
    Generates a date predicate for the WHERE clause of a
    GAQL query.
    """
    if start_date is None:
        raise ValueError("start_date must be provided")

    if end_date is None:
        end_date = datetime.now(tz=timezone.utc).date()

    clauses = []
    if start_date is not None:
        clauses.append(f"""{column} >= '{start_date.strftime("%Y-%m-%d")}'""")

    if end_date is not None:
        clauses.append(f"""{column} <= '{end_date.strftime("%Y-%m-%d")}'""")

    return " AND ".join(clauses)
