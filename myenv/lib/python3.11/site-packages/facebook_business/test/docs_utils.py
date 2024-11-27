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

import sys
import unittest
import inspect
import re
from ..objects import *
from ..specs import *
from ..exceptions import *


class DocsDataStore(object):
    _data = {}

    @classmethod
    def set(self, key, value):
        self._data[key] = value
        handle = open(DocsDataStore.get('filename'), 'a')
        handle.write('docs_data#' + key + "\n" + value + "\n\n")
        handle.close()

    @classmethod
    def get(self, key):
        return self._data[key]


linted_classes = []


class DocsTestCase(unittest.TestCase):
    def __init__(self, *args, **kwargs):

        super(DocsTestCase, self).__init__(*args, **kwargs)

        def get_aco_methods():
            sdk_obj = getattr(sys.modules[__name__], 'AbstractCrudObject')
            members = inspect.getmembers(sdk_obj)
            member_names = [m[0] for m in members]
            return member_names

        errors = []
        warnings = []

        sdk_class_name = re.sub(r'DocsTestCase$', '', self.__class__.__name__)

        if sdk_class_name not in linted_classes:
            sdk_obj = getattr(sys.modules[__name__], sdk_class_name)
            sdk_members = inspect.getmembers(sdk_obj, inspect.ismethod)
            sdk_members = [m[0] for m in sdk_members
                           if m[0] not in get_aco_methods() and
                           not m[0].startswith('remote_')]

            members = inspect.getmembers(self, inspect.ismethod)
            members = [m for m in members
                       if (m[0].startswith('test_'))]
            for member in members:
                expected_string = re.sub(r'^test_', '', member[0]) + "("
                sourcelines = inspect.getsourcelines(member[1])[0]
                sourcelines.pop(0)
                source = "".join(sourcelines).strip()
                if expected_string not in source and source != "pass":
                    errors.append(
                        "Error: Expected method call to " + expected_string +
                        ") not used in " + self.__class__.__name__ + "::" +
                        member[0],
                    )

            member_names = [m[0] for m in members]
            for sdk_member in sdk_members:
                if "test_" + sdk_member not in member_names:
                    warnings.append(
                        "Warning: Method defined in SDK not defined in " +
                        "test - " + sdk_class_name + "::" + sdk_member + "()",
                    )

            if len(warnings) > 0:
                print("\n".join(warnings))

            if len(errors) > 0:
                print("\n".join(errors))
                sys.exit()

            linted_classes.append(sdk_class_name)

    def tearDown(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        campaigns = account.get_campaigns()
        for campaign in campaigns:
            campaign.remote_delete()

    def verify(self, obj, output):
        def strip_spacing(content):
            content = str(content)
            content = re.sub(r'\s+', ' ', content)
            content = re.sub(r'\n|\r', '', content)
            return content

        return strip_spacing(obj) == strip_spacing(output)

    def create_campaign(self, counter):
        campaign = Campaign(parent_id=DocsDataStore.get('adaccount_id'))
        campaign['name'] = "Campaign " + str(counter)
        campaign['status'] = "PAUSED"
        campaign.remote_create()
        return campaign

    def create_adset(self, counter, campaign):
        adset = AdSet(parent_id=DocsDataStore.get('adaccount_id'))
        adset['name'] = "Ad Set " + str(counter)
        adset['campaign_id'] = campaign['id']
        adset['daily_budget'] = 1000
        adset['bid_amount'] = 2
        adset['billing_event'] = 'LINK_CLICKS'
        adset['optimization_goal'] = 'LINK_CLICKS'
        adset['status'] = 'PAUSED'
        adset['daily_budget'] = 1000
        adset['targeting'] = {
            'geo_locations': {
                'countries': ['US'],
            },
            'interests': [
                {
                    "id": "6003232518610",
                    "name": "Parenting",
                },
            ],
        }
        adset.remote_create()
        return adset

    def create_ad(self, counter, adset, creative):
        adgroup = Ad(parent_id=DocsDataStore.get('adaccount_id'))
        adgroup['name'] = "Ad " + str(counter)
        adgroup['adset_id'] = adset['id']
        adgroup['creative'] = {'creative_id': creative.get_id()}
        adgroup['status'] = 'PAUSED'
        adgroup.remote_create()
        return adgroup

    def create_creative(self, counter):
        creative = AdCreative(parent_id=DocsDataStore.get('adaccount_id'))
        creative['title'] = "My Creative " + str(counter)
        creative['body'] = "This is my creative's body"
        creative['object_url'] = "https://internet.org"
        creative['image_hash'] = self.create_image()['hash']
        creative.remote_create()
        return creative

    def create_creative_leads(self, counter):
        image_hash = self.create_image()['hash']
        link_data = LinkData()
        link_data[LinkData.Field.message] = 'try it out'
        link_data[LinkData.Field.link] = "www.wikipedia.com"
        link_data[LinkData.Field.caption] = 'Caption'
        link_data[LinkData.Field.image_hash] = image_hash

        object_story_spec = ObjectStorySpec()
        page_id = DocsDataStore.get('page_id')
        object_story_spec[ObjectStorySpec.Field.page_id] = page_id
        object_story_spec[ObjectStorySpec.Field.link_data] = link_data

        creative = AdCreative(parent_id=DocsDataStore.get('adaccount_id'))
        creative[AdCreative.Field.name] = 'Test Creative'
        creative[AdCreative.Field.object_story_spec] = object_story_spec
        creative.remote_create()
        return creative

    def create_image(self):
        image = AdImage(parent_id=DocsDataStore.get('adaccount_id'))
        image['filename'] = './facebook_business/test/misc/image.png'
        image.remote_create()
        return image

    def create_adlabel(self):
        label = AdLabel(parent_id=DocsDataStore.get('adaccount_id'))
        label[AdLabel.Field.name] = 'AdLabel name'
        label.remote_create()
        return label

    def create_custom_audience(self):
        audience = CustomAudience(parent_id=DocsDataStore.get('adaccount_id'))
        audience[CustomAudience.Field.subtype] = CustomAudience.Subtype.custom
        audience[CustomAudience.Field.name] = 'Test Audience'
        audience[CustomAudience.Field.description] = 'Autogen-docs example'
        audience.remote_create()
        return audience

    def create_reach_frequency_prediction(self):
        act_id = DocsDataStore.get('adaccount_id')
        rfp = ReachFrequencyPrediction(parent_id=act_id)
        rfp['frequency_cap'] = 2
        rfp['start_time'] = 1449080260
        rfp['stop_time'] = 1449083860
        rfp['reach'] = 20
        rfp['story_event_type'] = 0
        rfp['prediction_mode'] = 0
        rfp['target_spec'] = {
            'geo_locations': {
                'countries': ['US'],
            },
        }
        rfp.remote_create()
        return rfp

    def create_ads_pixel(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        pixel = account.get_ads_pixels([AdsPixel.Field.code])

        if pixel is None:
            pixel = AdsPixel(parent_id=DocsDataStore.get('adaccount_id'))
            pixel[AdsPixel.Field.name] = unique_name('Test Pixel')
            pixel.remote_create()
        return pixel

    def create_product_catalog(self):
        params = {}
        params['name'] = 'Test Catalog'
        product_catalog = ProductCatalog(
            parent_id=DocsDataStore.get('business_id')
        )
        product_catalog.update(params)
        product_catalog.remote_create()
        return product_catalog

    def create_product_set(self, product_catalog_id):
        params = {}
        params['name'] = 'Test Product Set'
        product_set = ProductSet(parent_id=product_catalog_id)
        product_set.update(params)
        product_set.remote_create()
        return product_set

    def create_product_feed(self, product_catalog_id):
        product_feed = ProductFeed(parent_id=product_catalog_id)
        product_feed[ProductFeed.Field.name] = 'Test Feed'
        product_feed[ProductFeed.Field.schedule] = {
            'interval': 'DAILY',
            'url': 'http://www.example.com/sample_feed.tsv',
            'hour': 22,
        }
        product_feed.remote_create()
        return product_feed

    def store_response(self, obj):
        class_name = re.sub(r'DocsTestCase$', '', self.__class__.__name__)
        method = inspect.stack()[1][3]
        handle = open(DocsDataStore.get('filename'), 'a')
        obj_str = str(obj)
        obj_str = re.sub('<', '&lt;', obj_str)
        obj_str = re.sub('>', '&gt;', obj_str)
        handle.write(class_name + '#' + method + "\n" + obj_str + "\n\n")
        handle.close()


DocsDataStore.set('filename', '/tmp/python_sdk_docs.nlsv')
