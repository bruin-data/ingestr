from enum import Enum


class Dimension(Enum):
    campaign = "campaign"
    creative = "creative"
    account = "account"
    member_job_title = "member_job_title"
    member_seniority = "member_seniority"
    member_industry = "member_industry"
    member_company_size = "member_company_size"
    member_company = "member_company"


class TimeGranularity(Enum):
    daily = "DAILY"
    monthly = "MONTHLY"
