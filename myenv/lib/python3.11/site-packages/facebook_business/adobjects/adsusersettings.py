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

class AdsUserSettings(
    AbstractCrudObject,
):

    def __init__(self, fbid=None, parent_id=None, api=None):
        self._isAdsUserSettings = True
        super(AdsUserSettings, self).__init__(fbid, parent_id, api)

    class Field(AbstractObject.Field):
        a_plus_c_survey_seen = 'a_plus_c_survey_seen'
        adgroup_name_template = 'adgroup_name_template'
        ads_tool_visits = 'ads_tool_visits'
        aplusc_carousel_cda_opt_in_status = 'aplusc_carousel_cda_opt_in_status'
        aplusc_carousel_inline_comment_opt_in_status = 'aplusc_carousel_inline_comment_opt_in_status'
        aplusc_epa_opt_in_status = 'aplusc_epa_opt_in_status'
        aplusc_opt_out_friction = 'aplusc_opt_out_friction'
        autoflow_lite_opt_in_status = 'autoflow_lite_opt_in_status'
        autoflow_lite_should_opt_in = 'autoflow_lite_should_opt_in'
        blended_ads_creation_defaulting_opt_in_status = 'blended_ads_creation_defaulting_opt_in_status'
        bookmarked_pages = 'bookmarked_pages'
        campaign_group_name_template = 'campaign_group_name_template'
        campaign_name_template = 'campaign_name_template'
        carousel_to_video_opt_in_status = 'carousel_to_video_opt_in_status'
        connected_sources_catalog_opt_in_status = 'connected_sources_catalog_opt_in_status'
        default_creation_mode = 'default_creation_mode'
        export_format_default = 'export_format_default'
        focus_mode_default = 'focus_mode_default'
        gen_ai_alpha_test_status = 'gen_ai_alpha_test_status'
        id = 'id'
        image_expansion_opt_in_status = 'image_expansion_opt_in_status'
        is_ads_ai_consented = 'is_ads_ai_consented'
        is_cbo_default_on = 'is_cbo_default_on'
        is_se_removal_guidance_dismissed = 'is_se_removal_guidance_dismissed'
        last_used_post_format = 'last_used_post_format'
        last_visited_time = 'last_visited_time'
        multi_ads_settings = 'multi_ads_settings'
        music_on_reels_opt_in = 'music_on_reels_opt_in'
        muted_cbo_midflight_education_messages = 'muted_cbo_midflight_education_messages'
        onsite_destination_optimization_opt_in = 'onsite_destination_optimization_opt_in'
        open_tabs = 'open_tabs'
        previously_seen_recommendations = 'previously_seen_recommendations'
        product_extensions_opt_in = 'product_extensions_opt_in'
        selected_ad_account = 'selected_ad_account'
        selected_comparison_timerange = 'selected_comparison_timerange'
        selected_metric_cic = 'selected_metric_cic'
        selected_metrics_cic = 'selected_metrics_cic'
        selected_page = 'selected_page'
        selected_page_section = 'selected_page_section'
        selected_power_editor_pane = 'selected_power_editor_pane'
        selected_stat_range = 'selected_stat_range'
        should_export_filter_empty_cols = 'should_export_filter_empty_cols'
        should_export_rows_without_unsupported_feature = 'should_export_rows_without_unsupported_feature'
        should_not_auto_expand_tree_table = 'should_not_auto_expand_tree_table'
        should_not_show_cbo_campaign_toggle_off_confirmation_message = 'should_not_show_cbo_campaign_toggle_off_confirmation_message'
        should_not_show_publish_message_on_editor_close = 'should_not_show_publish_message_on_editor_close'
        show_original_videos_opt_in = 'show_original_videos_opt_in'
        static_ad_product_extensions_opt_in = 'static_ad_product_extensions_opt_in'
        sticky_setting_after_default_on = 'sticky_setting_after_default_on'
        syd_campaign_trends_metric = 'syd_campaign_trends_metric'
        total_coupon_syd_dismissals = 'total_coupon_syd_dismissals'
        total_coupon_upsell_dismissals = 'total_coupon_upsell_dismissals'
        use_pe_create_flow = 'use_pe_create_flow'
        use_stepper_primary_entry = 'use_stepper_primary_entry'
        user = 'user'

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
            target_class=AdsUserSettings,
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
        'a_plus_c_survey_seen': 'bool',
        'adgroup_name_template': 'Object',
        'ads_tool_visits': 'list<Object>',
        'aplusc_carousel_cda_opt_in_status': 'string',
        'aplusc_carousel_inline_comment_opt_in_status': 'string',
        'aplusc_epa_opt_in_status': 'string',
        'aplusc_opt_out_friction': 'list<string>',
        'autoflow_lite_opt_in_status': 'string',
        'autoflow_lite_should_opt_in': 'bool',
        'blended_ads_creation_defaulting_opt_in_status': 'string',
        'bookmarked_pages': 'list<Page>',
        'campaign_group_name_template': 'Object',
        'campaign_name_template': 'Object',
        'carousel_to_video_opt_in_status': 'string',
        'connected_sources_catalog_opt_in_status': 'string',
        'default_creation_mode': 'string',
        'export_format_default': 'string',
        'focus_mode_default': 'string',
        'gen_ai_alpha_test_status': 'int',
        'id': 'string',
        'image_expansion_opt_in_status': 'string',
        'is_ads_ai_consented': 'bool',
        'is_cbo_default_on': 'bool',
        'is_se_removal_guidance_dismissed': 'bool',
        'last_used_post_format': 'string',
        'last_visited_time': 'datetime',
        'multi_ads_settings': 'list<map<string, string>>',
        'music_on_reels_opt_in': 'list<map<string, string>>',
        'muted_cbo_midflight_education_messages': 'list<string>',
        'onsite_destination_optimization_opt_in': 'string',
        'open_tabs': 'list<string>',
        'previously_seen_recommendations': 'list<string>',
        'product_extensions_opt_in': 'string',
        'selected_ad_account': 'AdAccount',
        'selected_comparison_timerange': 'Object',
        'selected_metric_cic': 'string',
        'selected_metrics_cic': 'list<string>',
        'selected_page': 'Page',
        'selected_page_section': 'string',
        'selected_power_editor_pane': 'string',
        'selected_stat_range': 'Object',
        'should_export_filter_empty_cols': 'string',
        'should_export_rows_without_unsupported_feature': 'string',
        'should_not_auto_expand_tree_table': 'bool',
        'should_not_show_cbo_campaign_toggle_off_confirmation_message': 'bool',
        'should_not_show_publish_message_on_editor_close': 'bool',
        'show_original_videos_opt_in': 'string',
        'static_ad_product_extensions_opt_in': 'string',
        'sticky_setting_after_default_on': 'string',
        'syd_campaign_trends_metric': 'string',
        'total_coupon_syd_dismissals': 'int',
        'total_coupon_upsell_dismissals': 'int',
        'use_pe_create_flow': 'bool',
        'use_stepper_primary_entry': 'bool',
        'user': 'User',
    }
    @classmethod
    def _get_field_enum_info(cls):
        field_enum_info = {}
        return field_enum_info


