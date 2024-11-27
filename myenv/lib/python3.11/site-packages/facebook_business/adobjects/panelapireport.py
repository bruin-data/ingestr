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

class PanelAPIReport(
    AbstractCrudObject,
):

    def __init__(self, fbid=None, parent_id=None, api=None):
        self._isPanelAPIReport = True
        super(PanelAPIReport, self).__init__(fbid, parent_id, api)

    class Field(AbstractObject.Field):
        checksum = 'checksum'
        download_url = 'download_url'
        end_date = 'end_date'
        export_file_type = 'export_file_type'
        id = 'id'
        index = 'index'
        name = 'name'
        number_of_chunks = 'number_of_chunks'
        start_date = 'start_date'
        upload_date = 'upload_date'

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
            target_class=PanelAPIReport,
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
        'checksum': 'string',
        'download_url': 'string',
        'end_date': 'datetime',
        'export_file_type': 'string',
        'id': 'string',
        'index': 'int',
        'name': 'string',
        'number_of_chunks': 'int',
        'start_date': 'datetime',
        'upload_date': 'datetime',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


