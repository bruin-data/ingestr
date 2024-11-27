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
    python -m facebook_business.test.integration_adcreative
'''

import warnings
import json
from facebook_business.session import FacebookSession
from facebook_business.exceptions import FacebookRequestError
from facebook_business.api import FacebookAdsApi, FacebookRequest, FacebookResponse
from facebook_business.adobjects.adaccount import AdAccount
from facebook_business.adobjects.adcreative import AdCreative
from facebook_business.adobjects.adpreview import AdPreview
from .integration_utils import *
from .integration_constant import *


class AdCreativeTestCase(IntegrationTestCase):
    def test_get_adcreative(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode(
                '{'
                '"' + str(FieldName.ACCOUNT_ID) + '":"' + str(TestValue.ACCOUNT_ID) + '",'
                '"' + str(FieldName.ACTOR_ID) + '":"' + str(TestValue.ACTOR_ID) + '",'
                '"' + str(FieldName.ADLABELS) + '":' + str(TestValue.AD_LABEL) + ','
                '"' + str(FieldName.APPLINK_TREATMENT) + '":"' + str(TestValue.APPLINK_TREATMENT) + '",'
                '"' + str(FieldName.AUTHORIZATION_CATEGORY) + '":"' + str(TestValue.AUTHORIZATION_CATEGORY) + '",'
                '"' + str(FieldName.AUTO_UPDATE) + '":"' + str(TestValue.AUTO_UPDATE) + '",'
                '"' + str(FieldName.BODY) + '":"' + str(TestValue.BODY) + '",'
                '"' + str(FieldName.CALL_TO_ACTION_TYPE) + '":"' + str(TestValue.CALL_TO_ACTION_TYPE) + '",'
                '"' + str(FieldName.CATEGORIZATION_CRITERIA) + '":"' + str(TestValue.CATEGORIZATION_CRITERIA) + '",'
                '"' + str(FieldName.IMAGE_HASH) + '":"' + str(TestValue.IMAGE_HASH) + '",'
                '"' + str(FieldName.TITLE) + '":"' + str(TestValue.TITLE) + '",'
                '"' + str(FieldName.OBJECT_URL) + '":"' + str(TestValue.OBJECT_URL) + '",'
                '"' + str(FieldName.NAME) + '":"' + str(TestValue.NAME) + '"'
                '}'
            )

            self.mock_request.return_value = self.mock_response

            fields = [
                FieldName.ACCOUNT_ID,
                FieldName.ACTOR_ID,
                FieldName.ADLABELS,
                FieldName.APPLINK_TREATMENT,
                FieldName.AUTHORIZATION_CATEGORY,
                FieldName.AUTO_UPDATE,
                FieldName.BODY,
                FieldName.CALL_TO_ACTION_TYPE,
                FieldName.CATEGORIZATION_CRITERIA,
                FieldName.IMAGE_HASH,
                FieldName.TITLE,
                FieldName.OBJECT_URL,
                FieldName.NAME,
            ]
            params = {}

            creative = AdCreative(TestValue.CREATIVE_ID).api_get(
                fields=fields,
                params=params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(creative, AdCreative))
            self.assertEqual(creative[FieldName.ACCOUNT_ID], TestValue.ACCOUNT_ID)
            self.assertEqual(creative[FieldName.ACTOR_ID], TestValue.ACTOR_ID)
            self.assertEqual(creative[FieldName.ADLABELS], [json.loads(TestValue.AD_LABEL)])
            self.assertEqual(creative[FieldName.APPLINK_TREATMENT], TestValue.APPLINK_TREATMENT)
            self.assertEqual(creative[FieldName.AUTHORIZATION_CATEGORY], TestValue.AUTHORIZATION_CATEGORY)
            self.assertEqual(creative[FieldName.BODY], TestValue.BODY)
            self.assertEqual(creative[FieldName.CALL_TO_ACTION_TYPE], TestValue.CALL_TO_ACTION_TYPE)
            self.assertEqual(creative[FieldName.CATEGORIZATION_CRITERIA], TestValue.CATEGORIZATION_CRITERIA)
            self.assertEqual(creative[FieldName.IMAGE_HASH], TestValue.IMAGE_HASH)
            self.assertEqual(creative[FieldName.TITLE], TestValue.TITLE)
            self.assertEqual(creative[FieldName.OBJECT_URL], TestValue.OBJECT_URL)
            self.assertEqual(creative[FieldName.NAME], TestValue.NAME)


    def test_get_ad_creative_with_wrong_fields(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = [
                'unexist_field',
            ]
            params = {}

            with self.assertRaises(FacebookRequestError):
                creative = AdCreative(TestValue.CREATIVE_ID).api_get(
                    fields=fields,
                    params=params,
                )

            self.assertEqual(len(warning), 1)
            self.assertTrue(issubclass(warning[0].category, UserWarning))


    def test_create_ad_creative(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode('{"' + str(FieldName.ID) + '":"' + str(TestValue.CREATIVE_ID) + '", "success": "true"}')
            self.mock_request.return_value = self.mock_response

            fields = []
            params = {
                FieldName.ADLABELS: [json.loads(TestValue.AD_LABEL)],
                FieldName.ACTOR_ID: TestValue.ACTOR_ID,
                FieldName.APPLINK_TREATMENT: TestValue.APPLINK_TREATMENT,
                FieldName.AUTHORIZATION_CATEGORY: TestValue.AUTHORIZATION_CATEGORY,
                FieldName.AUTO_UPDATE: TestValue.AUTO_UPDATE,
                FieldName.BODY: TestValue.BODY,
                FieldName.CALL_TO_ACTION_TYPE: TestValue.CALL_TO_ACTION_TYPE,
                FieldName.CATEGORIZATION_CRITERIA: TestValue.CATEGORIZATION_CRITERIA,
                FieldName.IMAGE_HASH: TestValue.IMAGE_HASH,
                FieldName.TITLE: TestValue.TITLE,
                FieldName.OBJECT_URL: TestValue.OBJECT_URL,
                FieldName.NAME: TestValue.NAME,
            }

            creative = AdAccount(TestValue.ACCOUNT_ID).create_ad_creative(
                fields,
                params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(creative, AdCreative))
            self.assertEqual(creative[FieldName.ID], TestValue.CREATIVE_ID)


    def test_create_ad_creative_with_wrong_params(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = []
            params = {
                'authorization_category': 'unexited_category',
            }
            with self.assertRaises(FacebookRequestError):
                creative = AdAccount(TestValue.ACCOUNT_ID).create_ad_creative(
                    fields,
                    params,
                )


            self.assertEqual(len(warning), 1)
            self.assertTrue(issubclass(warning[-1].category, UserWarning))


    def test_get_previews(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.SUCCESS
            self.mock_response._content = str.encode(
                '{'
                '"' + str(FieldName.BODY) + '":"' + str(TestValue.BODY) + '"'
                '}'
            )

            self.mock_request.return_value = self.mock_response

            fields = []
            params = {
                FieldName.AD_FORMAT: TestValue.AD_FORMAT,
                FieldName.DYNAMIC_ASSET_LABEL: TestValue.DYNAMIC_ASSET_LABEL,
                FieldName.DYNAMIC_CREATIVE_SPEC: json.loads(TestValue.DYNAMIC_CREATIVE_SPEC),
                FieldName.HEIGHT: TestValue.HEIGHT,
                FieldName.WIDTH: TestValue.WIDTH,
                FieldName.RENDER_TYPE: TestValue.RENDER_TYPE,
            }

            previews = AdCreative(TestValue.CREATIVE_ID).get_previews(
                fields=fields,
                params=params,
            )

            self.assertEqual(len(warning), 0)
            self.assertTrue(isinstance(previews[0], AdPreview))
            self.assertEqual(previews[0][FieldName.BODY], TestValue.BODY)


    def test_get_previews_with_wrong_params(self):
        with warnings.catch_warnings(record=True) as warning:
            self.mock_response.status_code = StatusCode.ERROR
            self.mock_request.return_value = self.mock_response

            fields = []
            params = {
                'render_type':'wrong_type',
            }
            with self.assertRaises(FacebookRequestError):
                previews = AdCreative(TestValue.CREATIVE_ID).get_previews(
                    fields=fields,
                    params=params,
                )

            self.assertEqual(len(warning), 1)
            self.assertTrue(issubclass(warning[0].category, UserWarning))


if __name__ == '__main__':
    unittest.main()
