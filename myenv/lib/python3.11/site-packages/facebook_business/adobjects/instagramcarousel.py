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

class InstagramCarousel(
    AbstractCrudObject,
):

    def __init__(self, fbid=None, parent_id=None, api=None):
        self._isInstagramCarousel = True
        super(InstagramCarousel, self).__init__(fbid, parent_id, api)

    class Field(AbstractObject.Field):
        caption_text = 'caption_text'
        comment_count = 'comment_count'
        content_type = 'content_type'
        display_url = 'display_url'
        id = 'id'
        like_count = 'like_count'
        owner_instagram_user = 'owner_instagram_user'
        permalink = 'permalink'
        taken_at = 'taken_at'
        video_url = 'video_url'

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
            target_class=InstagramCarousel,
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

    def get_comments(self, fields=None, params=None, batch=None, success=None, failure=None, pending=False):
        from facebook_business.utils import api_utils
        if batch is None and (success is not None or failure is not None):
          api_utils.warning('`success` and `failure` callback only work for batch call.')
        from facebook_business.adobjects.instagramcomment import InstagramComment
        param_types = {
        }
        enums = {
        }
        request = FacebookRequest(
            node_id=self['id'],
            method='GET',
            endpoint='/comments',
            api=self._api,
            param_checker=TypeChecker(param_types, enums),
            target_class=InstagramComment,
            api_type='EDGE',
            response_parser=ObjectParser(target_class=InstagramComment, api=self._api),
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

    def create_comment(self, fields=None, params=None, batch=None, success=None, failure=None, pending=False):
        from facebook_business.utils import api_utils
        if batch is None and (success is not None or failure is not None):
          api_utils.warning('`success` and `failure` callback only work for batch call.')
        from facebook_business.adobjects.instagramcomment import InstagramComment
        param_types = {
            'ad_id': 'string',
            'message': 'string',
        }
        enums = {
        }
        request = FacebookRequest(
            node_id=self['id'],
            method='POST',
            endpoint='/comments',
            api=self._api,
            param_checker=TypeChecker(param_types, enums),
            target_class=InstagramComment,
            api_type='EDGE',
            response_parser=ObjectParser(target_class=InstagramComment, api=self._api),
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
        'caption_text': 'string',
        'comment_count': 'int',
        'content_type': 'int',
        'display_url': 'string',
        'id': 'string',
        'like_count': 'int',
        'owner_instagram_user': 'InstagramUser',
        'permalink': 'string',
        'taken_at': 'datetime',
        'video_url': 'string',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


