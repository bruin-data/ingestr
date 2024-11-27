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

class CatalogItemAppealStatus(
    AbstractObject,
):

    def __init__(self, api=None):
        super(CatalogItemAppealStatus, self).__init__()
        self._isCatalogItemAppealStatus = True
        self._api = api

    class Field(AbstractObject.Field):
        handle = 'handle'
        item_id = 'item_id'
        status = 'status'
        use_cases = 'use_cases'

    class Status:
        this_item_cannot_be_appealed_as_it_is_either_approved_or_already_has_an_appeal = 'This item cannot be appealed as it is either approved or already has an appeal'
        this_item_is_not_rejected_for_any_of_channels = 'This item is not rejected for any of channels'
        we_ve_encountered_unexpected_error_while_processing_this_request_please_try_again_later_ = 'We\'ve encountered unexpected error while processing this request. Please try again later !'
        you_ve_reached_the_maximum_number_of_item_requests_you_can_make_this_week_you_ll_be_able_to_request_item_reviews_again_within_the_next_7_days_ = 'You\'ve reached the maximum number of item requests you can make this week. You\'ll be able to request item reviews again within the next 7 days.'
        your_request_was_received_see_information_below_to_learn_more_ = 'Your request was received. See information below to learn more.'

    _field_types = {
        'handle': 'string',
        'item_id': 'int',
        'status': 'Status',
        'use_cases': 'list<Object>',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        field_enum_info['Status'] = CatalogItemAppealStatus.Status.__dict__.values()
        return field_enum_info


