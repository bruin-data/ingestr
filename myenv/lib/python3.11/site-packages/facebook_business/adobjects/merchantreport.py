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

class MerchantReport(
    AbstractObject,
):

    def __init__(self, api=None):
        super(MerchantReport, self).__init__()
        self._isMerchantReport = True
        self._api = api

    class Field(AbstractObject.Field):
        add_to_cart = 'add_to_cart'
        brand = 'brand'
        catalog_segment_id = 'catalog_segment_id'
        catalog_segment_purchase_value = 'catalog_segment_purchase_value'
        category = 'category'
        date = 'date'
        latest_date = 'latest_date'
        link_clicks = 'link_clicks'
        merchant_currency = 'merchant_currency'
        page_id = 'page_id'
        product_id = 'product_id'
        product_quantity = 'product_quantity'
        product_total_value = 'product_total_value'
        purchase = 'purchase'
        purchase_value = 'purchase_value'

    _field_types = {
        'add_to_cart': 'int',
        'brand': 'string',
        'catalog_segment_id': 'string',
        'catalog_segment_purchase_value': 'float',
        'category': 'string',
        'date': 'string',
        'latest_date': 'string',
        'link_clicks': 'int',
        'merchant_currency': 'string',
        'page_id': 'string',
        'product_id': 'string',
        'product_quantity': 'int',
        'product_total_value': 'float',
        'purchase': 'int',
        'purchase_value': 'float',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


