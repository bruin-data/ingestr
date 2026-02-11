from ingestr.src.google_ads import extract_fields

FIELD_PATHS = [
    "customer.id",
    "campaign.id",
    "campaign.name",
    "ad_group.id",
    "ad_group.name",
    "ad_group_ad.resource_name",
    "ad_group_ad.status",
    "ad_group_ad.ad.id",
    "ad_group_ad.ad.type",
    "ad_group_ad.ad.name",
    "ad_group_ad.ad.final_urls",
    "ad_group_ad.ad.responsive_search_ad.path1",
    "ad_group_ad.ad.responsive_search_ad.path2",
    "ad_group_ad.ad.responsive_display_ad.long_headline",
    "ad_group_ad.ad.responsive_display_ad.call_to_action_text",
    "ad_group_ad.ad.responsive_display_ad.format_setting",
    "ad_group_ad.ad.responsive_display_ad.headlines",
    "ad_group_ad.ad.responsive_display_ad.descriptions",
]


def test_responsive_display_ad():
    data = {
        "customer": {
            "resource_name": "customers/1234567890",
            "id": "1234567890",
        },
        "campaign": {
            "resource_name": "customers/1234567890/campaigns/111",
            "name": "Summer Display Campaign",
            "id": "111",
        },
        "ad_group": {
            "resource_name": "customers/1234567890/adGroups/222",
            "id": "222",
            "name": "Display Ad Group",
        },
        "ad_group_ad": {
            "resource_name": "customers/1234567890/adGroupAds/222~333",
            "status": "ENABLED",
            "ad": {
                "type": "RESPONSIVE_DISPLAY_AD",
                "responsive_display_ad": {
                    "headlines": [{"text": "Buy Now"}],
                    "long_headline": {"text": "Great deals on summer products"},
                    "descriptions": [
                        {"text": "Free shipping on all orders"},
                        {"text": "Limited time offer"},
                    ],
                    "format_setting": "ALL_FORMATS",
                },
                "resource_name": "customers/1234567890/ads/333",
                "id": "333",
                "final_urls": ["https://example.com/summer"],
            },
        },
    }

    result = extract_fields(data, FIELD_PATHS)

    assert result["customer_id"] == "1234567890"
    assert result["campaign_id"] == "111"
    assert result["campaign_name"] == "Summer Display Campaign"
    assert result["ad_group_id"] == "222"
    assert result["ad_group_name"] == "Display Ad Group"
    assert (
        result["ad_group_ad_resource_name"] == "customers/1234567890/adGroupAds/222~333"
    )
    assert result["ad_group_ad_status"] == "ENABLED"
    assert result["ad_group_ad_ad_id"] == "333"
    assert result["ad_group_ad_ad_type"] == "RESPONSIVE_DISPLAY_AD"
    assert result["ad_group_ad_ad_final_urls"] == ["https://example.com/summer"]
    assert result["ad_group_ad_ad_responsive_display_ad_headlines"] == [
        {"text": "Buy Now"}
    ]
    assert result["ad_group_ad_ad_responsive_display_ad_long_headline"] == {
        "text": "Great deals on summer products"
    }
    assert result["ad_group_ad_ad_responsive_display_ad_descriptions"] == [
        {"text": "Free shipping on all orders"},
        {"text": "Limited time offer"},
    ]
    assert (
        result["ad_group_ad_ad_responsive_display_ad_format_setting"] == "ALL_FORMATS"
    )

    assert "ad_group_ad_ad_responsive_search_ad_path1" not in result
    assert "ad_group_ad_ad_responsive_search_ad_path2" not in result
    assert "ad_group_ad_ad_name" not in result


def test_call_ad():
    data = {
        "customer": {
            "resource_name": "customers/1234567890",
            "id": "1234567890",
        },
        "campaign": {
            "resource_name": "customers/1234567890/campaigns/444",
            "name": "Call Campaign",
            "id": "444",
        },
        "ad_group": {
            "resource_name": "customers/1234567890/adGroups/555",
            "id": "555",
            "name": "Call Ad Group",
        },
        "ad_group_ad": {
            "resource_name": "customers/1234567890/adGroupAds/555~666",
            "status": "PAUSED",
            "ad": {
                "type": "CALL_AD",
                "resource_name": "customers/1234567890/ads/666",
                "id": "666",
                "final_urls": ["https://example.com/contact"],
            },
        },
    }

    result = extract_fields(data, FIELD_PATHS)

    assert result["customer_id"] == "1234567890"
    assert result["campaign_name"] == "Call Campaign"
    assert result["ad_group_ad_status"] == "PAUSED"
    assert result["ad_group_ad_ad_type"] == "CALL_AD"
    assert result["ad_group_ad_ad_id"] == "666"
    assert result["ad_group_ad_ad_final_urls"] == ["https://example.com/contact"]

    assert "ad_group_ad_ad_responsive_display_ad_headlines" not in result
    assert "ad_group_ad_ad_responsive_display_ad_descriptions" not in result
    assert "ad_group_ad_ad_responsive_display_ad_long_headline" not in result
    assert "ad_group_ad_ad_responsive_search_ad_path1" not in result
    assert "ad_group_ad_ad_responsive_search_ad_path2" not in result
    assert "ad_group_ad_ad_name" not in result


def test_responsive_search_ad():
    data = {
        "customer": {
            "resource_name": "customers/1234567890",
            "id": "1234567890",
        },
        "campaign": {
            "resource_name": "customers/1234567890/campaigns/777",
            "name": "Search Campaign",
            "id": "777",
        },
        "ad_group": {
            "resource_name": "customers/1234567890/adGroups/888",
            "id": "888",
            "name": "Search Ad Group",
        },
        "ad_group_ad": {
            "resource_name": "customers/1234567890/adGroupAds/888~999",
            "status": "PAUSED",
            "ad": {
                "type": "RESPONSIVE_SEARCH_AD",
                "responsive_search_ad": {
                    "path1": "deals",
                    "path2": "today",
                },
                "resource_name": "customers/1234567890/ads/999",
                "id": "999",
                "final_urls": ["https://example.com/search"],
            },
        },
    }

    result = extract_fields(data, FIELD_PATHS)

    assert result["customer_id"] == "1234567890"
    assert result["campaign_name"] == "Search Campaign"
    assert result["ad_group_ad_ad_type"] == "RESPONSIVE_SEARCH_AD"
    assert result["ad_group_ad_ad_responsive_search_ad_path1"] == "deals"
    assert result["ad_group_ad_ad_responsive_search_ad_path2"] == "today"
    assert result["ad_group_ad_ad_final_urls"] == ["https://example.com/search"]

    assert "ad_group_ad_ad_responsive_display_ad_headlines" not in result
    assert "ad_group_ad_ad_responsive_display_ad_descriptions" not in result
    assert "ad_group_ad_ad_responsive_display_ad_long_headline" not in result
    assert "ad_group_ad_ad_name" not in result
