# Copyright 2014 Facebook, Inc.

# You are hereby granted a non-exclusive, worldwide, royalty-free license to
# use, copy, modify, and distribute this software in source code or binary
# form for use in connection with the web services and APIs provided by
# Facebook.

# As with any software that integrates with the Facebook platform, your use
# of this software is subject to the Facebook Developer Principles and
# Policies [http://developers.facebook.com/policy/]. This copyright notice
# shall be included in all copies or substantial portions of the software.

# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
# THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
# FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
# DEALINGS IN THE SOFTWARE.

'''
Unit tests for the Python Facebook Business SDK.

How to run:
    python -m facebook_business.test.docs
    python -m facebook_business.test.docs -v AdGroupDocsTestCase
'''

import os
import sys
import json
from .docs_utils import *


class AdAccountDocsTestCase(DocsTestCase):
    def setUp(self):
        # Create Campaigns
        campaign = self.create_campaign(1)
        self.create_campaign(2)
        # Create AdSets
        adset = self.create_adset(1, campaign)
        self.create_adset(2, campaign)
        # Create Creatives
        creative1 = self.create_creative(1)
        creative2 = self.create_creative(2)
        # Create AdGroups
        ad = self.create_ad(1, adset, creative1)
        self.create_ad(2, adset, creative2)
        DocsDataStore.set('ad_id', ad.get_id())
        # Create Ad Labels
        adlabel = self.create_adlabel()
        DocsDataStore.set('adlabel_id', adlabel['id'])
        # Create AdImage
        image = self.create_image()
        DocsDataStore.set('ad_account_image_hash', image['hash'])


    def test_get_insights(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        insights = account.get_insights(fields=[
            Insights.Field.campaign_id,
            Insights.Field.unique_clicks,
            Insights.Field.impressions,
        ], params={
            'level': Insights.Level.campaign,
            'date_preset': Insights.Preset.yesterday,
        })
        self.store_response(insights)

    def test_get_activities(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        activities = account.get_activities(fields=[
            Activity.Field.event_type,
            Activity.Field.event_time,
        ])
        self.store_response(activities[0])

    def test_opt_out_user_from_targeting(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        response = account.opt_out_user_from_targeting(
            schema=CustomAudience.Schema.email_hash,
            users=['joe@example.com'],
        )
        self.store_response(response)

    def test_get_campaigns(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        campaigns = account.get_campaigns(fields=[
            Campaign.Field.name,
            Campaign.Field.configured_status,
        ])
        self.store_response(campaigns)

    def test_get_ad_sets(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        adsets = account.get_ad_sets(fields=[
            AdSet.Field.name,
            AdSet.Field.bid_info,
            AdSet.Field.configured_status,
            AdSet.Field.daily_budget,
            AdSet.Field.targeting,
        ])
        self.store_response(adsets)

    def test_get_ads(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        ads = account.get_ads(fields=[
            Ad.Field.name,
            Ad.Field.configured_status,
            Ad.Field.creative,
        ])
        self.store_response(ads)

    def test_get_ad_users(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        users = account.get_ad_users()
        self.store_response(users)

    def test_get_ad_creatives(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        creatives = account.get_ad_creatives(fields=[
            AdCreative.Field.name,
            AdCreative.Field.image_hash,
        ])
        self.store_response(creatives[0:2])

    def test_get_ad_images(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        images = account.get_ad_images(fields=[
            AdImage.Field.hash,
        ])
        self.store_response(images[0:2])

    def test_get_ad_conversion_pixels(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        pixels = account.get_ad_conversion_pixels(fields=[
            AdImage.Field.hash,
        ])
        self.store_response(pixels)

    def test_get_broad_category_targeting(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        bct = account.get_broad_category_targeting()
        self.store_response(bct[0:2])

    def test_get_connection_objects(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        connection_objects = account.get_connection_objects()
        connection_objects = [
            co for co in connection_objects if co['id'] == '606699326111137']
        self.store_response(connection_objects)

    def test_get_custom_audiences(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        custom_audiences = account.get_custom_audiences()
        self.store_response(custom_audiences[0:2])

    def test_get_partner_categories(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        partner_categories = account.get_partner_categories()
        self.store_response(partner_categories[0])

    def test_get_rate_cards(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        rate_cards = account.get_rate_cards()
        self.store_response(rate_cards[0:2])

    def test_get_reach_estimate(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        reach_estimate = account.get_reach_estimate(params={
            'currency': 'USD',
            'optimize_for': 'OFFSITE_CONVERSIONS',
            'targeting_spec': {
                'geo_locations': {
                    'countries': ['US'],
                }
            }
        })
        self.store_response(reach_estimate)

    def test_get_transactions(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        transactions = account.get_transactions()
        self.store_response(transactions[0:2])

    def test_get_ad_preview(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        ad_preview = account.get_ad_preview(params={
            'creative': {
                'title': 'This is the title',
                'body': 'This is the body',
                'object_url': 'https://facebookmarketingpartners.com',
                'image_hash': DocsDataStore.get('ad_account_image_hash'),
            },
            'ad_format': 'RIGHT_COLUMN_STANDARD',
        })
        self.store_response(ad_preview)

    def test_get_ads_pixels(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        pixels = account.get_ads_pixels(fields=[
            AdsPixel.Field.name,
            AdsPixel.Field.id,
        ])
        self.store_response(pixels)

    def test_get_targeting_description(self):
        adgroup = Ad(DocsDataStore.get('ad_id'))
        targeting = {
            TargetingSpecsField.geo_locations: {
                TargetingSpecsField.countries: ['US'],
            },
        }
        targeting_desc = adgroup.get_targeting_description(fields=[
            'targetingsentencelines'
        ], params={
            'targeting_spec': targeting
        })
        self.store_response(targeting_desc)

    def test_get_ad_labels(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        adlabels = account.get_ad_labels(fields=[
            AdLabel.Field.name,
            AdLabel.Field.id,
        ])
        self.store_response(adlabels)

    def test_get_ad_creatives_by_labels(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        adlabel_id = DocsDataStore.get('adlabel_id')
        params = {'ad_label_ids': [adlabel_id], 'operator': 'ALL'}
        adcreatives = account.get_ad_creatives_by_labels(fields=[
            AdLabel.Field.name,
            AdLabel.Field.id,
        ], params=params)
        self.store_response(adcreatives)

    def test_get_ads_by_labels(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        adlabel_id = DocsDataStore.get('adlabel_id')
        params = {'ad_label_ids': [adlabel_id], 'operator': 'ALL'}
        ads = account.get_ads_by_labels(fields=[
            AdLabel.Field.name,
            AdLabel.Field.id,
        ], params=params)
        self.store_response(ads)

    def test_get_adsets_by_labels(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        adlabel_id = DocsDataStore.get('adlabel_id')
        params = {'ad_label_ids': [adlabel_id], 'operator': 'ALL'}
        adsets = account.get_adsets_by_labels(fields=[
            AdLabel.Field.name,
            AdLabel.Field.id,
        ], params=params)
        self.store_response(adsets)

    def test_get_campaigns_by_labels(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        adlabel_id = DocsDataStore.get('adlabel_id')
        params = {'ad_label_ids': [adlabel_id], 'operator': 'ALL'}
        campaigns = account.get_campaigns_by_labels(fields=[
            AdLabel.Field.name,
            AdLabel.Field.id,
        ], params=params)
        self.store_response(campaigns)

    def test_get_minimum_budgets(self):
        account = AdAccount(DocsDataStore.get('adaccount_id'))
        min_budgets = account.get_minimum_budgets()
        self.store_response(min_budgets[0:2])


class AdUserDocsTestCase(DocsTestCase):
    def test_get_ad_accounts(self):
        user = AdUser('me')
        accounts = user.get_ad_accounts(fields=[
            AdAccount.Field.name,
        ])
        self.store_response(accounts[0:3])

    def test_get_ad_account(self):
        user = AdUser('me')
        account = user.get_ad_account(fields=[
            AdAccount.Field.name,
        ])
        self.store_response(account)

    def test_get_pages(self):
        user = AdUser('me')
        pages = user.get_pages(fields=[
            Page.Field.name,
        ])
        self.store_response(pages)


class AdCreativeDocsTestCase(DocsTestCase):
    def setUp(self):
        creative = self.create_creative(1)
        DocsDataStore.set('creative_id', creative.get_id())

    def test_get_ad_preview(self):
        creative = AdCreative(DocsDataStore.get('creative_id'))
        preview = creative.get_ad_preview(params={
            'ad_format': 'RIGHT_COLUMN_STANDARD',
        })
        self.store_response(preview)


class AdDocsTestCase(DocsTestCase):
    def setUp(self):
        campaign = self.create_campaign(1)
        adset = self.create_adset(1, campaign)
        creative = self.create_creative_leads(1)
        ad = self.create_ad(1, adset, creative)
        DocsDataStore.set('ad_id', ad.get_id())

    def test_get_ad_creatives(self):
        ad = Ad(DocsDataStore.get('ad_id'))
        creatives = ad.get_ad_creatives(fields=[AdCreative.Field.name])
        self.store_response(creatives)

    def test_get_targeting_description(self):
        ad = Ad(DocsDataStore.get('ad_id'))
        targeting_desc = ad.get_targeting_description(fields=[
            'targetingsentencelines',
        ])
        self.store_response(targeting_desc)

    def test_get_keyword_stats(self):
        ad = Ad(DocsDataStore.get('ad_id'))
        keywords = ad.get_keyword_stats()
        self.store_response(keywords)

    def test_get_ad_preview(self):
        ad = Ad(DocsDataStore.get('ad_id'))
        ad_preview = ad.get_ad_preview(params={
            'ad_format': 'RIGHT_COLUMN_STANDARD',
        })
        self.store_response(ad_preview)

    def test_get_reach_estimate(self):
        ad = Ad(DocsDataStore.get('ad_id'))
        reach_estimate = ad.get_reach_estimate()
        self.store_response(reach_estimate)

    def test_get_click_tracking_tag(self):
        ad = Ad(DocsDataStore.get('ad_id'))
        tag = ad.get_click_tracking_tag()
        self.store_response(tag)

    def test_get_leads(self):
        ad = Ad(DocsDataStore.get('ad_id'))
        leads = ad.get_leads()
        self.store_response(leads)


class AdImageDocsTestCase(DocsTestCase):
    def setUp(self):
        DocsDataStore.set(
            'image_zip', os.path.join(os.path.dirname(__file__), 'test.zip'))
        DocsDataStore.set(
            'image_path', os.path.join(os.path.dirname(__file__), 'test.png'))
        image = AdImage(parent_id=DocsDataStore.get('adaccount_id'))
        image[AdImage.Field.filename] = DocsDataStore.get('image_path')
        image.remote_create()
        DocsDataStore.set('image_id', image['id'])

    def test_remote_create(self):
        image = AdImage(parent_id=DocsDataStore.get('adaccount_id'))
        image[AdImage.Field.filename] = DocsDataStore.get('image_path')
        image.remote_create()
        self.store_response(image)

    def test_remote_create_from_zip(self):
        images = AdImage.remote_create_from_zip(
            filename=DocsDataStore.get('image_zip'),
            parent_id=DocsDataStore.get('adaccount_id'),
        )
        self.store_response(images)

    def test_remote_read(self):
        image = AdImage(DocsDataStore.get('image_id'))
        image.remote_read()
        self.store_response(image)

    def test_get_hash(self):
        image = AdImage(DocsDataStore.get('image_id'))
        image.remote_read()
        image_hash = image.get_hash()
        self.store_response(image_hash)


class AdSetDocsTestCase(DocsTestCase):
    def setUp(self):
        campaign = self.create_campaign(1)
        adset = self.create_adset(1, campaign)
        creative1 = self.create_creative(1)
        creative2 = self.create_creative(2)
        self.create_ad(1, adset, creative1)
        self.create_ad(2, adset, creative2)
        DocsDataStore.set('adcampaign_id', campaign.get_id())
        DocsDataStore.set('adset_id', adset.get_id())

    def test_get_ads(self):
        adset = AdSet(DocsDataStore.get('adset_id'))
        adgroups = adset.get_ads(fields=[
            AdSet.Field.name,
            AdSet.Field.campaign_id,
            AdSet.Field.configured_status,
        ])
        self.store_response(adgroups)

    def test_get_ad_creatives(self):
        adset = AdSet(DocsDataStore.get('adset_id'))
        adcreatives = adset.get_ad_creatives(fields=[
            AdCreative.Field.name,
            AdCreative.Field.id,
            AdCreative.Field.preview_url,
            AdCreative.Field.call_to_action_type,
        ])
        self.store_response(adcreatives)


class AdsPixelDocsTestCase(DocsTestCase):

    def setUp(self):
        pixel = self.create_ads_pixel()
        DocsDataStore.set('pixel_id', pixel.get_id())

    def test_share_pixel_with_ad_account(self):
        business_id = DocsDataStore.get('business_id')
        act_id = DocsDataStore.get('adaccount_id')
        destination_account_id = act_id.replace("act_", "")
        pixel_id = DocsDataStore.get('pixel_id')
        pixel = AdsPixel(pixel_id)
        pixel.share_pixel_with_ad_account(business_id, destination_account_id)
        self.store_response(pixel)

    def test_get_agencies(self):
        pixel_id = DocsDataStore.get('pixel_id')
        pixel = AdsPixel(pixel_id)
        shared_agencies = pixel.get_agencies()
        self.store_response(shared_agencies)

    def test_unshare_pixel_from_ad_account(self):
        business_id = DocsDataStore.get('business_id')
        account_id = DocsDataStore.get('adaccount_id').replace("act_", "")
        pixel_id = DocsDataStore.get('pixel_id')
        pixel = AdsPixel(pixel_id)
        pixel.unshare_pixel_from_ad_account(business_id, account_id)
        self.store_response(pixel)


class BusinessDocsTestCase(DocsTestCase):
    def test_get_product_catalogs(self):
        business = Business(DocsDataStore.get('business_id'))
        catalogs = business.get_product_catalogs()
        self.store_response(catalogs[0])

    def test_get_insights(self):
        business = Business(DocsDataStore.get('business_id'))
        insights = business.get_insights(fields=[
            Insights.Field.campaign_id,
            Insights.Field.unique_clicks,
            Insights.Field.impressions,
        ], params={
            'level': Insights.Level.campaign,
            'date_preset': Insights.Preset.yesterday,
        })
        self.store_response(insights)


class CustomAudienceDocsTestCase(DocsTestCase):

    def setUp(self):
        ca = self.create_custom_audience()
        DocsDataStore.set('ca_id', ca.get_id_assured())

    def test_add_users(self):
        custom_audience = CustomAudience(DocsDataStore.get('ca_id'))
        response = custom_audience.add_users(
            schema=CustomAudience.Schema.email_hash,
            users=[
                'joe@example.com',
            ]
        )
        self.store_response(response)

    def test_remove_users(self):
        custom_audience = CustomAudience(DocsDataStore.get('ca_id'))
        response = custom_audience.remove_users(
            schema=CustomAudience.Schema.email_hash,
            users=[
                'joe@example.com',
            ]
        )
        self.store_response(response)

    def test_format_params(self):
        formatted_params = CustomAudience.format_params(
            schema=CustomAudience.Schema.email_hash,
            users=['joe@example.com'],
        )
        self.store_response(formatted_params)


class CampaignDocsTestCase(DocsTestCase):
    def setUp(self):
        campaign = self.create_campaign(1)
        adset = self.create_adset(1, campaign)
        self.create_adset(2, campaign)
        creative = self.create_creative(1)
        self.create_ad(1, adset, creative)
        self.create_ad(2, adset, creative)
        DocsDataStore.set('campaign_id', campaign.get_id())

    def test_get_ad_sets(self):
        campaign = Campaign(DocsDataStore.get('campaign_id'))
        adsets = campaign.get_ad_sets(fields=[
            AdSet.Field.name,
            AdSet.Field.id,
            AdSet.Field.daily_budget,
        ])
        self.store_response(adsets[0])

    def test_get_ads(self):
        campaign = Campaign(DocsDataStore.get('campaign_id'))
        ads = campaign.get_ads(fields=[
            Ad.Field.name,
            Ad.Field.configured_status,
            Ad.Field.creative,
        ])
        self.store_response(ads)


class ProductGroupDocsTestCase(DocsTestCase):
    pass


class ProductFeedDocsTestCase(DocsTestCase):

    def setUp(self):
        product_catalog = self.create_product_catalog()
        DocsDataStore.set('dpa_catalog_id', product_catalog.get_id())
        product_feed = self.create_product_feed(product_catalog.get_id())
        DocsDataStore.set('dpa_feed_id', product_feed.get_id())

    def test_get_products(self):
        feed = ProductFeed(DocsDataStore.get('dpa_feed_id'))
        products = feed.get_products(fields=[
            Product.Field.title,
            Product.Field.price,
        ])
        self.store_response(products)


class ProductAudienceDocsTestCase(DocsTestCase):
    pass


class ProductDocsTestCase(DocsTestCase):
    pass


class ProductCatalogDocsTestCase(DocsTestCase):

    def setUp(self):
        product_catalog = self.create_product_catalog()
        DocsDataStore.set('dpa_catalog_id', product_catalog.get_id())
        pixel = self.create_ads_pixel()
        DocsDataStore.set('pixel_id', pixel.get_id())

    def test_get_product_feeds(self):
        catalog = ProductCatalog(DocsDataStore.get('dpa_catalog_id'))
        feeds = catalog.get_product_feeds()
        self.store_response(feeds[0])

    def test_add_external_event_sources(self):
        catalog = ProductCatalog(DocsDataStore.get('dpa_catalog_id'))
        response = catalog.add_external_event_sources(pixel_ids=[
            DocsDataStore.get('pixel_id'),
        ])
        self.store_response(response)

    def test_get_external_event_sources(self):
        catalog = ProductCatalog(DocsDataStore.get('dpa_catalog_id'))
        sources = catalog.get_external_event_sources()
        self.store_response(sources)

    def test_remove_external_event_sources(self):
        catalog = ProductCatalog(DocsDataStore.get('dpa_catalog_id'))
        response = catalog.remove_external_event_sources(pixel_ids=[
            DocsDataStore.get('pixel_id'),
        ])
        self.store_response(response)


class ProductSetDocsTestCase(DocsTestCase):

    def setUp(self):
        product_catalog = self.create_product_catalog()
        DocsDataStore.set('dpa_catalog_id', product_catalog.get_id())
        product_set = self.create_product_set(product_catalog.get_id())
        DocsDataStore.set('dpa_set_id', product_set.get_id())

    def test_get_product_groups(self):
        product_set = ProductSet(DocsDataStore.get('dpa_set_id'))
        product_groups = product_set.get_product_groups(fields=[
            Product.Field.title,
            Product.Field.price,
        ])
        self.store_response(product_groups)

    def test_get_products(self):
        product_set = ProductSet(DocsDataStore.get('dpa_set_id'))
        products = product_set.get_products(fields=[
            Product.Field.title,
            Product.Field.price,
        ])
        self.store_response(products)


class ProductFeedUploadErrorDocsTestCase(DocsTestCase):
    pass


class AdConversionPixelDocsTestCase(DocsTestCase):
    pass


class ClickTrackingTagDocsTestCase(DocsTestCase):
    pass


class InsightsDocsTestCase(DocsTestCase):
    pass


class PageDocsTestCase(DocsTestCase):

    def test_get_leadgen_forms(self):
        page = Page(DocsDataStore.get('page_id'))
        leadgen_forms = page.get_leadgen_forms()
        self.store_response(leadgen_forms[0:2])

class ReachFrequencyPredictionDocsTestCase(DocsTestCase):
    def setUp(self):
        rfp = self.create_reach_frequency_prediction()
        DocsDataStore.set('rfp_id', rfp.get_id())

    def test_reserve(self):
        pass

    def test_cancel(self):
        pass


class TargetingSearchDocsTestCase(DocsTestCase):
    def test_search(self):
        results = TargetingSearch.search(params={
            'q': 'United States',
            'type': TargetingSearch.TargetingSearchTypes.country,
            'limit': 2,
        })
        self.store_response(results)


if __name__ == '__main__':
    handle = open(DocsDataStore.get('filename'), 'w')
    handle.write('')
    handle.close()

    try:
        config_file = open('./config.json')
    except IOError:
        print("No config file found, skipping docs tests")
        sys.exit()
    config = json.load(config_file)
    config_file.close()

    act_id = "1505766289694659"
    FacebookAdsApi.init(
        config['app_id'],
        config['app_secret'],
        config['access_token'],
        'act_' + str(act_id)
    )
    DocsDataStore.set('adaccount_id', 'act_' + str(act_id))
    DocsDataStore.set('adaccount_id_int', act_id)
    DocsDataStore.set('business_id', '1454288444842444')
    DocsDataStore.set('ca_id', '6026172406640')
    DocsDataStore.set('dpa_catalog_id', '447683242047472')
    DocsDataStore.set('dpa_set_id', '808641022536664')
    DocsDataStore.set('dpa_feed_id', '1577689442497017')
    DocsDataStore.set('dpa_upload_id', '1577690399163588')
    DocsDataStore.set('as_user_id', '358829457619128')
    DocsDataStore.set('pixel_id', '417531085081002')
    DocsDataStore.set('page_id', config['page_id'])

    unittest.main()
