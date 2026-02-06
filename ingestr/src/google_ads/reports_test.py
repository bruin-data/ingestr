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

import unittest

from .reports import Report


class TestReportPrimaryKeys(unittest.TestCase):
    def test_empty_dimensions_and_segments(self):
        """Primary keys should return empty list when no dimensions or segments."""
        report = Report(
            resource="campaign",
            dimensions=[],
            metrics=["metrics.clicks"],
            segments=[],
        )
        result = report.primary_keys()
        self.assertEqual(result, [])

    def test_dimensions_only_no_id_or_name(self):
        """Dimensions without .id or .name should be included and converted."""
        report = Report(
            resource="campaign",
            dimensions=["campaign.status", "customer.currency_code"],
            metrics=["metrics.clicks"],
            segments=[],
        )
        result = report.primary_keys()
        self.assertEqual(result, ["campaign_status", "customer_currency_code"])

    def test_dimensions_with_id_fields(self):
        """Dimensions with .id fields should be included and converted."""
        report = Report(
            resource="campaign",
            dimensions=["campaign.id", "customer.id"],
            metrics=["metrics.clicks"],
            segments=[],
        )
        result = report.primary_keys()
        self.assertEqual(result, ["campaign_id", "customer_id"])

    def test_includes_name_and_id_fields(self):
        """Both .id and .name fields should be included."""
        report = Report(
            resource="campaign",
            dimensions=["campaign.id", "campaign.name", "customer.id"],
            metrics=["metrics.clicks"],
            segments=[],
        )
        result = report.primary_keys()
        self.assertEqual(result, ["campaign_id", "campaign_name", "customer_id"])

    def test_keeps_name_when_no_matching_id(self):
        """Name fields should be kept when no matching .id exists."""
        report = Report(
            resource="campaign",
            dimensions=["campaign.name", "customer.id"],
            metrics=["metrics.clicks"],
            segments=[],
        )
        result = report.primary_keys()
        # campaign.name should be kept because campaign.id doesn't exist
        self.assertEqual(result, ["campaign_name", "customer_id"])

    def test_segments_only(self):
        """Segments should be processed the same as dimensions."""
        report = Report(
            resource="campaign",
            dimensions=[],
            metrics=["metrics.clicks"],
            segments=["segments.date", "segments.device"],
        )
        result = report.primary_keys()
        self.assertEqual(result, ["segments_date", "segments_device"])

    def test_dimensions_and_segments_combined(self):
        """Both dimensions and segments should be combined in primary keys."""
        report = Report(
            resource="campaign",
            dimensions=["campaign.id", "customer.id"],
            metrics=["metrics.clicks"],
            segments=["segments.date", "segments.ad_network_type"],
        )
        result = report.primary_keys()
        self.assertEqual(
            result,
            ["campaign_id", "customer_id", "segments_date", "segments_ad_network_type"],
        )

    def test_name_across_dimensions_and_segments(self):
        """All fields from both dimensions and segments should be included."""
        report = Report(
            resource="campaign",
            dimensions=["campaign.id", "campaign.name"],
            metrics=["metrics.clicks"],
            segments=["customer.id", "customer.name"],
        )
        result = report.primary_keys()
        self.assertEqual(result, ["campaign_id", "campaign_name", "customer_id", "customer_name"])

    def test_multiple_name_fields_with_single_id(self):
        """All fields including multiple .name fields should be included."""
        report = Report(
            resource="campaign",
            dimensions=["campaign.id", "campaign.name", "ad_group.name"],
            metrics=["metrics.clicks"],
            segments=[],
        )
        result = report.primary_keys()
        self.assertEqual(result, ["campaign_id", "campaign_name", "ad_group_name"])

    def test_nested_field_id_and_name(self):
        """Nested fields like ad_group_ad.ad.id should be included."""
        report = Report(
            resource="ad_group_ad",
            dimensions=["ad_group_ad.ad.id", "ad_group_ad.ad.name"],
            metrics=["metrics.clicks"],
            segments=[],
        )
        result = report.primary_keys()
        self.assertEqual(result, ["ad_group_ad_ad_id", "ad_group_ad_ad_name"])

    def test_preserves_order(self):
        """Primary keys should preserve the order of dimensions then segments."""
        report = Report(
            resource="campaign",
            dimensions=["customer.id", "campaign.id", "ad_group.id"],
            metrics=["metrics.clicks"],
            segments=["segments.date", "segments.device"],
        )
        result = report.primary_keys()
        self.assertEqual(
            result,
            [
                "customer_id",
                "campaign_id",
                "ad_group_id",
                "segments_date",
                "segments_device",
            ],
        )

    def test_id_in_segment_and_name_in_dimension(self):
        """Both .name in dimensions and .id in segments should be included."""
        report = Report(
            resource="campaign",
            dimensions=["campaign.name"],
            metrics=["metrics.clicks"],
            segments=["campaign.id"],
        )
        result = report.primary_keys()
        self.assertEqual(result, ["campaign_name", "campaign_id"])

    def test_real_world_campaign_report(self):
        """Test with a realistic campaign report configuration."""
        report = Report(
            resource="campaign",
            dimensions=[
                "campaign.id",
                "campaign.name",
                "customer.id",
                "customer.descriptive_name",
            ],
            metrics=[
                "metrics.clicks",
                "metrics.impressions",
                "metrics.cost_micros",
            ],
            segments=["segments.date", "segments.ad_network_type", "segments.device"],
        )
        result = report.primary_keys()
        self.assertEqual(
            result,
            [
                "campaign_id",
                "campaign_name",
                "customer_id",
                "customer_descriptive_name",
                "segments_date",
                "segments_ad_network_type",
                "segments_device",
            ],
        )

    def test_field_to_column_conversion(self):
        """Verify that dots are converted to underscores in column names."""
        report = Report(
            resource="test",
            dimensions=["a.b.c.d"],
            metrics=[],
            segments=[],
        )
        result = report.primary_keys()
        self.assertEqual(result, ["a_b_c_d"])


if __name__ == "__main__":
    unittest.main()
