from unittest.mock import MagicMock

import pytest

from ingestr.src.reddit_ads.helpers import (
    convert_microcurrency,
    handle_rate_limit,
    parse_custom_table,
)


def test_convert_microcurrency_converts_monetary_fields():
    records = [
        {"spend": 5_000_000, "impressions": 100, "ecpm": 1_500_000, "cpc": 250_000}
    ]
    result = convert_microcurrency(records, ["SPEND", "ECPM", "CPC"])
    assert result[0]["spend"] == 5.0
    assert result[0]["ecpm"] == 1.5
    assert result[0]["cpc"] == 0.25
    assert result[0]["impressions"] == 100


def test_convert_microcurrency_no_monetary_fields():
    records = [{"impressions": 100, "clicks": 10}]
    result = convert_microcurrency(records, ["IMPRESSIONS", "CLICKS"])
    assert result[0]["impressions"] == 100
    assert result[0]["clicks"] == 10


def test_convert_microcurrency_handles_none_values():
    records = [{"spend": None, "impressions": 100}]
    result = convert_microcurrency(records, ["SPEND"])
    assert result[0]["spend"] is None
    assert result[0]["impressions"] == 100


def test_convert_microcurrency_multiple_records():
    records = [
        {"spend": 1_000_000, "clicks": 5},
        {"spend": 2_000_000, "clicks": 10},
    ]
    result = convert_microcurrency(records, ["SPEND"])
    assert result[0]["spend"] == 1.0
    assert result[1]["spend"] == 2.0
    assert result[0]["clicks"] == 5
    assert result[1]["clicks"] == 10


def test_parse_custom_table_basic():
    level, breakdowns, metrics = parse_custom_table(
        "custom:campaign,date:impressions,clicks,spend"
    )
    assert level == "CAMPAIGN"
    assert breakdowns == ["date"]
    assert metrics == ["IMPRESSIONS", "CLICKS", "SPEND"]


def test_parse_custom_table_multiple_breakdowns():
    level, breakdowns, metrics = parse_custom_table(
        "custom:ad_group,date,country:impressions"
    )
    assert level == "AD_GROUP"
    assert breakdowns == ["date", "country"]
    assert metrics == ["IMPRESSIONS"]


def test_parse_custom_table_no_breakdowns():
    level, breakdowns, metrics = parse_custom_table("custom:account:spend,impressions")
    assert level == "ACCOUNT"
    assert breakdowns == []
    assert metrics == ["SPEND", "IMPRESSIONS"]


def test_parse_custom_table_ad_level():
    level, breakdowns, metrics = parse_custom_table("custom:ad,date:impressions,clicks")
    assert level == "AD"
    assert breakdowns == ["date"]
    assert metrics == ["IMPRESSIONS", "CLICKS"]


def test_parse_custom_table_too_many_breakdowns():
    with pytest.raises(ValueError, match="at most 2 breakdowns"):
        parse_custom_table("custom:campaign,date,country,region:impressions")


def test_parse_custom_table_invalid_level():
    with pytest.raises(ValueError, match="Invalid level"):
        parse_custom_table("custom:invalid,date:impressions")


def test_parse_custom_table_normalizes_breakdown_case():
    level, breakdowns, metrics = parse_custom_table(
        "custom:campaign,Date,COUNTRY:impressions"
    )
    assert breakdowns == ["date", "country"]


def test_parse_custom_table_invalid_breakdown():
    with pytest.raises(ValueError, match="Invalid breakdown"):
        parse_custom_table("custom:campaign,invalid_dim:impressions")


def test_parse_custom_table_invalid_metric():
    with pytest.raises(ValueError, match="Invalid metric"):
        parse_custom_table("custom:campaign,date:IMPRESIONS")


def test_parse_custom_table_missing_metrics():
    with pytest.raises(ValueError, match="At least one metric"):
        parse_custom_table("custom:campaign,date:")


def test_parse_custom_table_invalid_format_too_few_parts():
    with pytest.raises(ValueError, match="Invalid custom table format"):
        parse_custom_table("custom:campaign")


def test_parse_custom_table_invalid_format_too_many_parts():
    with pytest.raises(ValueError, match="Invalid custom table format"):
        parse_custom_table("custom:campaign:impressions:extra")


def test_parse_custom_table_preserves_breakdown_order():
    level, breakdowns, metrics = parse_custom_table(
        "custom:campaign,country,date:impressions"
    )
    assert breakdowns == ["country", "date"]


def test_handle_rate_limit_sleeps_when_remaining_low(monkeypatch):
    sleep_calls = []
    monkeypatch.setattr(
        "ingestr.src.reddit_ads.helpers.time.sleep", lambda s: sleep_calls.append(s)
    )

    response = MagicMock()
    response.headers = {"X-RateLimit-Remaining": "1", "X-RateLimit-Reset": "5"}
    handle_rate_limit(response)

    assert len(sleep_calls) == 1
    assert sleep_calls[0] == 5.0


def test_handle_rate_limit_no_sleep_when_remaining_high(monkeypatch):
    sleep_calls = []
    monkeypatch.setattr(
        "ingestr.src.reddit_ads.helpers.time.sleep", lambda s: sleep_calls.append(s)
    )

    response = MagicMock()
    response.headers = {"X-RateLimit-Remaining": "50", "X-RateLimit-Reset": "10"}
    handle_rate_limit(response)

    assert len(sleep_calls) == 0


def test_handle_rate_limit_no_headers(monkeypatch):
    sleep_calls = []
    monkeypatch.setattr(
        "ingestr.src.reddit_ads.helpers.time.sleep", lambda s: sleep_calls.append(s)
    )

    response = MagicMock()
    response.headers = {}
    handle_rate_limit(response)

    assert len(sleep_calls) == 0


def test_handle_rate_limit_zero_remaining(monkeypatch):
    sleep_calls = []
    monkeypatch.setattr(
        "ingestr.src.reddit_ads.helpers.time.sleep", lambda s: sleep_calls.append(s)
    )

    response = MagicMock()
    response.headers = {"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "3"}
    handle_rate_limit(response)

    assert len(sleep_calls) == 1
    assert sleep_calls[0] == 3.0
