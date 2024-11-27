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
    python -m facebook_business.test.integration_adset
'''

import unittest
import warnings
import json
from facebook_business.session import FacebookSession
from facebook_business.exceptions import FacebookRequestError
from facebook_business.api import FacebookAdsApi, FacebookRequest, FacebookResponse
from facebook_business.adobjects.adaccount import AdAccount
from facebook_business.adobjects.adset import AdSet
from facebook_business.adobjects.adbidadjustments import AdBidAdjustments
from .integration_utils import *
from .integration_constant import *


class AdSetTestCase(IntegrationTestCase):
    def test_get_ad_set(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode(
                '{'
                '"' + str(FieldName.ACCOUNT_ID) + '":"' + str(TestValue.ACCOUNT_ID) + '",'
                '"' + str(FieldName.ADLABELS) + '":' + str(TestValue.AD_LABEL) + ','
                '"' + str(FieldName.ADSET_SCHEDULE) + '":' + str(TestValue.ADSET_SCHEDULE) + ','
                '"' + str(FieldName.ASSET_FEED_ID) + '":"' + str(TestValue.ASSET_FEED_ID) + '",'
                '"' + str(FieldName.BID_ADJUSTMENTS) + '":' + str(TestValue.BID_ADJUSTMENTS) + ','
                '"' + str(FieldName.BID_AMOUNT) + '":"' + str(TestValue.BID_AMOUNT) + '",'
                '"' + str(FieldName.BILLING_EVENT) + '":"' + str(TestValue.BILLING_EVENT) + '",'
                '"' + str(FieldName.BID_STRATEGY) + '":"' + str(TestValue.BID_STRATEGY) + '",'
                '"' + str(FieldName.BUDGET_REMAINING) + '": "' + str(TestValue.BUDGET_REMAINING) + '",'
                '"' + str(FieldName.CAMPAIGN_ID) + '":"' + str(TestValue.CAMPAIGN_ID) + '",'
                '"' + str(FieldName.CONFIGURED_STATUS) + '": "' + str(TestValue.CONFIGURED_STATUS) + '",'
                '"' + str(FieldName.DATE_FORMAT) + '":"' + str(TestValue.DATE_FORMAT) + '",'
                '"' + str(FieldName.DAILY_MIN_SPEND_TARGET) + '": "' + str(TestValue.DAILY_MIN_SPEND_TARGET) + '",'
                '"' + str(FieldName.EFFECTIVE_STATUS) + '":"' + str(TestValue.EFFECTIVE_STATUS) + '",'
                '"' + str(FieldName.INSTAGRAM_ACTOR_ID) + '":"' + str(TestValue.INSTAGRAM_ACTOR_ID) + '",'
                '"' + str(FieldName.ISSUES_INFO) + '": ' + str(TestValue.ISSUES_INFO) + ','
                '"' + str(FieldName.OPTIMIZATION_GOAL) + '":"' + str(TestValue.OPTIMIZATION_GOAL) + '",'
                '"' + str(FieldName.PACING_TYPE) + '":"' + str(TestValue.PACING_TYPE) + '",'
                '"' + str(FieldName.REVIEW_FEEDBACK) + '":"' + str(TestValue.REVIEW_FEEDBACK) + '",'
                '"' + str(FieldName.TUNE_FOR_CATEGORY) + '":"' + str(TestValue.TUNE_FOR_CATEGORY) + '"'
                '}'
            )

            self.mock_request.return_value = self.mock_response

            fields = [
                FieldName.ACCOUNT_ID,
                FieldName.ADLABELS,
                FieldName.ADSET_SCHEDULE,
                FieldName.ASSET_FEED_ID,
                FieldName.BID_ADJUSTMENTS,
                FieldName.BID_AMOUNT,
                FieldName.BILLING_EVENT,
                FieldName.BID_STRATEGY,
                FieldName.BUDGET_REMAINING,
                FieldName.CAMPAIGN_ID,
                FieldName.CONFIGURED_STATUS,
                FieldName.DATE_FORMAT,
                FieldName.DAILY_MIN_SPEND_TARGET,
                FieldName.EFFECTIVE_STATUS,
                FieldName.INSTAGRAM_ACTOR_ID,
                FieldName.ISSUES_INFO,
                FieldName.OPTIMIZATION_GOAL,
                FieldName.PACING_TYPE,
                FieldName.REVIEW_FEEDBACK,
                FieldName.TUNE_FOR_CATEGORY,
            ]
            params = {}
            print(params.__class__.__name__)
            ad_set = AdSet(TestValue.ADSET_ID).api_get(
                fields=fields,
                params=params,
            )
            
            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(ad_set, AdSet))
            self.assertEqual(ad_set[FieldName.ACCOUNT_ID], TestValue.ACCOUNT_ID)
            self.assertEqual(ad_set[FieldName.ADLABELS], [json.loads(TestValue.AD_LABEL)])
            self.assertEqual(ad_set[FieldName.ADSET_SCHEDULE], [json.loads(TestValue.ADSET_SCHEDULE)])
            self.assertEqual(ad_set[FieldName.ASSET_FEED_ID], TestValue.ASSET_FEED_ID)
            self.assertTrue(isinstance(ad_set[FieldName.BID_ADJUSTMENTS], AdBidAdjustments))
            self.assertEqual(ad_set[FieldName.BID_AMOUNT], TestValue.BID_AMOUNT)
            self.assertEqual(ad_set[FieldName.BILLING_EVENT], TestValue.BILLING_EVENT)
            self.assertEqual(ad_set[FieldName.BID_STRATEGY], TestValue.BID_STRATEGY)
            self.assertEqual(ad_set[FieldName.BUDGET_REMAINING], TestValue.BUDGET_REMAINING)
            self.assertEqual(ad_set[FieldName.CAMPAIGN_ID], TestValue.CAMPAIGN_ID)
            self.assertEqual(ad_set[FieldName.CONFIGURED_STATUS], TestValue.CONFIGURED_STATUS)
            self.assertEqual(ad_set[FieldName.DATE_FORMAT], TestValue.DATE_FORMAT)
            self.assertEqual(ad_set[FieldName.DAILY_MIN_SPEND_TARGET], TestValue.DAILY_MIN_SPEND_TARGET)
            self.assertEqual(ad_set[FieldName.EFFECTIVE_STATUS], TestValue.EFFECTIVE_STATUS)
            self.assertEqual(ad_set[FieldName.INSTAGRAM_ACTOR_ID], TestValue.INSTAGRAM_ACTOR_ID)
            self.assertEqual(ad_set[FieldName.ISSUES_INFO], [json.loads(TestValue.ISSUES_INFO)])
            self.assertEqual(ad_set[FieldName.OPTIMIZATION_GOAL], TestValue.OPTIMIZATION_GOAL)
            self.assertEqual(ad_set[FieldName.PACING_TYPE], [TestValue.PACING_TYPE])
            self.assertEqual(ad_set[FieldName.REVIEW_FEEDBACK], TestValue.REVIEW_FEEDBACK)
            self.assertEqual(ad_set[FieldName.TUNE_FOR_CATEGORY], TestValue.TUNE_FOR_CATEGORY)


    def test_get_ad_set_with_wrong_fields(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = [
                'unexist_field',
            ]
            params = {}
            with self.assertRaises(FacebookRequestError):
                ad_set = AdSet(TestValue.ADSET_ID).api_get(
                    fields=fields,
                    params=params,
                )

            self.assertEqual(len(warning), 1)
            self.assertTrue((issubclass(warning[0].category, UserWarning)))


    def test_create_ad_set(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode('{"' + str(FieldName.ID) + '":"' + str(TestValue.ADSET_ID) + '", "success": "true"}')
            self.mock_request.return_value = self.mock_response

            fields = []
            params = {
                FieldName.ADLABELS: [json.loads(TestValue.AD_LABEL)],
                FieldName.BID_STRATEGY: TestValue.BID_STRATEGY,
                FieldName.BUDGET_REBALANCE_FLAG: False,
                FieldName.BUYING_TYPE: TestValue.BUYING_TYPE,
                FieldName.BILLING_EVENT: TestValue.BILLING_EVENT,
                FieldName.DAILY_BUDGET: TestValue.DAILY_BUDGET,
                FieldName.EXECUTION_OPTIONS: [TestValue.EXECUTION_OPTIONS],
                FieldName.ITERATIVE_SPLIT_TEST_CONFIGS: [json.loads(TestValue.ITERATIVE_SPLIT_TEST_CONFIGS)],
                FieldName.LIFETIME_BUDGET: TestValue.LIFETIME_BUDGET,
                FieldName.NAME: TestValue.NAME,
                FieldName.OBJECTIVE: TestValue.OBJECTIVE,
                FieldName.OPTIMIZATION_GOAL: TestValue.OPTIMIZATION_GOAL,
                FieldName.PACING_TYPE: [TestValue.PACING_TYPE],
                FieldName.PROMOTED_OBJECT: json.loads(TestValue.PROMOTED_OBJECT),
                FieldName.SOURCE_CAMPAIGN_ID: TestValue.CAMPAIGN_ID,
                FieldName.SPECIAL_AD_CATEGORY: TestValue.SPECIAL_AD_CATEGORY,
                FieldName.SPEND_CAP: TestValue.SPEND_CAP,
                FieldName.STATUS: TestValue.STATUS,
                FieldName.TARGETING: json.loads(TestValue.TARGETING),
                FieldName.TOPLINE_ID: TestValue.TOPLINE_ID,
                FieldName.UPSTREAM_EVENTS: json.loads(TestValue.UPSTREAM_EVENTS),
            }

            ad_set = AdAccount(TestValue.ACCOUNT_ID).create_ad_set(
                fields,
                params,
            )
            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(ad_set, AdSet))
            self.assertEqual(ad_set[FieldName.ID], TestValue.ADSET_ID)


    def test_create_ad_set_with_wrong_params(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = []
            params = {
                FieldName.STATUS: 3,
                FieldName.TARGETING: 'wrong_targeting',
            }
            with self.assertRaises(FacebookRequestError):
                ad_set = AdAccount(TestValue.ACCOUNT_ID).create_ad_set(
                    fields,
                    params,
                )

            self.assertEqual(len(warning), 2)
            self.assertTrue(issubclass(warning[-1].category, UserWarning))


if __name__ == '__main__':
    unittest.main()
