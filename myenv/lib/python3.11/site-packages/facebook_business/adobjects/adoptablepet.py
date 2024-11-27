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

class AdoptablePet(
    AbstractCrudObject,
):

    def __init__(self, fbid=None, parent_id=None, api=None):
        self._isAdoptablePet = True
        super(AdoptablePet, self).__init__(fbid, parent_id, api)

    class Field(AbstractObject.Field):
        adoptable_pet_id = 'adoptable_pet_id'
        adoption_application_form_url = 'adoption_application_form_url'
        age_bucket = 'age_bucket'
        animal_type = 'animal_type'
        applinks = 'applinks'
        availability = 'availability'
        breed = 'breed'
        category_specific_fields = 'category_specific_fields'
        coat_length = 'coat_length'
        color = 'color'
        currency = 'currency'
        description = 'description'
        features = 'features'
        gender = 'gender'
        id = 'id'
        image_fetch_status = 'image_fetch_status'
        images = 'images'
        name = 'name'
        price = 'price'
        sanitized_images = 'sanitized_images'
        secondary_color = 'secondary_color'
        shelter_email = 'shelter_email'
        shelter_name = 'shelter_name'
        shelter_page_id = 'shelter_page_id'
        shelter_phone = 'shelter_phone'
        size = 'size'
        tertiary_color = 'tertiary_color'
        url = 'url'
        visibility = 'visibility'

    class ImageFetchStatus:
        direct_upload = 'DIRECT_UPLOAD'
        fetched = 'FETCHED'
        fetch_failed = 'FETCH_FAILED'
        no_status = 'NO_STATUS'
        outdated = 'OUTDATED'
        partial_fetch = 'PARTIAL_FETCH'

    class Visibility:
        published = 'PUBLISHED'
        staging = 'STAGING'

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
            target_class=AdoptablePet,
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

    def get_augmented_realities_metadata(self, fields=None, params=None, batch=None, success=None, failure=None, pending=False):
        from facebook_business.utils import api_utils
        if batch is None and (success is not None or failure is not None):
          api_utils.warning('`success` and `failure` callback only work for batch call.')
        from facebook_business.adobjects.dynamicarmetadata import DynamicARMetadata
        param_types = {
        }
        enums = {
        }
        request = FacebookRequest(
            node_id=self['id'],
            method='GET',
            endpoint='/augmented_realities_metadata',
            api=self._api,
            param_checker=TypeChecker(param_types, enums),
            target_class=DynamicARMetadata,
            api_type='EDGE',
            response_parser=ObjectParser(target_class=DynamicARMetadata, api=self._api),
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

    def get_channels_to_integrity_status(self, fields=None, params=None, batch=None, success=None, failure=None, pending=False):
        from facebook_business.utils import api_utils
        if batch is None and (success is not None or failure is not None):
          api_utils.warning('`success` and `failure` callback only work for batch call.')
        from facebook_business.adobjects.catalogitemchannelstointegritystatus import CatalogItemChannelsToIntegrityStatus
        param_types = {
        }
        enums = {
        }
        request = FacebookRequest(
            node_id=self['id'],
            method='GET',
            endpoint='/channels_to_integrity_status',
            api=self._api,
            param_checker=TypeChecker(param_types, enums),
            target_class=CatalogItemChannelsToIntegrityStatus,
            api_type='EDGE',
            response_parser=ObjectParser(target_class=CatalogItemChannelsToIntegrityStatus, api=self._api),
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

    def get_videos_metadata(self, fields=None, params=None, batch=None, success=None, failure=None, pending=False):
        from facebook_business.utils import api_utils
        if batch is None and (success is not None or failure is not None):
          api_utils.warning('`success` and `failure` callback only work for batch call.')
        from facebook_business.adobjects.dynamicvideometadata import DynamicVideoMetadata
        param_types = {
        }
        enums = {
        }
        request = FacebookRequest(
            node_id=self['id'],
            method='GET',
            endpoint='/videos_metadata',
            api=self._api,
            param_checker=TypeChecker(param_types, enums),
            target_class=DynamicVideoMetadata,
            api_type='EDGE',
            response_parser=ObjectParser(target_class=DynamicVideoMetadata, api=self._api),
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
        'adoptable_pet_id': 'string',
        'adoption_application_form_url': 'string',
        'age_bucket': 'string',
        'animal_type': 'string',
        'applinks': 'CatalogItemAppLinks',
        'availability': 'string',
        'breed': 'string',
        'category_specific_fields': 'CatalogSubVerticalList',
        'coat_length': 'string',
        'color': 'string',
        'currency': 'string',
        'description': 'string',
        'features': 'list<string>',
        'gender': 'string',
        'id': 'string',
        'image_fetch_status': 'ImageFetchStatus',
        'images': 'list<string>',
        'name': 'string',
        'price': 'string',
        'sanitized_images': 'list<string>',
        'secondary_color': 'string',
        'shelter_email': 'string',
        'shelter_name': 'string',
        'shelter_page_id': 'Page',
        'shelter_phone': 'string',
        'size': 'string',
        'tertiary_color': 'string',
        'url': 'string',
        'visibility': 'Visibility',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        field_enum_info['ImageFetchStatus'] = AdoptablePet.ImageFetchStatus.__dict__.values()
        field_enum_info['Visibility'] = AdoptablePet.Visibility.__dict__.values()
        return field_enum_info


