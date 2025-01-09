"""Loads campaigns, ads sets, ads, leads and insight data from Facebook Marketing API"""

from typing import Iterator, Sequence

import dlt
from dlt.common import pendulum
from dlt.common.typing import TDataItems
from dlt.sources import DltResource
from facebook_business.adobjects.ad import Ad

from .helpers import (
    execute_job,
    get_ads_account,
    get_data_chunked,
    get_start_date,
    process_report_item,
)
from .settings import (
    ALL_ACTION_ATTRIBUTION_WINDOWS,
    ALL_ACTION_BREAKDOWNS,
    DEFAULT_AD_FIELDS,
    DEFAULT_ADCREATIVE_FIELDS,
    DEFAULT_ADSET_FIELDS,
    DEFAULT_CAMPAIGN_FIELDS,
    DEFAULT_INSIGHT_FIELDS,
    DEFAULT_LEAD_FIELDS,
    INSIGHT_FIELDS_TYPES,
    INSIGHTS_BREAKDOWNS_OPTIONS,
    INSIGHTS_PRIMARY_KEY,
    INVALID_INSIGHTS_FIELDS,
    TInsightsBreakdownOptions,
    TInsightsLevels,
)


@dlt.source(name="facebook_ads", max_table_nesting=0)
def facebook_ads_source(
    account_id: str = dlt.config.value,
    access_token: str = dlt.secrets.value,
    chunk_size: int = 50,
    request_timeout: float = 300.0,
    app_api_version: str = "v20.0",
) -> Sequence[DltResource]:
    """Returns a list of resources to load campaigns, ad sets, ads, creatives and ad leads data from Facebook Marketing API.

    All the resources have `replace` write disposition by default and define primary keys. Resources are parametrized and allow the user
    to change the set of fields that will be loaded from the API and the object statuses that will be loaded. See the demonstration script for details.

    You can convert the source into merge resource to keep the deleted objects. Currently Marketing API does not return deleted objects. See the demo script.

    We also provide a transformation `enrich_ad_objects` that you can add to any of the resources to get additional data per object via `object.get_api`

    Args:
        account_id (str, optional): Account id associated with add manager. See README.md
        access_token (str, optional): Access token associated with the Business Facebook App. See README.md
        chunk_size (int, optional): A size of the page and batch request. You may need to decrease it if you request a lot of fields. Defaults to 50.
        request_timeout (float, optional): Connection timeout. Defaults to 300.0.
        app_api_version(str, optional): A version of the facebook api required by the app for which the access tokens were issued ie. 'v17.0'. Defaults to the facebook_business library default version

    Returns:
        Sequence[DltResource]: campaigns, ads, ad_sets, ad_creatives, leads
    """
    account = get_ads_account(
        account_id, access_token, request_timeout, app_api_version
    )

    @dlt.resource(primary_key="id", write_disposition="replace")
    def campaigns(
        fields: Sequence[str] = DEFAULT_CAMPAIGN_FIELDS, states: Sequence[str] = None
    ) -> Iterator[TDataItems]:
        yield get_data_chunked(account.get_campaigns, fields, states, chunk_size)

    @dlt.resource(primary_key="id", write_disposition="replace")
    def ads(
        fields: Sequence[str] = DEFAULT_AD_FIELDS, states: Sequence[str] = None
    ) -> Iterator[TDataItems]:
        yield get_data_chunked(account.get_ads, fields, states, chunk_size)

    @dlt.resource(primary_key="id", write_disposition="replace")
    def ad_sets(
        fields: Sequence[str] = DEFAULT_ADSET_FIELDS, states: Sequence[str] = None
    ) -> Iterator[TDataItems]:
        yield get_data_chunked(account.get_ad_sets, fields, states, chunk_size)

    @dlt.transformer(primary_key="id", write_disposition="replace", selected=True)
    def leads(
        items: TDataItems,
        fields: Sequence[str] = DEFAULT_LEAD_FIELDS,
        states: Sequence[str] = None,
    ) -> Iterator[TDataItems]:
        for item in items:
            ad = Ad(item["id"])
            yield get_data_chunked(ad.get_leads, fields, states, chunk_size)

    @dlt.resource(primary_key="id", write_disposition="replace")
    def ad_creatives(
        fields: Sequence[str] = DEFAULT_ADCREATIVE_FIELDS, states: Sequence[str] = None
    ) -> Iterator[TDataItems]:
        yield get_data_chunked(account.get_ad_creatives, fields, states, chunk_size)

    return campaigns, ads, ad_sets, ad_creatives, ads | leads


@dlt.source(name="facebook_ads", max_table_nesting=0)
def facebook_insights_source(
    account_id: str = dlt.config.value,
    access_token: str = dlt.secrets.value,
    initial_load_past_days: int = 1,
    fields: Sequence[str] = DEFAULT_INSIGHT_FIELDS,
    attribution_window_days_lag: int = 7,
    time_increment_days: int = 1,
    breakdowns: TInsightsBreakdownOptions = "ads_insights",
    action_breakdowns: Sequence[str] = ALL_ACTION_BREAKDOWNS,
    level: TInsightsLevels = "ad",
    action_attribution_windows: Sequence[str] = ALL_ACTION_ATTRIBUTION_WINDOWS,
    batch_size: int = 50,
    request_timeout: int = 300,
    app_api_version: str = None,
) -> DltResource:
    """Incrementally loads insight reports with defined granularity level, fields, breakdowns etc.

    By default, the reports are generated one by one for each day, starting with today - attribution_window_days_lag. On subsequent runs, only the reports
    from the last report date until today are loaded (incremental load). The reports from last 7 days (`attribution_window_days_lag`) are refreshed on each load to
    account for changes during attribution window.

    Mind that each report is a job and takes some time to execute.

    Args:
        account_id: str = dlt.config.value,
        access_token: str = dlt.secrets.value,
        initial_load_past_days (int, optional): How many past days (starting from today) to intially load. Defaults to 30.
        fields (Sequence[str], optional): A list of fields to include in each reports. Note that `breakdowns` option adds fields automatically. Defaults to DEFAULT_INSIGHT_FIELDS.
        attribution_window_days_lag (int, optional): Attribution window in days. The reports in attribution window are refreshed on each run.. Defaults to 7.
        time_increment_days (int, optional): The report aggregation window in days. use 7 for weekly aggregation. Defaults to 1.
        breakdowns (TInsightsBreakdownOptions, optional): A presents with common aggregations. See settings.py for details. Defaults to "ads_insights_age_and_gender".
        action_breakdowns (Sequence[str], optional): Action aggregation types. See settings.py for details. Defaults to ALL_ACTION_BREAKDOWNS.
        level (TInsightsLevels, optional): The granularity level. Defaults to "ad".
        action_attribution_windows (Sequence[str], optional): Attribution windows for actions. Defaults to ALL_ACTION_ATTRIBUTION_WINDOWS.
        batch_size (int, optional): Page size when reading data from particular report. Defaults to 50.
        request_timeout (int, optional): Connection timeout. Defaults to 300.
        app_api_version(str, optional): A version of the facebook api required by the app for which the access tokens were issued ie. 'v17.0'. Defaults to the facebook_business library default version

    Returns:
        DltResource: facebook_insights

    """
    account = get_ads_account(
        account_id, access_token, request_timeout, app_api_version
    )

    # we load with a defined lag
    initial_load_start_date = pendulum.today().subtract(days=initial_load_past_days)
    initial_load_start_date_str = initial_load_start_date.isoformat()

    @dlt.resource(
        primary_key=INSIGHTS_PRIMARY_KEY,
        write_disposition="merge",
        columns=INSIGHT_FIELDS_TYPES,
    )
    def facebook_insights(
        date_start: dlt.sources.incremental[str] = dlt.sources.incremental(
            "date_start",
            initial_value=initial_load_start_date_str,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterator[TDataItems]:
        start_date = get_start_date(date_start, attribution_window_days_lag)
        end_date = pendulum.now()

        # fetch insights in incremental day steps
        while start_date <= end_date:
            query = {
                "level": level,
                "action_breakdowns": list(action_breakdowns),
                "breakdowns": list(
                    INSIGHTS_BREAKDOWNS_OPTIONS[breakdowns]["breakdowns"]
                ),
                "limit": batch_size,
                "fields": list(
                    set(fields)
                    .union(INSIGHTS_BREAKDOWNS_OPTIONS[breakdowns]["fields"])
                    .difference(INVALID_INSIGHTS_FIELDS)
                ),
                "time_increment": time_increment_days,
                "action_attribution_windows": list(action_attribution_windows),
                "time_ranges": [
                    {
                        "since": start_date.to_date_string(),
                        "until": start_date.add(
                            days=time_increment_days - 1
                        ).to_date_string(),
                    }
                ],
            }
            job = execute_job(account.get_insights(params=query, is_async=True))
            yield list(map(process_report_item, job.get_result()))
            start_date = start_date.add(days=time_increment_days)

    return facebook_insights
