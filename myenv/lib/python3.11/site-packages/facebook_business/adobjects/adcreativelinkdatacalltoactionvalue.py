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

class AdCreativeLinkDataCallToActionValue(
    AbstractObject,
):

    def __init__(self, api=None):
        super(AdCreativeLinkDataCallToActionValue, self).__init__()
        self._isAdCreativeLinkDataCallToActionValue = True
        self._api = api

    class Field(AbstractObject.Field):
        app_destination = 'app_destination'
        app_link = 'app_link'
        application = 'application'
        event_id = 'event_id'
        lead_gen_form_id = 'lead_gen_form_id'
        link = 'link'
        link_caption = 'link_caption'
        link_format = 'link_format'
        page = 'page'
        product_link = 'product_link'
        whatsapp_number = 'whatsapp_number'

    _field_types = {
        'app_destination': 'string',
        'app_link': 'string',
        'application': 'string',
        'event_id': 'string',
        'lead_gen_form_id': 'string',
        'link': 'string',
        'link_caption': 'string',
        'link_format': 'string',
        'page': 'string',
        'product_link': 'string',
        'whatsapp_number': 'string',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


