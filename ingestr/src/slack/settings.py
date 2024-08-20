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
