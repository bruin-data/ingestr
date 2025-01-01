from linkedin_ads.helpers import flat_structure
def test_flat_structure_linkedin_ads():
    items_daily = [
        {
            "clicks": 0,
            "impressions": 43,
            "pivotValues": [
                "urn:li:sponsoredCampaign:123456",
            ],
            "dateRange": {
                "start": {
                    "month": 12,
                    "day": 10,
                    "year": 2024
                },
                "end": {
                    "month": 12,
                    "day": 10,
                    "year": 2024
                }
            },
            "likes": 0
        }
    ]

    expected_output_daily = [
        {   "clicks": 0,
            "impressions": 43,
            "campaign": "urn:li:sponsoredCampaign:123456",
            "date": "2024-12-10",
            "likes": 0
        }
    ]
    assert flat_structure(items_daily,"campaign","daily") == expected_output_daily

    items_monthly = [
        {
            "clicks": 0,
            "impressions": 43,
            "pivotValues": [
                "urn:li:sponsoredCampaign:123456",
                "urn:li:sponsoredCampaign:7891011",
            ],
            "dateRange": {
                "start": {
                    "month": 12,
                    "day": 10,
                    "year": 2024
                },
                "end": {
                    "month": 12,
                    "day": 30,
                    "year": 2024
                }
            },
            "likes": 0
        }   
    ]
    expected_output_monthly = [
        {   "clicks": 0,
            "impressions": 43,
            "campaign":  "urn:li:sponsoredCampaign:123456, urn:li:sponsoredCampaign:7891011",
            "start_date": "2024-12-10",
            "end_date": "2024-12-30",
            "likes": 0
        }
    ]

    assert flat_structure(items_monthly,"campaign","monthly") == expected_output_monthly
