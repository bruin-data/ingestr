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

class ExternalEventSourceDAStatsResult(
    AbstractObject,
):

    def __init__(self, api=None):
        super(ExternalEventSourceDAStatsResult, self).__init__()
        self._isExternalEventSourceDAStatsResult = True
        self._api = api

    class Field(AbstractObject.Field):
        count_content_ids = 'count_content_ids'
        count_content_ids_match_any_catalog = 'count_content_ids_match_any_catalog'
        count_fires = 'count_fires'
        count_fires_match_any_catalog = 'count_fires_match_any_catalog'
        date = 'date'
        percentage_missed_users = 'percentage_missed_users'

    _field_types = {
        'count_content_ids': 'unsigned int',
        'count_content_ids_match_any_catalog': 'unsigned int',
        'count_fires': 'unsigned int',
        'count_fires_match_any_catalog': 'unsigned int',
        'date': 'string',
        'percentage_missed_users': 'float',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


