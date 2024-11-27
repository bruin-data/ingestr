# Copyright (c) Meta Platforms, Inc. and affiliates.
# All rights reserved.

# This source code is licensed under the license found in the
# LICENSE file in the root directory of this source tree.

from facebook_business.adobjects.abstractobject import AbstractObject

"""
This class is auto-generated.

For any issues or feature requests related to this class, please let us know on
github and we'll fix in our codegen framework. We'll not be able to accept
pull request for this class.
"""

class ShadowIGMediaBoostedInsightsResult(
    AbstractObject,
):

    def __init__(self, api=None):
        super(ShadowIGMediaBoostedInsightsResult, self).__init__()
        self._isShadowIGMediaBoostedInsightsResult = True
        self._api = api

    class Field(AbstractObject.Field):
        description = 'description'
        name = 'name'
        organic_media_id = 'organic_media_id'
        source_type = 'source_type'
        title = 'title'
        values = 'values'

    _field_types = {
        'description': 'string',
        'name': 'string',
        'organic_media_id': 'string',
        'source_type': 'string',
        'title': 'string',
        'values': 'list<Object>',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


