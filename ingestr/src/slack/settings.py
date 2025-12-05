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

"""Slack source settings and constants"""

from dlt.common import pendulum

DEFAULT_START_DATE = pendulum.datetime(year=2000, month=1, day=1)

SLACK_API_URL = "https://slack.com/api/"

MAX_PAGE_SIZE = 1000

MSG_DATETIME_FIELDS = [
    "ts",
    "thread_ts",
    "latest_reply",
    "blocks.thread_ts",
    "blocks.latest_reply",
    "attachment.thread_ts",
    "attachment.latest_reply",
    "edited.ts",
]

DEFAULT_DATETIME_FIELDS = ["updated", "created"]
