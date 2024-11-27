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

class ContentDeliveryReport(
    AbstractObject,
):

    def __init__(self, api=None):
        super(ContentDeliveryReport, self).__init__()
        self._isContentDeliveryReport = True
        self._api = api

    class Field(AbstractObject.Field):
        content_name = 'content_name'
        content_url = 'content_url'
        creator_name = 'creator_name'
        creator_url = 'creator_url'
        estimated_impressions = 'estimated_impressions'

    _field_types = {
        'content_name': 'string',
        'content_url': 'string',
        'creator_name': 'string',
        'creator_url': 'string',
        'estimated_impressions': 'unsigned int',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


