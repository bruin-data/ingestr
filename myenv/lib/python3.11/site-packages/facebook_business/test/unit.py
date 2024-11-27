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
    python -m facebook_business.test.unit
'''

import unittest
import json
import inspect
import six
import re
import hashlib
from six.moves import urllib
from sys import version_info
from .. import api
from .. import specs
from .. import exceptions
from .. import session
from .. import utils
from facebook_business.adobjects import (abstractcrudobject,
                                   ad,
                                   adaccount,
                                   adcreative,
                                   customaudience,
                                   productcatalog)
from facebook_business.utils import version


class CustomAudienceTestCase(unittest.TestCase):

    def test_format_params(self):
        payload = customaudience.CustomAudience.format_params(
            customaudience.CustomAudience.Schema.email_hash,
            ["  test  ", "test", "..test.."]
        )
        # This is the value of "test" when it's hashed with sha256
        test_hash = \
            "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
        users = payload['payload']['data']
        assert users[0] == test_hash
        assert users[1] == users[0]
        assert users[2] == users[1]

    def test_fail_when_no_app_ids(self):
        def uid_payload():
            customaudience.CustomAudience.format_params(
                customaudience.CustomAudience.Schema.uid,
                ["123123"],
            )
        self.assertRaises(
            exceptions.FacebookBadObjectError,
            uid_payload,
        )

    def test_format_params_pre_hashed(self):
        # This is the value of "test" when it's hashed with sha256
        user = "test"
        test_hash = (hashlib.sha256(user.encode('utf8')).hexdigest())
        payload = customaudience.CustomAudience.format_params(
            customaudience.CustomAudience.Schema.email_hash,
            [test_hash],
            pre_hashed=True
        )

        users = payload['payload']['data']
        assert users[0] == test_hash

    def test_multi_key_params(self):
        schema = [
            customaudience.CustomAudience.Schema.MultiKeySchema.extern_id,
            customaudience.CustomAudience.Schema.MultiKeySchema.fn,
            customaudience.CustomAudience.Schema.MultiKeySchema.email,
            customaudience.CustomAudience.Schema.MultiKeySchema.ln,
        ]
        payload = customaudience.CustomAudience.format_params(
            schema,[["abc123def", "  TEST ", "test", "..test.."]],
            is_raw=True,
        )
        # This is the value of ["  Test ", " test", "..test"] and
        # ["TEST2", 'TEST3', '..test5..'] when it's hashed with sha256
        # extern_id, however, should never get hashed.
        test_hash1 = [
            "abc123def",
            "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
            "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
            "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
        ]

        users = payload['payload']['data']
        assert users[0] == test_hash1

    def test_extern_id_key_single(self):

        schema = [
            customaudience.CustomAudience.Schema.MultiKeySchema.extern_id,
        ]
        payload = customaudience.CustomAudience.format_params(
            schema,
            [
                ["abc123def"], ["abc234def"], ["abc345def"], ["abc456def"],
            ],
            is_raw=True,
        )

        expected = [
            ["abc123def"], ["abc234def"], ["abc345def"], ["abc456def"],
         ]

        actual = payload['payload']['data']
        assert actual == expected


class EdgeIteratorTestCase(unittest.TestCase):

    def test_builds_from_array(self):
        """
        Sometimes the response returns an array inside the data
        key. This asserts that we successfully build objects using
        the objects in that array.
        """
        response = {
            "data": [{
                "id": "6019579"
            }, {
                "id": "6018402"
            }]
        }
        ei = api.Cursor(
            adaccount.AdAccount(fbid='123'),
            ad.Ad,
        )
        objs = ei.build_objects_from_response(response)
        assert len(objs) == 2

    def test_builds_from_object(self):
        """
        Sometimes the response returns a single JSON object. This asserts
        that we're not looking for the data key and that we correctly build
        the object without relying on the data key.
        """
        response = {
            "id": "601957/targetingsentencelines",
            "targetingsentencelines": [{
                "content": "Location - Living In:",
                "children": [
                    "United States"
                ]
            }, {
                "content": "Age:",
                "children": [
                    "18 - 65+"
                ]
            }]
        }
        ei = api.Cursor(
            adaccount.AdAccount(fbid='123'),
            ad.Ad,
        )
        obj = ei.build_objects_from_response(response)
        assert len(obj) == 1 and obj[0]['id'] == "601957/targetingsentencelines"

    def test_total_is_none(self):
        ei = api.Cursor(
            adaccount.AdAccount(fbid='123'),
            ad.Ad,
        )
        self.assertRaises(
            exceptions.FacebookUnavailablePropertyException, ei.total)

    def test_total_is_defined(self):
        ei = api.Cursor(
            adaccount.AdAccount(fbid='123'),
            ad.Ad,
        )
        ei._total_count = 32
        self.assertEqual(ei.total(), 32)

    def test_builds_from_object_with_data_key(self):
        """
        Sometimes the response returns a single JSON object - with a "data".
        For instance with adcreative. This asserts that we successfully
        build the object that is in "data" key.
        """
        response = {
            "data": {
                "name": "test name",
                "status": "ACTIVE",
                "account_id": 'act_345'
            }
        }
        ei = api.Cursor(
            ad.Ad('123'),
            adcreative.AdCreative,
        )
        obj = ei.build_objects_from_response(response)
        assert len(obj) == 1 and obj[0]['account_id'] == 'act_345'

class AbstractCrudObjectTestCase(unittest.TestCase):
    def test_all_aco_has_id_field(self):
        # Some objects do not have FBIDs or don't need checking (ACO)
        for obj in (ad.Ad,
                    adaccount.AdAccount,
                    adcreative.AdCreative,
                    customaudience.CustomAudience,
                    productcatalog.ProductCatalog):
            if (
                inspect.isclass(obj) and
                issubclass(obj, abstractcrudobject.AbstractCrudObject) and
                obj != abstractcrudobject.AbstractCrudObject
            ):
                try:
                    id_field = obj.Field.id
                    assert id_field != ''
                except Exception as e:
                    self.fail("Could not instantiate " + str(obj) + "\n  " + str(e))

    def test_delitem_changes_history(self):
        account = adaccount.AdAccount()
        account['name'] = 'foo'
        assert len(account._changes) > 0
        del account['name']
        assert len(account._changes) == 0

    def test_fields_to_params(self):
        """
        Demonstrates that AbstractCrudObject._assign_fields_to_params()
        handles various combinations of params and fields properly.
        """
        class Foo(abstractcrudobject.AbstractCrudObject):
            _default_read_fields = ['id', 'name']

        class Bar(abstractcrudobject.AbstractCrudObject):
            _default_read_fields = []

        for adclass, fields, params, expected in [
            (Foo, None, {}, {'fields': 'id,name'}),
            (Foo, None, {'a': 'b'}, {'a': 'b', 'fields': 'id,name'}),
            (Foo, ['x'], {}, {'fields': 'x'}),
            (Foo, ['x'], {'a': 'b'}, {'a': 'b', 'fields': 'x'}),
            (Foo, [], {}, {}),
            (Foo, [], {'a': 'b'}, {'a': 'b'}),
            (Bar, None, {}, {}),
            (Bar, None, {'a': 'b'}, {'a': 'b'}),
            (Bar, ['x'], {}, {'fields': 'x'}),
            (Bar, ['x'], {'a': 'b'}, {'a': 'b', 'fields': 'x'}),
            (Bar, [], {}, {}),
            (Bar, [], {'a': 'b'}, {'a': 'b'}),
        ]:
            adclass._assign_fields_to_params(fields, params)
            assert params == expected


class AbstractObjectTestCase(unittest.TestCase):
    def test_export_nested_object(self):
        obj = specs.PagePostData()
        obj2 = {}
        obj2['id'] = 'id'
        obj2['name'] = 'foo'
        obj['from'] = obj2
        expected = {
            'from': {
                'id': 'id',
                'name': 'foo'
            }
        }
        assert obj.export_data() == expected

    def test_export_dict(self):
        obj = specs.ObjectStorySpec()
        obj['link_data'] = {
            'link_data': 3
        }
        expected = {
            'link_data': {
                'link_data': 3
            }
        }
        assert obj.export_data() == expected

    def test_export_none(self):
        obj = specs.ObjectStorySpec()
        obj['link_data'] = None
        expected = {}
        assert obj.export_data() == expected

    def test_export_list(self):
        obj = adcreative.AdCreative()
        obj2 = specs.LinkData()
        obj3 = specs.AttachmentData()
        obj3['description'] = "$100"
        obj2['child_attachments'] = [obj3]
        obj['link_data'] = obj2

        try:
            json.dumps(obj.export_data())
        except:
            self.fail("Objects in crud object export")

    def test_export_no_objects(self):
        obj = specs.ObjectStorySpec()
        obj2 = specs.VideoData()
        obj2['description'] = "foo"
        obj['video_data'] = obj2

        try:
            json.dumps(obj.export_data())
        except:
            self.fail("Objects in object export")

    def test_can_print(self):
        '''Must be able to print nested objects without serialization issues'''
        obj = specs.PagePostData()
        obj2 = {}
        obj2['id'] = 'id'
        obj2['name'] = 'foo'
        obj['from'] = obj2

        try:
            obj.__repr__()
        except TypeError as e:
            self.fail('Cannot call __repr__ on AbstractObject\n %s' % e)


class SessionTestCase(unittest.TestCase):

    def gen_appsecret_proof(self, access_token, app_secret):
        import hashlib
        import hmac

        if version_info < (3, 0):
            h = hmac.new(
                bytes(app_secret),
                msg=bytes(access_token),
                digestmod=hashlib.sha256
            )
        else:
            h = hmac.new(
                bytes(app_secret, 'utf-8'),
                msg=bytes(access_token, 'utf-8'),
                digestmod=hashlib.sha256
            )
        return h.hexdigest()

    def test_appsecret_proof(self):
        app_id = 'reikgukrhgfgtcheghjteirdldlrkjbu'
        app_secret = 'gdrtejfdghurnhnjghjnertihbknlrvv'
        access_token = 'bekguvjhdvdburldfnrfdguljijenklc'

        fb_session = session.FacebookSession(app_id, app_secret, access_token)
        self.assertEqual(
            fb_session.appsecret_proof,
            self.gen_appsecret_proof(access_token, app_secret)
        )


class ProductCatalogTestCase(unittest.TestCase):
    def test_b64_encode_is_correct(self):
        product_id = 'ID_1'
        b64_id_as_str = 'SURfMQ=='

        catalog = productcatalog.ProductCatalog()
        self.assertEqual(b64_id_as_str, catalog.b64_encoded_id(product_id))


class SessionWithoutAppSecretTestCase(unittest.TestCase):
    def test_appsecret_proof_absence(self):
        try:
            session.FacebookSession(
                access_token='thisisfakeaccesstoken'
            )
        except Exception as e:
            self.fail("Could not instantiate " + "\n  " + str(e))


class UrlsUtilsTestCase(unittest.TestCase):

    def test_quote_with_encoding_basestring(self):
        s = "some string"
        self.assertEqual(
            utils.urls.quote_with_encoding(s),
            urllib.parse.quote(s)
        )
        # do not need to test for that in PY3
        if six.PY2:
            s = u"some string with ùnicode".encode("utf-8")
            self.assertEqual(
                utils.urls.quote_with_encoding(s),
                urllib.parse.quote(s)
            )

    def test_quote_with_encoding_unicode(self):
        s = u"some string with ùnicode"
        self.assertEqual(
            utils.urls.quote_with_encoding(s),
            urllib.parse.quote(s.encode("utf-8"))
        )

    def test_quote_with_encoding_integer(self):
        s = 1234
        self.assertEqual(
            utils.urls.quote_with_encoding(s),
            urllib.parse.quote('1234')
        )

    def test_quote_with_encoding_other_than_string_and_integer(self):
        s = [1, 2]
        self.assertRaises(
            ValueError,
            utils.urls.quote_with_encoding, s
        )


class FacebookAdsApiBatchTestCase(unittest.TestCase):

    def test_add_works_with_utf8(self):
        default_api = api.FacebookAdsApi.get_default_api()
        batch_api = api.FacebookAdsApiBatch(default_api)
        batch_api.add('GET', 'some/path', params={"key": u"vàlué"})
        self.assertEqual(len(batch_api), 1)
        self.assertEqual(batch_api._batch[0], {
            'method': 'GET',
            'relative_url': 'some/path?'+'key=' + utils.urls.quote_with_encoding(u'vàlué')
        })


class VersionUtilsTestCase(unittest.TestCase):

    def test_api_version_is_pulled(self):
        version_value = utils.version.get_version()
        assert re.search('[0-9]+\.[0-9]+\.[0-9]', version_value)


class FacebookResponseTestCase(unittest.TestCase):

    def test_is_success_200(self):
        resp = api.FacebookResponse(http_status=200)
        self.assertTrue(resp.is_success())

    def test_is_success_service_unavailable(self):
        resp = api.FacebookResponse(body="Service Unavailable", http_status=200)
        self.assertFalse(resp.is_success())

if __name__ == '__main__':
    unittest.main()
