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

class Stories(
    AbstractObject,
):

    def __init__(self, api=None):
        super(Stories, self).__init__()
        self._isStories = True
        self._api = api

    class Field(AbstractObject.Field):
        creation_time = 'creation_time'
        media_id = 'media_id'
        media_type = 'media_type'
        post_id = 'post_id'
        status = 'status'
        url = 'url'

    class Status:
        archived = 'ARCHIVED'
        published = 'PUBLISHED'

    _field_types = {
        'creation_time': 'string',
        'media_id': 'string',
        'media_type': 'string',
        'post_id': 'string',
        'status': 'string',
        'url': 'string',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        field_enum_info['Status'] = Stories.Status.__dict__.values()
        return field_enum_info


