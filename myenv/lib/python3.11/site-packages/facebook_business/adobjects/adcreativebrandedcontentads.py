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

class AdCreativeBrandedContentAds(
    AbstractObject,
):

    def __init__(self, api=None):
        super(AdCreativeBrandedContentAds, self).__init__()
        self._isAdCreativeBrandedContentAds = True
        self._api = api

    class Field(AbstractObject.Field):
        ad_format = 'ad_format'
        content_search_input = 'content_search_input'
        creator_ad_permission_type = 'creator_ad_permission_type'
        facebook_boost_post_access_token = 'facebook_boost_post_access_token'
        instagram_boost_post_access_token = 'instagram_boost_post_access_token'
        is_mca_internal = 'is_mca_internal'
        partners = 'partners'
        promoted_page_id = 'promoted_page_id'
        ui_version = 'ui_version'

    _field_types = {
        'ad_format': 'int',
        'content_search_input': 'string',
        'creator_ad_permission_type': 'string',
        'facebook_boost_post_access_token': 'string',
        'instagram_boost_post_access_token': 'string',
        'is_mca_internal': 'bool',
        'partners': 'list<AdCreativeBrandedContentAdsPartners>',
        'promoted_page_id': 'string',
        'ui_version': 'int',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


