from ingestr.src.adjust.adjust_helpers import parse_filters


def test_parse_filters():
    filters_raw = (
        "ad_spend_mode=cost,attribution_source=organic,index=network,campaign,adgroup"
    )
    assert parse_filters(filters_raw) == {
        "ad_spend_mode": "cost",
        "attribution_source": "organic",
        "index": ["network", "campaign", "adgroup"],
    }

    filters_raw = "key1=value1,key2=value2"
    assert parse_filters(filters_raw) == {"key1": "value1", "key2": "value2"}
