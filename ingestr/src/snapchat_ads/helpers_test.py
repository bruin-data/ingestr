"""Tests for Snapchat Ads helpers."""

import pytest

from ingestr.src.snapchat_ads.helpers import parse_stats_table


class TestParseStatsTable:
    """Test parse_stats_table function."""

    def test_with_breakdown(self):
        """Test stats table parsing with breakdown parameter."""
        result = parse_stats_table("campaigns_stats:campaign,DAY,impressions,spend")
        assert result["resource_name"] == "campaigns_stats"
        assert result["stats_config"]["breakdown"] == "campaign"
        assert result["stats_config"]["granularity"] == "DAY"
        assert result["stats_config"]["fields"] == "impressions,spend"

    def test_without_breakdown(self):
        """Test stats table parsing without breakdown parameter."""
        result = parse_stats_table("campaigns_stats:DAY,impressions,swipes,spend")
        assert result["resource_name"] == "campaigns_stats"
        assert result["stats_config"]["granularity"] == "DAY"
        assert result["stats_config"]["fields"] == "impressions,swipes,spend"
        assert "breakdown" not in result["stats_config"]

    def test_lifetime_granularity(self):
        """Test stats table parsing with LIFETIME granularity."""
        result = parse_stats_table("ads_stats:ad,LIFETIME,impressions")
        assert result["resource_name"] == "ads_stats"
        assert result["stats_config"]["breakdown"] == "ad"
        assert result["stats_config"]["granularity"] == "LIFETIME"
        assert result["stats_config"]["fields"] == "impressions"

    def test_missing_granularity(self):
        """Test that missing granularity raises ValueError."""
        with pytest.raises(ValueError, match="Granularity is required"):
            parse_stats_table("campaigns_stats:impressions,spend")
