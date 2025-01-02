import pendulum

from ingestr.src.linkedin_ads.helpers import (
    construct_url,
    find_intervals,
    flat_structure,
)


def test_flat_structure_linkedin_ads():
    items_daily = [
        {
            "clicks": 0,
            "impressions": 43,
            "pivotValues": [
                "urn:li:sponsoredCampaign:123456",
            ],
            "dateRange": {
                "start": {"month": 12, "day": 10, "year": 2024},
                "end": {"month": 12, "day": 10, "year": 2024},
            },
            "likes": 0,
        }
    ]

    expected_output_daily = [
        {
            "clicks": 0,
            "impressions": 43,
            "campaign": "urn:li:sponsoredCampaign:123456",
            "date": pendulum.date(2024, 12, 10),
            "likes": 0,
        }
    ]
    assert flat_structure(items_daily, "CAMPAIGN", "DAILY") == expected_output_daily

    items_monthly = [
        {
            "clicks": 0,
            "impressions": 43,
            "pivotValues": [
                "urn:li:sponsoredCampaign:123456",
                "urn:li:sponsoredCampaign:7891011",
            ],
            "dateRange": {
                "start": {"month": 12, "day": 10, "year": 2024},
                "end": {"month": 12, "day": 30, "year": 2024},
            },
            "likes": 0,
        }
    ]
    expected_output_monthly = [
        {
            "clicks": 0,
            "impressions": 43,
            "campaign": "urn:li:sponsoredCampaign:123456, urn:li:sponsoredCampaign:7891011",
            "start_date": pendulum.Date(2024, 12, 10),
            "end_date": "2024-12-30",
            "likes": 0,
        }
    ]

    assert (
        flat_structure(items_monthly, "CAMPAIGN", "MONTHLY") == expected_output_monthly
    )


def test_find_intervals_linkedin_ads():
    start_date = pendulum.date(2024, 1, 1)
    end_date = pendulum.date(2024, 12, 31)
    assert find_intervals(start_date, end_date, "MONTHLY") == [
        (pendulum.date(2024, 1, 1), pendulum.date(2024, 12, 31))
    ]

    assert find_intervals(
        pendulum.date(2020, 1, 1), pendulum.date(2024, 12, 31), "MONTHLY"
    ) == [
        (pendulum.date(2020, 1, 1), pendulum.date(2022, 1, 1)),
        (pendulum.date(2022, 1, 2), pendulum.date(2024, 1, 2)),
        (pendulum.date(2024, 1, 3), pendulum.date(2024, 12, 31)),
    ]

    assert find_intervals(
        pendulum.date(2022, 2, 1), pendulum.date(2024, 2, 8), "MONTHLY"
    ) == [
        (pendulum.date(2022, 2, 1), pendulum.date(2024, 2, 1)),
        (pendulum.date(2024, 2, 2), pendulum.date(2024, 2, 8)),
    ]

    assert find_intervals(
        pendulum.date(2023, 1, 1), pendulum.date(2024, 12, 20), "DAILY"
    ) == [
        (pendulum.date(2023, 1, 1), pendulum.date(2023, 7, 1)),
        (pendulum.date(2023, 7, 2), pendulum.date(2024, 1, 2)),
        (pendulum.date(2024, 1, 3), pendulum.date(2024, 7, 3)),
        (pendulum.date(2024, 7, 4), pendulum.date(2024, 12, 20)),
    ]


def test_construct_url_linkedin_ads():
    start = pendulum.date(2024, 1, 1)
    end = pendulum.date(2024, 12, 31)
    account_ids = ["123456", "456789"]
    metrics = ["impressions", "clicks", "likes"]
    dimension = "CAMPAIGN"
    time_granularity = "MONTHLY"

    assert (
        construct_url(start, end, account_ids, metrics, dimension, time_granularity)
        == "https://api.linkedin.com/rest/adAnalytics?q=analytics&timeGranularity=MONTHLY&dateRange=(start:(year:2024,month:1,day:1),end:(year:2024,month:12,day:31))&accounts=List(urn%3Ali%3AsponsoredAccount%3A123456,urn%3Ali%3AsponsoredAccount%3A456789)&pivot=CAMPAIGN&fields=impressions,clicks,likes"
    )
    assert (
        construct_url(
            start=pendulum.date(2019, 1, 1),
            end=pendulum.date(2024, 12, 31),
            account_ids=["123456"],
            dimension="CREATIVE",
            metrics=["impressions", "clicks", "likes"],
            time_granularity="MONTHLY",
        )
        == "https://api.linkedin.com/rest/adAnalytics?q=analytics&timeGranularity=MONTHLY&dateRange=(start:(year:2019,month:1,day:1),end:(year:2024,month:12,day:31))&accounts=List(urn%3Ali%3AsponsoredAccount%3A123456)&pivot=CREATIVE&fields=impressions,clicks,likes"
    )
