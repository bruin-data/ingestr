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

class ManagedPartnerBusiness(
    AbstractObject,
):

    def __init__(self, api=None):
        super(ManagedPartnerBusiness, self).__init__()
        self._isManagedPartnerBusiness = True
        self._api = api

    class Field(AbstractObject.Field):
        ad_account = 'ad_account'
        catalog_segment = 'catalog_segment'
        extended_credit = 'extended_credit'
        page = 'page'
        seller_business_info = 'seller_business_info'
        seller_business_status = 'seller_business_status'
        template = 'template'

    _field_types = {
        'ad_account': 'AdAccount',
        'catalog_segment': 'ProductCatalog',
        'extended_credit': 'ManagedPartnerExtendedCredit',
        'page': 'Page',
        'seller_business_info': 'Object',
        'seller_business_status': 'string',
        'template': 'list<Object>',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


