# Copyright (c) Meta Platforms, Inc. and affiliates.
# All rights reserved.

# This source code is licensed under the license found in the
# LICENSE file in the root directory of this source tree.

from facebook_business.adobjects.abstractobject import AbstractObject
from facebook_business.adobjects.abstractcrudobject import AbstractCrudObject
from facebook_business.adobjects.objectparser import ObjectParser
from facebook_business.api import FacebookRequest
from facebook_business.typechecker import TypeChecker

"""
This class is auto-generated.

For any issues or feature requests related to this class, please let us know on
github and we'll fix in our codegen framework. We'll not be able to accept
pull request for this class.
"""

class NegativeKeywordList(
    AbstractCrudObject,
):

    def __init__(self, fbid=None, parent_id=None, api=None):
        self._isNegativeKeywordList = True
        super(NegativeKeywordList, self).__init__(fbid, parent_id, api)

    class Field(AbstractObject.Field):
        applied_active_ad_campaign_groups = 'applied_active_ad_campaign_groups'
        applied_inactive_ad_campaign_groups = 'applied_inactive_ad_campaign_groups'
        creator_id = 'creator_id'
        id = 'id'
        is_fully_reviewed = 'is_fully_reviewed'
        last_update_time = 'last_update_time'
        last_update_user_id = 'last_update_user_id'
        list_name = 'list_name'
        total_approved_keyword_count = 'total_approved_keyword_count'
        total_declined_keyword_count = 'total_declined_keyword_count'
        total_negative_keyword_count = 'total_negative_keyword_count'
        total_validated_keyword_count = 'total_validated_keyword_count'

    def api_get(self, fields=None, params=None, batch=None, success=None, failure=None, pending=False):
        from facebook_business.utils import api_utils
        if batch is None and (success is not None or failure is not None):
          api_utils.warning('`success` and `failure` callback only work for batch call.')
        param_types = {
        }
        enums = {
        }
        request = FacebookRequest(
            node_id=self['id'],
            method='GET',
            endpoint='/',
            api=self._api,
            param_checker=TypeChecker(param_types, enums),
            target_class=NegativeKeywordList,
            api_type='NODE',
            response_parser=ObjectParser(reuse_object=self),
        )
        request.add_params(params)
        request.add_fields(fields)

        if batch is not None:
            request.add_to_batch(batch, success=success, failure=failure)
            return request
        elif pending:
            return request
        else:
            self.assure_call()
            return request.execute()

    def api_update(self, fields=None, params=None, batch=None, success=None, failure=None, pending=False):
        from facebook_business.utils import api_utils
        if batch is None and (success is not None or failure is not None):
          api_utils.warning('`success` and `failure` callback only work for batch call.')
        param_types = {
            'business_id': 'unsigned int',
            'list_name': 'string',
        }
        enums = {
        }
        request = FacebookRequest(
            node_id=self['id'],
            method='POST',
            endpoint='/',
            api=self._api,
            param_checker=TypeChecker(param_types, enums),
            target_class=NegativeKeywordList,
            api_type='NODE',
            response_parser=ObjectParser(reuse_object=self),
        )
        request.add_params(params)
        request.add_fields(fields)

        if batch is not None:
            request.add_to_batch(batch, success=success, failure=failure)
            return request
        elif pending:
            return request
        else:
            self.assure_call()
            return request.execute()

    _field_types = {
        'applied_active_ad_campaign_groups': 'list<map<string, string>>',
        'applied_inactive_ad_campaign_groups': 'list<map<string, string>>',
        'creator_id': 'string',
        'id': 'string',
        'is_fully_reviewed': 'bool',
        'last_update_time': 'datetime',
        'last_update_user_id': 'string',
        'list_name': 'string',
        'total_approved_keyword_count': 'int',
        'total_declined_keyword_count': 'int',
        'total_negative_keyword_count': 'int',
        'total_validated_keyword_count': 'int',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


