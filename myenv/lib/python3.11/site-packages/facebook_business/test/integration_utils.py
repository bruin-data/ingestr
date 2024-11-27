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


import unittest
import warnings
from enum import Enum
try:
    from unittest.mock import patch
except ImportError:
    from mock import patch
from requests.models import Response
from facebook_business.api import FacebookAdsApi, FacebookResponse

class IntegrationTestCase(unittest.TestCase):
    mock_response = None

    def __init__(self, *args, **kwargs):
        super(IntegrationTestCase, self).__init__(*args, **kwargs)
        FacebookAdsApi.init(access_token='access_token', crash_log=False)

    def setUp(self):
        self.patcher = patch('requests.Session.request')
        self.mock_request = self.patcher.start()
        self.mock_response = Response()
        
        # ignore Deprecation warning from SDK which is not the part of our testcase
        warnings.filterwarnings(
            action='ignore',
            category=DeprecationWarning,
        )

    def tearDown(self):
        mock_response = None
        self.patcher.stop()

class StatusCode(Enum):
    SUCCESS = 200
    ERROR = 400
