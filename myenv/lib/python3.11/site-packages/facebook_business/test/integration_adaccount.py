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
    python -m facebook_business.test.integration_adaccount
'''

import warnings
import json
from facebook_business.session import FacebookSession
from facebook_business.exceptions import FacebookRequestError
from facebook_business.api import FacebookAdsApi, FacebookRequest, FacebookResponse
from facebook_business.adobjects.adaccount import AdAccount
from facebook_business.adobjects.adcreative import AdCreative
from facebook_business.adobjects.ad import Ad
from facebook_business.adobjects.campaign import Campaign
from facebook_business.adobjects.adsinsights import AdsInsights
from facebook_business.adobjects.agencyclientdeclaration import AgencyClientDeclaration
from facebook_business.adobjects.business import Business
from facebook_business.adobjects.extendedcreditinvoicegroup import ExtendedCreditInvoiceGroup
from .integration_utils import *
from .integration_constant import *


class AdAccountTestCase(IntegrationTestCase):
    def test_get_adaccount(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode(
                '{'
                '"' + str(FieldName.ACCOUNT_ID) + '":"' + str(TestValue.ACCOUNT_ID) + '",'
                '"' + str(FieldName.ACCOUNT_STATUS) + '":' + str(TestValue.ACCOUNT_STATUS) + ','
                '"' + str(FieldName.AGE) + '":"' + str(TestValue.AGE) + '",'
                '"' + str(FieldName.AGENCY_CLIENT_DECLARATION) + '":' + str(TestValue.AGENCY_CLIENT_DECLARATION) + ','
                '"' + str(FieldName.AMOUNT_SPENT) + '":"' + str(TestValue.AMOUNT_SPENT) + '",'
                '"' + str(FieldName.BALANCE) + '":"' + str(TestValue.BALANCE) + '",'
                '"' + str(FieldName.BUSINESS) + '":' + str(TestValue.BUSINESS) + ','
                '"' + str(FieldName.BUSINESS_CITY) + '":"' + str(TestValue.BUSINESS_CITY) + '",'
                '"' + str(FieldName.CAPABILITIES) + '":"' + str(TestValue.CAPABILITIES) + '",'
                '"' + str(FieldName.CURRENCY) + '":"' + str(TestValue.CURRENCY) + '",'
                '"' + str(FieldName.DISABLE_REASON) + '":' + str(TestValue.DISABLE_REASON) + ','
                '"' + str(FieldName.EXTENDED_CREDIT_INVOICE_GROUP) + '":' + str(TestValue.EXTENDED_CREDIT_INVOICE_GROUP) + ','
                '"' + str(FieldName.FAILED_DELIVERY_CHECKS) + '":' + str(TestValue.FAILED_DELIVERY_CHECKS) + ','
                '"' + str(FieldName.HAS_PAGE_AUTHORIZED_ADACCOUNT) + '":"' + str(TestValue.HAS_PAGE_AUTHORIZED_ADACCOUNT) + '",'
                '"' + str(FieldName.TOS_ACCEPTED) + '":' + str(TestValue.TOS_ACCEPTED) + ''
                '}'
            )

            self.mock_request.return_value = self.mock_response

            fields = [
                FieldName.ACCOUNT_ID,
                FieldName.ACCOUNT_STATUS,
                FieldName.AGE,
                FieldName.AGENCY_CLIENT_DECLARATION,
                FieldName.BALANCE,
                FieldName.BUSINESS,
                FieldName.BUSINESS_CITY,
                FieldName.CAPABILITIES,
                FieldName.CURRENCY,
                FieldName.DISABLE_REASON,
                FieldName.EXTENDED_CREDIT_INVOICE_GROUP,
                FieldName.FAILED_DELIVERY_CHECKS,
                FieldName.HAS_PAGE_AUTHORIZED_ADACCOUNT,
                FieldName.TOS_ACCEPTED,
            ]
            params = {}

            account = AdAccount(TestValue.ACCOUNT_ID).api_get(
                fields=fields,
                params=params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(account, AdAccount))
            self.assertEqual(account[FieldName.ACCOUNT_ID],TestValue.ACCOUNT_ID)
            self.assertEqual(account[FieldName.ACCOUNT_STATUS], TestValue.ACCOUNT_STATUS)
            self.assertEqual(account[FieldName.AGE], TestValue.AGE)
            self.assertTrue(isinstance(account[FieldName.AGENCY_CLIENT_DECLARATION], AgencyClientDeclaration))
            self.assertEqual(account[FieldName.BALANCE], TestValue.BALANCE)
            self.assertTrue(isinstance(account[FieldName.BUSINESS], Business))
            self.assertEqual(account[FieldName.BUSINESS_CITY], TestValue.BUSINESS_CITY)
            self.assertEqual(account[FieldName.CAPABILITIES], [TestValue.CAPABILITIES])
            self.assertEqual(account[FieldName.CURRENCY], TestValue.CURRENCY)
            self.assertEqual(account[FieldName.DISABLE_REASON], TestValue.DISABLE_REASON)
            self.assertTrue(isinstance(account[FieldName.EXTENDED_CREDIT_INVOICE_GROUP], ExtendedCreditInvoiceGroup))
            self.assertEqual(account[FieldName.FAILED_DELIVERY_CHECKS], [json.loads(TestValue.FAILED_DELIVERY_CHECKS)])
            self.assertEqual(account[FieldName.HAS_PAGE_AUTHORIZED_ADACCOUNT], TestValue.HAS_PAGE_AUTHORIZED_ADACCOUNT)
            self.assertEqual(account[FieldName.TOS_ACCEPTED], json.loads(TestValue.TOS_ACCEPTED))


    def test_get_adaccount_with_wrong_fields(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = [
                'unexist_field',
            ]
            params = {}

            with self.assertRaises(FacebookRequestError):
                account = AdAccount(TestValue.ACCOUNT_ID).api_get(
                    fields=fields,
                    params=params,
                )

            self.assertEqual(len(warning), 1)
            self.assertTrue(issubclass(warning[0].category, UserWarning))


    def test_create_adaccount(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode('{"' + str(FieldName.ID) + '":"' + str(TestValue.ACCOUNT_ID) + '", "success": "true"}')
            self.mock_request.return_value = self.mock_response

            fields = []
            params = {
                FieldName.AD_ACCOUNT_CREATED_FROM_BM_FLAG: TestValue.AD_ACCOUNT_CREATED_FROM_BM_FLAG,
                FieldName.CURRENCY: TestValue.CURRENCY,
                FieldName.INVOICE: TestValue.INVOICE,
                FieldName.NAME: TestValue.NAME,
                FieldName.TIMEZONE_ID: TestValue.TIMEZONE_ID,
            }

            account = Business(TestValue.BUSINESS_ID).create_ad_account(
                fields,
                params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(account, AdAccount))
            self.assertEqual(account[FieldName.ID], TestValue.ACCOUNT_ID)


    def test_create_adaccount_with_wrong_params(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = []
            params = {
                'invoice': 0,
                'timezone_id': 'abc',
            }
            with self.assertRaises(FacebookRequestError):
                account = Business(TestValue.BUSINESS_ID).create_ad_account(
                    fields,
                    params,
                )

            self.assertEqual(len(warning), 2)
            self.assertTrue(issubclass(warning[-1].category, UserWarning))


    def test_get_insights(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode(
                '{'
                '"' + str(FieldName.ID) + '":"' + str(TestValue.ACCOUNT_ID) + '"'
                '}'
            )

            self.mock_request.return_value = self.mock_response

            fields = [
                FieldName.ACCOUNT_ID,
            ]
            params = {
                FieldName.ACTION_ATTRIBUTION_WINDOWS: [TestValue.ACTION_ATTRIBUTION_WINDOWS],
                FieldName.ACTION_BREAKDOWNS: [TestValue.ACTION_BREAKDOWNS],
                FieldName.ACTION_REPORT_TIME: TestValue.ACTION_REPORT_TIME,
                FieldName.DATE_PRESET: TestValue.DATE_PRESET,
                FieldName.LEVEL: TestValue.LEVEL,
                FieldName.SUMMARY_ACTION_BREAKDOWNS: [TestValue.SUMMARY_ACTION_BREAKDOWNS],
            }

            ad_insights = AdAccount(TestValue.ACCOUNT_ID).get_insights(
                fields=fields,
                params=params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(ad_insights[0], AdsInsights))
            self.assertEqual(ad_insights[0][FieldName.ID], TestValue.ACCOUNT_ID)



    def test_get_insights_with_wrong_fields_and_params(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = [
                'unexisted_field',
            ]
            params = {
                FieldName.DATE_PRESET: 'invalide_date',
                FieldName.LEVEL: 'wrong_level',
            }
            with self.assertRaises(FacebookRequestError):
                ad_insights = AdAccount(TestValue.ACCOUNT_ID).get_insights(
                    fields,
                    params,
                )

            self.assertEqual(len(warning), 3)
            self.assertTrue(issubclass(warning[-1].category, UserWarning))


    def test_get_ad_creatives(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode(
                '{'
                '"' + str(FieldName.NAME) + '":"' + str(TestValue.NAME) + '"'
                '}'
            )

            self.mock_request.return_value = self.mock_response

            fields = [
                FieldName.NAME,
            ]
            params = {}

            creatives = AdAccount(TestValue.ACCOUNT_ID).get_ad_creatives(
                fields=fields,
                params=params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(creatives[0], AdCreative))
            self.assertEqual(creatives[0][FieldName.NAME], TestValue.NAME)


    def test_get_ad_creatives_with_wrong_fields(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = [
                'unexist_field',
            ]
            params = {}
            with self.assertRaises(FacebookRequestError):
                creatives = AdAccount(TestValue.ACCOUNT_ID).get_ad_creatives(
                    fields=fields,
                    params=params,
                )

            self.assertEqual(len(warning), 1)
            self.assertTrue(issubclass(warning[0].category, UserWarning))


    def test_get_campaigns(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode(
                '{'
                '"' + str(FieldName.NAME) + '":"' + str(TestValue.NAME) + '"'
                '}'
            )

            self.mock_request.return_value = self.mock_response

            fields = [
                FieldName.NAME,
            ]
            params = {
                FieldName.DATE_PRESET: TestValue.DATE_PRESET,
                FieldName.EFFECTIVE_STATUS: [TestValue.EFFECTIVE_STATUS],
                FieldName.INCLUDE_DRAFTS: TestValue.INCLUDE_DRAFTS,
                FieldName.TIME_RANGE: json.loads(TestValue.TIME_RANGE),
            }

            campaigns = AdAccount(TestValue.ACCOUNT_ID).get_campaigns(
                fields=fields,
                params=params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(campaigns[0], Campaign))
            self.assertEqual(campaigns[0][FieldName.NAME], TestValue.NAME)


    def test_get_campaigns_with_wrong_fields_and_params(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = [
                'unexist_field',
            ]
            params = {
                FieldName.EFFECTIVE_STATUS: 'unexisted_status',
            }

            with self.assertRaises(FacebookRequestError):
                campaigns = AdAccount(TestValue.ACCOUNT_ID).get_campaigns(
                    fields=fields,
                    params=params,
                )

            self.assertEqual(len(warning), 2)
            self.assertTrue(issubclass(warning[0].category, UserWarning))


    def test_get_ads(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode(
                '{'
                '"' + str(FieldName.NAME) + '":"' + str(TestValue.NAME) + '"'
                '}'
            )

            self.mock_request.return_value = self.mock_response

            fields = [
                FieldName.NAME,
            ]
            params = {
                FieldName.DATE_PRESET: TestValue.DATE_PRESET,
                FieldName.EFFECTIVE_STATUS: [TestValue.EFFECTIVE_STATUS],
                FieldName.INCLUDE_DRAFTS: TestValue.INCLUDE_DRAFTS,
                FieldName.TIME_RANGE: json.loads(TestValue.TIME_RANGE),
                FieldName.UPDATED_SINCE: TestValue.UPDATED_SINCE,
            }

            ads = AdAccount(TestValue.ACCOUNT_ID).get_ads(
                fields=fields,
                params=params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(ads[0], Ad))
            self.assertEqual(ads[0][FieldName.NAME], TestValue.NAME)


    def test_get_ads_with_wrong_fields_and_param(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = [
                'unexist_field',
            ]
            params = {
                FieldName.EFFECTIVE_STATUS: 'unexisted_status',
            }
            with self.assertRaises(FacebookRequestError):
                ads = AdAccount(TestValue.ACCOUNT_ID).get_ads(
                    fields=fields,
                    params=params,
                )

            self.assertEqual(len(warning), 2)
            self.assertTrue(issubclass(warning[0].category, UserWarning))


if __name__ == '__main__':
    unittest.main()
