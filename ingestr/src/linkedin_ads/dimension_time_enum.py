from enum import Enum


class Dimension(Enum):
    campaign = "campaign"
    creative = "creative"
    account = "account"


class TimeGranularity(Enum):
    daily = "DAILY"
    monthly = "MONTHLY"
