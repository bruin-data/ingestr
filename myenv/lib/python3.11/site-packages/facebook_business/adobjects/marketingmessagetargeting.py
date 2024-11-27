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

class MarketingMessageTargeting(
    AbstractObject,
):

    def __init__(self, api=None):
        super(MarketingMessageTargeting, self).__init__()
        self._isMarketingMessageTargeting = True
        self._api = api

    class Field(AbstractObject.Field):
        automation_type = 'automation_type'
        delay_send_time_second = 'delay_send_time_second'
        delay_send_time_unit = 'delay_send_time_unit'
        subscriber_lists = 'subscriber_lists'
        targeting_rules = 'targeting_rules'

    _field_types = {
        'automation_type': 'string',
        'delay_send_time_second': 'unsigned int',
        'delay_send_time_unit': 'string',
        'subscriber_lists': 'list<RawCustomAudience>',
        'targeting_rules': 'list<Object>',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


