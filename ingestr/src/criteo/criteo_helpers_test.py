import pendulum
import pytest

from .criteo_helpers import CriteoAPI


def test_criteo_api_init():
    """Test CriteoAPI initialization"""
    api = CriteoAPI(client_id="test_id", client_secret="test_secret")
    assert api.client_id == "test_id"
    assert api.client_secret == "test_secret"
    assert api.base_url == "https://api.criteo.com/2025-04"


def test_validate_dimensions_and_metrics():
    """Test dimension and metric validation"""
    api = CriteoAPI(client_id="test_id", client_secret="test_secret")

    # Valid dimensions and metrics should pass
    valid_dimensions = ["AdsetId", "Day", "CampaignId"]
    valid_metrics = ["Displays", "Clicks", "AdvertiserCost"]
    assert api.validate_dimensions_and_metrics(valid_dimensions, valid_metrics) is True

    # Invalid dimensions should raise ValueError
    invalid_dimensions = ["AdsetId", "InvalidDimension"]
    with pytest.raises(ValueError, match="Invalid dimensions"):
        api.validate_dimensions_and_metrics(invalid_dimensions, valid_metrics)

    # Invalid metrics should raise ValueError
    invalid_metrics = ["Displays", "InvalidMetric"]
    with pytest.raises(ValueError, match="Invalid metrics"):
        api.validate_dimensions_and_metrics(valid_dimensions, invalid_metrics)


def test_date_validation():
    """Test date range validation in fetch_campaign_statistics"""
    api = CriteoAPI(client_id="test_id", client_secret="test_secret")

    start_date = pendulum.parse("2024-01-01")
    end_date = pendulum.parse("2023-12-31")  # End date before start date

    with pytest.raises(ValueError, match="Invalid date range"):
        list(api.fetch_campaign_statistics(start_date, end_date))


def test_currency_validation():
    """Test currency validation in fetch_campaign_statistics"""
    api = CriteoAPI(client_id="test_id", client_secret="test_secret")

    start_date = pendulum.parse("2024-01-01")
    end_date = pendulum.parse("2024-01-02")

    with pytest.raises(ValueError, match="Unsupported currency"):
        list(api.fetch_campaign_statistics(start_date, end_date, currency="INVALID"))
