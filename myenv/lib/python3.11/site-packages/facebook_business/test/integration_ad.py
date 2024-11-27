# Copyright 2014 Facebook, Inc.

# You are hereby granted a non-exclusive, worldwide, royalty-free license to
# use, copy, modify, and distribute this software in source code or binary
# form for use in connection with the web services and APIs provided by
# Facebook.

# As with any software that integrates with the Facebook platform, your use
# of this software is subject to the Facebook Developer Principles and
# Policies [http://developers.facebook.com/policy/]. This copyright notice
# shall be included in all copies or substantial portions of the software.

# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
# THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
# FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
# DEALINGS IN THE SOFTWARE.

'''
Unit tests for the Python Facebook Business SDK.

How to run:
    python -m facebook_business.test.integration_ad
'''

import warnings
import json
from facebook_business.session import FacebookSession
from facebook_business.exceptions import FacebookRequestError
from facebook_business.api import FacebookAdsApi, FacebookRequest, FacebookResponse
from facebook_business.adobjects.adaccount import AdAccount
from facebook_business.adobjects.adcreative import AdCreative
from facebook_business.adobjects.ad import Ad
from facebook_business.adobjects.targeting import Targeting
from facebook_business.adobjects.adsinsights import AdsInsights
from .integration_utils import *
from .integration_constant import *


# ad is renamed from adgroup in API
class AdTestCase(IntegrationTestCase):
    def test_get_ad(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode(
                '{'
                '"' + str(FieldName.ACCOUNT_ID) + '":"' + str(TestValue.ACCOUNT_ID) + '",'
                '"' + str(FieldName.AD_REVIEW_FEEDBACK) + '":' + str(TestValue.AD_REVIEW_FEEDBACK) + ','
                '"' + str(FieldName.ADLABELS) + '":' + str(TestValue.AD_LABEL) + ','
                '"' + str(FieldName.ADSET_ID) + '":"' + str(TestValue.ADSET_ID) + '",'
                '"' + str(FieldName.BID_AMOUNT) + '":"' + str(TestValue.BID_AMOUNT) + '",'
                '"' + str(FieldName.CONFIGURED_STATUS) + '":"' + str(TestValue.CONFIGURED_STATUS) + '",'
                '"' + str(FieldName.CREATIVE) + '":' + str(TestValue.CREATIVE) + ','
                '"' + str(FieldName.EFFECTIVE_STATUS) + '":"' + str(TestValue.EFFECTIVE_STATUS) + '",'
                '"' + str(FieldName.ISSUES_INFO) + '":' + str(TestValue.ISSUES_INFO) + ','
                '"' + str(FieldName.PRIORITY) + '":"' + str(TestValue.PRIORITY) + '",'
                '"' + str(FieldName.TARGETING) + '":' + str(TestValue.TARGETING) + ','
                '"' + str(FieldName.DATE_FORMAT) + '":"' + str(TestValue.DATE_FORMAT) + '",'
                '"' + str(FieldName.EXECUTION_OPTIONS) + '":"' + str(TestValue.EXECUTION_OPTIONS) + '"'
                '}'
            )

            self.mock_request.return_value = self.mock_response

            fields = [
                FieldName.ACCOUNT_ID,
                FieldName.AD_REVIEW_FEEDBACK,
                FieldName.ADLABELS,
                FieldName.ADSET_ID,
                FieldName.BID_AMOUNT,
                FieldName.CONFIGURED_STATUS,
                FieldName.CREATIVE,
                FieldName.EFFECTIVE_STATUS,
                FieldName.ISSUES_INFO,
                FieldName.PRIORITY,
                FieldName.TARGETING,
                FieldName.DATE_FORMAT,
                FieldName.EXECUTION_OPTIONS,
            ]
            params = {}

            ad = Ad(TestValue.AD_ID).api_get(
                fields=fields,
                params=params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(ad, Ad))
            self.assertEqual(ad[FieldName.ACCOUNT_ID], TestValue.ACCOUNT_ID)
            self.assertEqual(ad[FieldName.AD_REVIEW_FEEDBACK], json.loads(TestValue.AD_REVIEW_FEEDBACK))
            self.assertEqual(ad[FieldName.ADLABELS], [json.loads(TestValue.AD_LABEL)])
            self.assertEqual(ad[FieldName.ADSET_ID], TestValue.ADSET_ID)
            self.assertEqual(ad[FieldName.BID_AMOUNT], TestValue.BID_AMOUNT)
            self.assertEqual(ad[FieldName.CONFIGURED_STATUS], TestValue.CONFIGURED_STATUS)
            self.assertTrue(isinstance(ad[FieldName.CREATIVE], AdCreative))
            self.assertEqual(ad[FieldName.EFFECTIVE_STATUS], TestValue.EFFECTIVE_STATUS)
            self.assertEqual(ad[FieldName.ISSUES_INFO], [json.loads(TestValue.ISSUES_INFO)])
            self.assertEqual(ad[FieldName.PRIORITY], TestValue.PRIORITY)
            self.assertTrue(isinstance(ad[FieldName.TARGETING], Targeting))
            self.assertEqual(ad[FieldName.DATE_FORMAT], TestValue.DATE_FORMAT)
            self.assertEqual(ad[FieldName.EXECUTION_OPTIONS], [TestValue.EXECUTION_OPTIONS])


    def test_get_ad_with_wrong_fields(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = [
                'unexist_field',
            ]
            params = {}
            with self.assertRaises(FacebookRequestError):
                ad = Ad(TestValue.AD_ID).api_get(
                    fields=fields,
                    params=params,
                )

            self.assertEqual(len(warning), 1)
            self.assertTrue(issubclass(warning[0].category, UserWarning))


    def test_create_ad(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode('{"' + str(FieldName.ID) + '":"' + str(TestValue.AD_ID) + '", "success": "true"}')
            self.mock_request.return_value = self.mock_response

            fields = []
            params = {
                FieldName.ACCOUNT_ID: TestValue.ACCOUNT_ID,
                FieldName.AD_REVIEW_FEEDBACK: json.loads(TestValue.AD_REVIEW_FEEDBACK),
                FieldName.ADLABELS: [json.loads(TestValue.AD_LABEL)],
                FieldName.ADSET_ID: TestValue.ADSET_ID,
                FieldName.BID_AMOUNT: TestValue.BID_AMOUNT,
                FieldName.CONFIGURED_STATUS: TestValue.CONFIGURED_STATUS,
                FieldName.CREATIVE: json.loads(TestValue.CREATIVE),
                FieldName.EFFECTIVE_STATUS: TestValue.EFFECTIVE_STATUS,
                FieldName.ISSUES_INFO: [json.loads(TestValue.ISSUES_INFO)],
                FieldName.PRIORITY: TestValue.PRIORITY,
                FieldName.TARGETING: json.loads(TestValue.TARGETING),
                FieldName.DATE_FORMAT: TestValue.DATE_FORMAT,
                FieldName.EXECUTION_OPTIONS: [TestValue.EXECUTION_OPTIONS],
            }

            ad = AdAccount(TestValue.ACCOUNT_ID).create_ad(
                fields,
                params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(ad, Ad))
            self.assertEqual(ad[FieldName.ID], TestValue.AD_ID)


    def test_create_ad_with_wrong_params(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = []
            params = {
                'status': 'unexited_status',
                'priority': 'hight',
            }
            with self.assertRaises(FacebookRequestError):
                ad = AdAccount(TestValue.ACCOUNT_ID).create_ad(
                    fields,
                    params,
                )

            self.assertEqual(len(warning), 2)
            self.assertTrue(issubclass(warning[0].category, UserWarning))


    def test_get_ad_creatives(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode(
                '{'
                '"' + str(FieldName.NAME) + '":"' + str(TestValue.NAME) + '",'
                '"' + str(FieldName.ID) + '":"' + str(TestValue.CREATIVE_ID) + '"'
                '}'
            )

            self.mock_request.return_value = self.mock_response

            fields = [
                FieldName.NAME,
                FieldName.ID,
            ]
            params = {}

            creatives = Ad(TestValue.AD_ID).get_ad_creatives(
                fields=fields,
                params=params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(creatives[0], AdCreative))
            self.assertEqual(creatives[0][FieldName.NAME], TestValue.NAME)
            self.assertEqual(creatives[0][FieldName.ID], TestValue.CREATIVE_ID)


    def test_get_ad_creatives_with_wrong_fields(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = [
                'unexist_field',
            ]
            params = {}
            with self.assertRaises(FacebookRequestError):
                creatives = Ad(TestValue.AD_ID).get_ad_creatives(
                    fields=fields,
                    params=params,
                )

            self.assertEqual(len(warning), 1)
            self.assertTrue(issubclass(warning[0].category, UserWarning))


    def test_get_insights(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode(
                '{'
                '"' + str(FieldName.AD_NAME) + '":"' + str(TestValue.NAME) + '",'
                '"' + str(FieldName.AD_ID) + '":"' + str(TestValue.AD_ID) + '",'
                '"' + str(FieldName.DATE_START) + '":"' + str(TestValue.DATE_START) + '",'
                '"' + str(FieldName.DATE_STOP) + '":"' + str(TestValue.DATE_STOP) + '"'
                '}'
            )

            self.mock_request.return_value = self.mock_response

            fields = [
                FieldName.AD_NAME,
            ]
            params = {
                FieldName.ACTION_BREAKDOWNS: [TestValue.ACTION_BREAKDOWNS],
                FieldName.ACTION_ATTRIBUTION_WINDOWS: [TestValue.ACTION_ATTRIBUTION_WINDOWS],
                FieldName.DATE_PRESET: TestValue.DATE_PRESET,
                FieldName.LEVEL: TestValue.LEVEL,
                FieldName.SUMMARY_ACTION_BREAKDOWNS: [TestValue.SUMMARY_ACTION_BREAKDOWNS],
            }

            ad_insights = Ad(TestValue.AD_ID).get_insights(
                fields=fields,
                params=params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(ad_insights[0], AdsInsights))
            self.assertEqual(ad_insights[0][FieldName.AD_NAME], TestValue.NAME)
            self.assertEqual(ad_insights[0][FieldName.DATE_START], TestValue.DATE_START)
            self.assertEqual(ad_insights[0][FieldName.DATE_STOP], TestValue.DATE_STOP)


    def test_get_insights_with_wrong_fields_and_params(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = [
                'unexisted_fields',
            ]
            params = {
                FieldName.ACTION_BREAKDOWNS: 3,
                FieldName.LEVEL: 'wrong_level',
            }
            with self.assertRaises(FacebookRequestError):
                ad_insights = Ad(TestValue.AD_ID).get_insights(
                    fields,
                    params,
                )

            self.assertEqual(len(warning), 3)
            self.assertTrue(issubclass(warning[0].category, UserWarning))


if __name__ == '__main__':
    unittest.main()
