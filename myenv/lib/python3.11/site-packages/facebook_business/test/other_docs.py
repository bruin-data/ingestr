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
    python -m facebook_business.test.other_docs
'''


import os
import sys
import json
from .docs_utils import *


class AdConversionPixelDocsTestCase(DocsTestCase):
    pass


class ClickTrackingTagDocsTestCase(DocsTestCase):
    pass


class InsightsDocsTestCase(DocsTestCase):
    pass

if __name__ == '__main__':
    with open(DocsDataStore.get('filename'), 'w') as handle:
        handle.write('')
    try:
        config_file = open('./config.json')
    except IOError:
        print("No config file found, skipping docs tests")
        sys.exit()
    config = json.load(config_file)
    config_file.close()

    act_id = "1505766289694659"
    FacebookAdsApi.init(
        config['app_id'],
        config['app_secret'],
        config['access_token'],
        'act_' + str(act_id)
    )
    DocsDataStore.set('adaccount_id', 'act_' + str(act_id))
    DocsDataStore.set('adaccount_id_int', act_id)
    DocsDataStore.set('business_id', '1454288444842444')
    DocsDataStore.set('ca_id', '6026172406640')
    DocsDataStore.set('dpa_catalog_id', '447683242047472')
    DocsDataStore.set('dpa_set_id', '808641022536664')
    DocsDataStore.set('dpa_feed_id', '1577689442497017')
    DocsDataStore.set('dpa_upload_id', '1577690399163588')
    DocsDataStore.set('as_user_id', '358829457619128')
    DocsDataStore.set('pixel_id', '417531085081002')

    unittest.main()
