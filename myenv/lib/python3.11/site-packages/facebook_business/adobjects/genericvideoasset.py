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

class GenericVideoAsset(
    AbstractCrudObject,
):

    def __init__(self, fbid=None, parent_id=None, api=None):
        self._isGenericVideoAsset = True
        super(GenericVideoAsset, self).__init__(fbid, parent_id, api)

    class Field(AbstractObject.Field):
        broadcast_id = 'broadcast_id'
        broadcast_planned_start_time = 'broadcast_planned_start_time'
        can_viewer_edit = 'can_viewer_edit'
        copyright_monitoring_status = 'copyright_monitoring_status'
        created_time = 'created_time'
        creator = 'creator'
        description = 'description'
        download_hd_url = 'download_hd_url'
        download_sd_url = 'download_sd_url'
        embeddable = 'embeddable'
        expiration = 'expiration'
        feed_type = 'feed_type'
        id = 'id'
        is_crossposting_eligible = 'is_crossposting_eligible'
        is_crossposting_within_bm_eligible = 'is_crossposting_within_bm_eligible'
        is_crossposting_within_bm_enabled = 'is_crossposting_within_bm_enabled'
        is_episode = 'is_episode'
        is_featured = 'is_featured'
        is_live_premiere = 'is_live_premiere'
        is_video_asset = 'is_video_asset'
        last_added_time = 'last_added_time'
        latest_creator = 'latest_creator'
        latest_owned_description = 'latest_owned_description'
        latest_owned_title = 'latest_owned_title'
        length = 'length'
        live_status = 'live_status'
        no_story = 'no_story'
        owner_name = 'owner_name'
        owner_picture = 'owner_picture'
        owner_post_state = 'owner_post_state'
        permalink_url = 'permalink_url'
        picture = 'picture'
        posts_count = 'posts_count'
        posts_ids = 'posts_ids'
        posts_status = 'posts_status'
        premiere_living_room_status = 'premiere_living_room_status'
        published = 'published'
        scheduled_publish_time = 'scheduled_publish_time'
        secret = 'secret'
        secure_stream_url = 'secure_stream_url'
        social_actions = 'social_actions'
        status = 'status'
        stream_url = 'stream_url'
        thumbnail_while_encoding = 'thumbnail_while_encoding'
        title = 'title'
        views = 'views'

    _field_types = {
        'broadcast_id': 'string',
        'broadcast_planned_start_time': 'datetime',
        'can_viewer_edit': 'bool',
        'copyright_monitoring_status': 'string',
        'created_time': 'datetime',
        'creator': 'User',
        'description': 'string',
        'download_hd_url': 'string',
        'download_sd_url': 'string',
        'embeddable': 'bool',
        'expiration': 'Object',
        'feed_type': 'string',
        'id': 'string',
        'is_crossposting_eligible': 'bool',
        'is_crossposting_within_bm_eligible': 'bool',
        'is_crossposting_within_bm_enabled': 'bool',
        'is_episode': 'bool',
        'is_featured': 'bool',
        'is_live_premiere': 'bool',
        'is_video_asset': 'bool',
        'last_added_time': 'datetime',
        'latest_creator': 'User',
        'latest_owned_description': 'string',
        'latest_owned_title': 'string',
        'length': 'float',
        'live_status': 'string',
        'no_story': 'bool',
        'owner_name': 'string',
        'owner_picture': 'string',
        'owner_post_state': 'string',
        'permalink_url': 'string',
        'picture': 'string',
        'posts_count': 'unsigned int',
        'posts_ids': 'list<string>',
        'posts_status': 'Object',
        'premiere_living_room_status': 'string',
        'published': 'bool',
        'scheduled_publish_time': 'datetime',
        'secret': 'bool',
        'secure_stream_url': 'string',
        'social_actions': 'bool',
        'status': 'VideoStatus',
        'stream_url': 'string',
        'thumbnail_while_encoding': 'string',
        'title': 'string',
        'views': 'unsigned int',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


