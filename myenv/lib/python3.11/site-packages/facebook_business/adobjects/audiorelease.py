# Copyright (c) Meta Platforms, Inc. and affiliates.
# All rights reserved.

# This source code is licensed under the license found in the
# LICENSE file in the root directory of this source tree.

from facebook_business.adobjects.abstractobject import AbstractObject
from facebook_business.adobjects.abstractcrudobject import AbstractCrudObject
from facebook_business.adobjects.objectparser import ObjectParser
from facebook_business.api import FacebookRequest
from facebook_business.typechecker import TypeChecker

"""
This class is auto-generated.

For any issues or feature requests related to this class, please let us know on
github and we'll fix in our codegen framework. We'll not be able to accept
pull request for this class.
"""

class AudioRelease(
    AbstractCrudObject,
):

    def __init__(self, fbid=None, parent_id=None, api=None):
        self._isAudioRelease = True
        super(AudioRelease, self).__init__(fbid, parent_id, api)

    class Field(AbstractObject.Field):
        album_title = 'album_title'
        asset_availability_status = 'asset_availability_status'
        audio_availability_status = 'audio_availability_status'
        audio_release_image_uri = 'audio_release_image_uri'
        created_time = 'created_time'
        displayed_artist = 'displayed_artist'
        ean = 'ean'
        genre = 'genre'
        grid = 'grid'
        id = 'id'
        isrc = 'isrc'
        label_name = 'label_name'
        original_release_date = 'original_release_date'
        parental_warning_type = 'parental_warning_type'
        proprietary_id = 'proprietary_id'
        upc = 'upc'

    def api_get(self, fields=None, params=None, batch=None, success=None, failure=None, pending=False):
        from facebook_business.utils import api_utils
        if batch is None and (success is not None or failure is not None):
          api_utils.warning('`success` and `failure` callback only work for batch call.')
        param_types = {
        }
        enums = {
        }
        request = FacebookRequest(
            node_id=self['id'],
            method='GET',
            endpoint='/',
            api=self._api,
            param_checker=TypeChecker(param_types, enums),
            target_class=AudioRelease,
            api_type='NODE',
            response_parser=ObjectParser(reuse_object=self),
        )
        request.add_params(params)
        request.add_fields(fields)

        if batch is not None:
            request.add_to_batch(batch, success=success, failure=failure)
            return request
        elif pending:
            return request
        else:
            self.assure_call()
            return request.execute()

    _field_types = {
        'album_title': 'string',
        'asset_availability_status': 'list<map<int, Object>>',
        'audio_availability_status': 'string',
        'audio_release_image_uri': 'string',
        'created_time': 'datetime',
        'displayed_artist': 'string',
        'ean': 'string',
        'genre': 'string',
        'grid': 'string',
        'id': 'string',
        'isrc': 'string',
        'label_name': 'string',
        'original_release_date': 'datetime',
        'parental_warning_type': 'string',
        'proprietary_id': 'string',
        'upc': 'string',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


