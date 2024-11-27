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

class KeywordDeliveryReport(
    AbstractObject,
):

    def __init__(self, api=None):
        super(KeywordDeliveryReport, self).__init__()
        self._isKeywordDeliveryReport = True
        self._api = api

    class Field(AbstractObject.Field):
        estimated_clicks = 'estimated_clicks'
        estimated_conversions = 'estimated_conversions'
        estimated_cost = 'estimated_cost'
        estimated_cpc = 'estimated_cpc'
        estimated_ctr = 'estimated_ctr'
        estimated_cvr = 'estimated_cvr'
        estimated_impressions = 'estimated_impressions'
        estimated_returns = 'estimated_returns'
        keyword = 'keyword'

    _field_types = {
        'estimated_clicks': 'unsigned int',
        'estimated_conversions': 'unsigned int',
        'estimated_cost': 'float',
        'estimated_cpc': 'float',
        'estimated_ctr': 'float',
        'estimated_cvr': 'float',
        'estimated_impressions': 'unsigned int',
        'estimated_returns': 'float',
        'keyword': 'string',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


