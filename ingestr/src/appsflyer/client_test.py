import pendulum

from ingestr.src.appsflyer.client import (
    exclude_metrics_for_date_range,
    standardize_keys,
)


def test_exclude_metrics_for_date_range():
    metrics = [
        "cohort_day_1_revenue_per_user",
        "cohort_day_1_total_revenue_per_user",
        "cohort_day_3_revenue_per_user",
        "cohort_day_3_total_revenue_per_user",
    ]

    from_date = "2024-01-01"
    to_date = "2024-01-11"
    now = "2024-01-12"

    pendulum.travel_to(pendulum.parse(now))

    excluded_metrics = exclude_metrics_for_date_range(metrics, from_date, to_date)
    assert excluded_metrics == [
        "cohort_day_1_revenue_per_user",
        "cohort_day_1_total_revenue_per_user",
        "cohort_day_3_revenue_per_user",
        "cohort_day_3_total_revenue_per_user",
    ]


def test_standardize_keys():
    data = [
        {
            "Key One": 100,
            "Key Two": 1000,
        },
        {
            "Key One": 200,
            "Key Two": 2000,
            "cohort_day_1_revenue_per_user": 200,
        },
    ]

    excluded_metrics = ["Key Three"]

    standardized = standardize_keys(data, excluded_metrics)
    assert standardized == [
        {"key_one": 100, "key_two": 1000, "key_three": None},
        {
            "key_one": 200,
            "key_two": 2000,
            "key_three": None,
            "cohort_day_1_revenue_per_user": 200,
        },
    ]
