"""Tests for Snapchat Ads helpers."""

import pytest

from ingestr.src.snapchat_ads.helpers import (
    parse_stats_table,
    parse_timeseries_stats,
    parse_total_stats,
)


class TestParseStatsTable:
    """Test parse_stats_table function.

    Table format: <resource-name>:<dimension-like-values>:<metrics>
    """

    def test_with_breakdown(self):
        """Test stats table parsing with breakdown parameter."""
        result = parse_stats_table("campaigns_stats:campaign,DAY:impressions,spend")
        assert result.resource_name == "campaigns_stats"
        assert result.granularity == "DAY"
        assert result.breakdown == "campaign"
        assert result.fields == "impressions,spend"
        assert result.dimension is None
        assert result.pivot is None

    def test_without_breakdown(self):
        """Test stats table parsing without breakdown parameter."""
        result = parse_stats_table("campaigns_stats:DAY:impressions,swipes,spend")
        assert result.resource_name == "campaigns_stats"
        assert result.granularity == "DAY"
        assert result.fields == "impressions,swipes,spend"
        assert result.breakdown is None

    def test_lifetime_granularity(self):
        """Test stats table parsing with LIFETIME granularity."""
        result = parse_stats_table("ads_stats:ad,LIFETIME:impressions")
        assert result.resource_name == "ads_stats"
        assert result.granularity == "LIFETIME"
        assert result.breakdown == "ad"
        assert result.fields == "impressions"

    def test_missing_granularity(self):
        """Test that missing granularity raises ValueError."""
        with pytest.raises(ValueError, match="Granularity is required"):
            parse_stats_table("campaigns_stats:campaign:impressions,spend")

    def test_order_independent(self):
        """Test that dimension-like values can be in any order."""
        # DAY before breakdown
        result1 = parse_stats_table("campaigns_stats:DAY,campaign:impressions")
        assert result1.granularity == "DAY"
        assert result1.breakdown == "campaign"

        # breakdown before DAY
        result2 = parse_stats_table("campaigns_stats:campaign,DAY:impressions")
        assert result2.granularity == "DAY"
        assert result2.breakdown == "campaign"

    def test_with_dimension_and_pivot(self):
        """Test stats table parsing with dimension and pivot parameters."""
        result = parse_stats_table(
            "campaigns_stats:campaign,DAY,GEO,country:impressions"
        )
        assert result.resource_name == "campaigns_stats"
        assert result.granularity == "DAY"
        assert result.breakdown == "campaign"
        assert result.dimension == "GEO"
        assert result.pivot == "country"
        assert result.fields == "impressions"

    def test_default_fields(self):
        """Test that default fields are used when metrics section is omitted."""
        result = parse_stats_table("campaigns_stats:DAY")
        assert result.resource_name == "campaigns_stats"
        assert result.granularity == "DAY"
        assert result.fields == "impressions,spend"  # default

    def test_unknown_parameter_raises_error(self):
        """Test that unknown parameters raise ValueError."""
        with pytest.raises(ValueError, match="Unknown parameter 'invalid'"):
            parse_stats_table("campaigns_stats:DAY,invalid:impressions")


class TestParseTimeseriesStats:
    """Test parse_timeseries_stats function."""

    def test_without_breakdown(self):
        """Test parsing timeseries stats without breakdown."""
        api_response = {
            "timeseries_stats": [
                {
                    "sub_request_status": "SUCCESS",
                    "timeseries_stat": {
                        "id": "campaign-123",
                        "type": "CAMPAIGN",
                        "timeseries": [
                            {
                                "start_time": "2024-01-01T00:00:00.000Z",
                                "end_time": "2024-01-01T01:00:00.000Z",
                                "stats": {"impressions": 100, "spend": 50},
                            }
                        ],
                    },
                }
            ]
        }

        results = list(parse_timeseries_stats(api_response))
        assert len(results) == 1
        assert results[0]["campaign_id"] == "campaign-123"
        assert results[0]["adsquad_id"] is None
        assert results[0]["ad_id"] is None
        assert results[0]["impressions"] == 100
        assert results[0]["spend"] == 50

    def test_with_ad_breakdown(self):
        """Test parsing timeseries stats with ad breakdown."""
        api_response = {
            "timeseries_stats": [
                {
                    "sub_request_status": "SUCCESS",
                    "timeseries_stat": {
                        "id": "campaign-123",
                        "type": "CAMPAIGN",
                        "breakdown_stats": {
                            "ad": [
                                {
                                    "id": "ad-456",
                                    "timeseries": [
                                        {
                                            "start_time": "2024-01-01T00:00:00.000Z",
                                            "end_time": "2024-01-01T01:00:00.000Z",
                                            "stats": {"impressions": 50, "spend": 25},
                                        }
                                    ],
                                }
                            ]
                        },
                    },
                }
            ]
        }

        results = list(parse_timeseries_stats(api_response))
        assert len(results) == 1
        assert results[0]["campaign_id"] == "campaign-123"
        assert results[0]["ad_id"] == "ad-456"
        assert results[0]["adsquad_id"] is None
        assert results[0]["impressions"] == 50
        assert results[0]["spend"] == 25

    def test_with_adsquad_breakdown(self):
        """Test parsing timeseries stats with adsquad breakdown."""
        api_response = {
            "timeseries_stats": [
                {
                    "sub_request_status": "SUCCESS",
                    "timeseries_stat": {
                        "id": "campaign-123",
                        "type": "CAMPAIGN",
                        "breakdown_stats": {
                            "adsquad": [
                                {
                                    "id": "adsquad-789",
                                    "timeseries": [
                                        {
                                            "start_time": "2024-01-01T00:00:00.000Z",
                                            "end_time": "2024-01-01T01:00:00.000Z",
                                            "stats": {"impressions": 75, "spend": 30},
                                        }
                                    ],
                                }
                            ]
                        },
                    },
                }
            ]
        }

        results = list(parse_timeseries_stats(api_response))
        assert len(results) == 1
        assert results[0]["campaign_id"] == "campaign-123"
        assert results[0]["adsquad_id"] == "adsquad-789"
        assert results[0]["ad_id"] is None
        assert results[0]["impressions"] == 75


class TestParseTotalStats:
    """Test parse_total_stats function."""

    def test_without_breakdown(self):
        """Test parsing total stats without breakdown."""
        api_response = {
            "total_stats": [
                {
                    "sub_request_status": "SUCCESS",
                    "total_stat": {
                        "id": "campaign-123",
                        "type": "CAMPAIGN",
                        "start_time": "2024-01-01T00:00:00.000Z",
                        "end_time": "2024-01-31T23:59:59.999Z",
                        "stats": {"impressions": 1000, "spend": 500},
                    },
                }
            ]
        }

        results = list(parse_total_stats(api_response))
        assert len(results) == 1
        assert results[0]["campaign_id"] == "campaign-123"
        assert results[0]["adsquad_id"] is None
        assert results[0]["ad_id"] is None
        assert results[0]["impressions"] == 1000
        assert results[0]["spend"] == 500

    def test_with_ad_breakdown(self):
        """Test parsing total stats with ad breakdown."""
        api_response = {
            "total_stats": [
                {
                    "sub_request_status": "SUCCESS",
                    "total_stat": {
                        "id": "campaign-123",
                        "type": "CAMPAIGN",
                        "start_time": "2024-01-01T00:00:00.000Z",
                        "end_time": "2024-01-31T23:59:59.999Z",
                        "breakdown_stats": {
                            "ad": [
                                {
                                    "id": "ad-456",
                                    "stats": {"impressions": 500, "spend": 250},
                                }
                            ]
                        },
                    },
                }
            ]
        }

        results = list(parse_total_stats(api_response))
        assert len(results) == 1
        assert results[0]["campaign_id"] == "campaign-123"
        assert results[0]["ad_id"] == "ad-456"
        assert results[0]["adsquad_id"] is None
        assert results[0]["impressions"] == 500
        assert results[0]["spend"] == 250
