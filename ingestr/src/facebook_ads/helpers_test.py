# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Tests for Facebook ads source helpers"""

import pytest

from .helpers import parse_insights_table_to_source_kwargs


class TestParseInsightsTableToSourceKwargs:
    """Test cases for parse_insights_table_to_source_kwargs function"""

    def test_basic_facebook_insights(self):
        with pytest.raises(IndexError):
            parse_insights_table_to_source_kwargs("facebook_insights")

    def test_breakdown_name_only(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:ads_insights_age_and_gender"
        )

        expected = {
            "dimensions": ("age", "gender"),
            "fields": ("age", "gender"),
        }
        assert result == expected

    def test_breakdown_name_with_custom_metrics(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:ads_insights_country:impressions,clicks,spend"
        )

        expected = {
            "dimensions": ("country",),
            "fields": ["impressions", "clicks", "spend"],
        }
        assert result == expected

    def test_custom_dimensions_with_metrics(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:age,gender:impressions,clicks"
        )

        expected = {
            "level": None,
            "dimensions": ["age", "gender"],
            "fields": ["impressions", "clicks"],
        }
        assert result == expected

    def test_level_with_dimensions_and_metrics(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:campaign,age,gender:impressions,clicks,spend"
        )

        expected = {
            "level": "campaign",
            "dimensions": ["age", "gender"],
            "fields": ["impressions", "clicks", "spend"],
        }
        assert result == expected

    def test_single_custom_dimension_with_metrics(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:country:impressions,spend"
        )

        expected = {
            "level": None,
            "dimensions": ["country"],
            "fields": ["impressions", "spend"],
        }
        assert result == expected

    def test_multiple_levels_in_dimensions(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:account,campaign,age:impressions"
        )

        expected = {
            "level": "campaign",
            "dimensions": ["account", "age"],
            "fields": ["impressions"],
        }
        assert result == expected

    def test_ad_level_with_dimensions(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:ad,country,age:clicks,impressions"
        )

        expected = {
            "level": "ad",
            "dimensions": ["country", "age"],
            "fields": ["clicks", "impressions"],
        }
        assert result == expected

    def test_adset_level_with_single_dimension(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:adset,gender:spend"
        )

        expected = {
            "level": "adset",
            "dimensions": ["gender"],
            "fields": ["spend"],
        }
        assert result == expected

    def test_account_level_only(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:account:impressions,clicks"
        )

        expected = {
            "level": "account",
            "dimensions": [],
            "fields": ["impressions", "clicks"],
        }
        assert result == expected

    def test_empty_metrics_error(self):
        with pytest.raises(ValueError, match="Custom metrics must be provided"):
            parse_insights_table_to_source_kwargs("facebook_insights:age,gender:")

    def test_whitespace_in_metrics(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:age: impressions , clicks , spend "
        )

        expected = {
            "level": None,
            "dimensions": ["age"],
            "fields": ["impressions", "clicks", "spend"],
        }
        assert result == expected

    def test_predefined_breakdown_ads_insights(self):
        result = parse_insights_table_to_source_kwargs("facebook_insights:ads_insights")

        expected = {
            "dimensions": (),
            "fields": (),
        }
        assert result == expected

    def test_predefined_breakdown_platform_and_device(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:ads_insights_platform_and_device"
        )

        expected = {
            "dimensions": (
                "publisher_platform",
                "platform_position",
                "impression_device",
            ),
            "fields": ("publisher_platform", "platform_position", "impression_device"),
        }
        assert result == expected

    def test_predefined_breakdown_with_custom_metrics(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:ads_insights_region:custom_metric1,custom_metric2"
        )

        expected = {
            "dimensions": ("region",),
            "fields": ["custom_metric1", "custom_metric2"],
        }
        assert result == expected

    def test_complex_level_and_dimensions_scenario(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:account,campaign,adset,country,age,gender:impressions,clicks,spend,reach"
        )

        expected = {
            "level": "adset",
            "dimensions": ["account", "campaign", "country", "age", "gender"],
            "fields": ["impressions", "clicks", "spend", "reach"],
        }
        assert result == expected

    def test_campaign_level_with_campaign_id_field(self):
        result = parse_insights_table_to_source_kwargs(
            "facebook_insights:campaign:campaign_id,clicks"
        )

        expected = {
            "level": "campaign",
            "dimensions": [],
            "fields": ["campaign_id", "clicks"],
        }
        assert result == expected
