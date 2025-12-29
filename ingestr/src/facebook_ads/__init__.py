# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Loads campaigns, ads sets, ads, leads and insight data from Facebook Marketing API"""

from typing import Iterator, Sequence

import dlt
from dlt.common import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItems
from dlt.sources import DltResource
from facebook_business.adobjects.ad import Ad

from .helpers import (
    execute_job,
    get_ads_account,
    get_data_chunked,
    process_report_item,
)
from .settings import (
    ALL_ACTION_ATTRIBUTION_WINDOWS,
    ALL_ACTION_BREAKDOWNS,
    DEFAULT_AD_FIELDS,
    DEFAULT_ADCREATIVE_FIELDS,
    DEFAULT_ADSET_FIELDS,
    DEFAULT_CAMPAIGN_FIELDS,
    DEFAULT_LEAD_FIELDS,
    INSIGHT_FIELDS_TYPES,
    TInsightsLevels,
)


def _create_facebook_insights_resource(
    accounts: list,
    start_date,
    end_date,
    dimensions: Sequence[str],
    fields: Sequence[str],
    level,
    action_breakdowns: Sequence[str],
    batch_size: int,
    time_increment_days: int,
    action_attribution_windows: Sequence[str],
    insights_max_async_sleep_seconds: int,
    insights_max_wait_to_finish_seconds: int,
    insights_max_wait_to_start_seconds: int,
):
    """Create a facebook_insights resource for the given accounts."""
    columns = {}
    for field in fields:
        if field in INSIGHT_FIELDS_TYPES:
            columns[field] = INSIGHT_FIELDS_TYPES[field]

    @dlt.resource(
        write_disposition="merge",
        merge_key="date_start",
        columns=columns,
    )
    def facebook_insights(
        date_start: dlt.sources.incremental[str] = dlt.sources.incremental(
            "date_start",
            initial_value=ensure_pendulum_datetime(start_date).start_of("day").date(),
            end_value=ensure_pendulum_datetime(end_date).end_of("day").date()
            if end_date
            else None,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterator[TDataItems]:
        current_start_date = date_start.last_value
        if date_start.end_value:
            end_date_val = pendulum.instance(date_start.end_value)
            current_end_date = (
                end_date_val
                if isinstance(end_date_val, pendulum.Date)
                else end_date_val.date()
            )
        else:
            current_end_date = pendulum.now().date()

        while current_start_date <= current_end_date:
            query = {
                "level": level,
                "action_breakdowns": list(action_breakdowns),
                "breakdowns": dimensions,
                "limit": batch_size,
                "fields": fields,
                "time_increment": time_increment_days,
                "action_attribution_windows": list(action_attribution_windows),
                "time_ranges": [
                    {
                        "since": current_start_date.to_date_string(),
                        "until": current_start_date.add(
                            days=time_increment_days - 1
                        ).to_date_string(),
                    }
                ],
            }
            for account in accounts:
                job = execute_job(
                    account.get_insights(params=query, is_async=True),
                    insights_max_async_sleep_seconds=insights_max_async_sleep_seconds,
                    insights_max_wait_to_finish_seconds=insights_max_wait_to_finish_seconds,
                    insights_max_wait_to_start_seconds=insights_max_wait_to_start_seconds,
                )
                output = list(map(process_report_item, job.get_result()))
                yield output
            current_start_date = current_start_date.add(days=time_increment_days)

    return facebook_insights


@dlt.source(name="facebook_ads", max_table_nesting=0)
def facebook_ads_source(
    account_id: str | list[str] = dlt.config.value,
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
        account_id (str | list[str], optional): Account id(s) associated with ad manager. Can be a single ID or a list of IDs. See README.md
        access_token (str, optional): Access token associated with the Business Facebook App. See README.md
        chunk_size (int, optional): A size of the page and batch request. You may need to decrease it if you request a lot of fields. Defaults to 50.
        request_timeout (float, optional): Connection timeout. Defaults to 300.0.
        app_api_version(str, optional): A version of the facebook api required by the app for which the access tokens were issued ie. 'v17.0'. Defaults to the facebook_business library default version

    Returns:
        Sequence[DltResource]: campaigns, ads, ad_sets, ad_creatives, leads
    """
    account_ids = account_id if isinstance(account_id, list) else [account_id]
    accounts = [
        get_ads_account(acc_id, access_token, request_timeout, app_api_version)
        for acc_id in account_ids
    ]

    @dlt.resource(primary_key="id", write_disposition="replace")
    def campaigns(
        fields: Sequence[str] = DEFAULT_CAMPAIGN_FIELDS, states: Sequence[str] = None
    ) -> Iterator[TDataItems]:
        for account in accounts:
            yield get_data_chunked(account.get_campaigns, fields, states, chunk_size)

    @dlt.resource(primary_key="id", write_disposition="replace")
    def ads(
        fields: Sequence[str] = DEFAULT_AD_FIELDS, states: Sequence[str] = None
    ) -> Iterator[TDataItems]:
        for account in accounts:
            yield get_data_chunked(account.get_ads, fields, states, chunk_size)

    @dlt.resource(primary_key="id", write_disposition="replace")
    def ad_sets(
        fields: Sequence[str] = DEFAULT_ADSET_FIELDS, states: Sequence[str] = None
    ) -> Iterator[TDataItems]:
        for account in accounts:
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
        for account in accounts:
            yield get_data_chunked(account.get_ad_creatives, fields, states, chunk_size)

    return campaigns, ads, ad_sets, ad_creatives, ads | leads


@dlt.source(name="facebook_ads", max_table_nesting=0)
def facebook_insights_source(
    account_id: str = dlt.config.value,
    access_token: str = dlt.secrets.value,
    initial_load_past_days: int = 1,
    dimensions: Sequence[str] = None,
    fields: Sequence[str] = None,
    time_increment_days: int = 1,
    action_breakdowns: Sequence[str] = ALL_ACTION_BREAKDOWNS,
    level: TInsightsLevels | None = "ad",
    action_attribution_windows: Sequence[str] = ALL_ACTION_ATTRIBUTION_WINDOWS,
    batch_size: int = 50,
    request_timeout: int = 300,
    app_api_version: str = None,
    start_date: pendulum.DateTime | None = None,
    end_date: pendulum.DateTime | None = None,
    insights_max_wait_to_finish_seconds: int = 60 * 60 * 4,
    insights_max_wait_to_start_seconds: int = 60 * 30,
    insights_max_async_sleep_seconds: int = 20,
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

    if start_date is None:
        start_date = pendulum.today().subtract(days=initial_load_past_days)

    if dimensions is None:
        dimensions = []
    if fields is None:
        fields = []

    return _create_facebook_insights_resource(
        accounts=[account],
        start_date=start_date,
        end_date=end_date,
        dimensions=dimensions,
        fields=fields,
        level=level,
        action_breakdowns=action_breakdowns,
        batch_size=batch_size,
        time_increment_days=time_increment_days,
        action_attribution_windows=action_attribution_windows,
        insights_max_async_sleep_seconds=insights_max_async_sleep_seconds,
        insights_max_wait_to_finish_seconds=insights_max_wait_to_finish_seconds,
        insights_max_wait_to_start_seconds=insights_max_wait_to_start_seconds,
    )


@dlt.source(name="facebook_ads", max_table_nesting=0)
def facebook_insights_with_account_ids_source(
    account_ids: list[str],
    access_token: str = dlt.secrets.value,
    initial_load_past_days: int = 1,
    dimensions: Sequence[str] = None,
    fields: Sequence[str] = None,
    time_increment_days: int = 1,
    action_breakdowns: Sequence[str] = ALL_ACTION_BREAKDOWNS,
    level: TInsightsLevels | None = "ad",
    action_attribution_windows: Sequence[str] = ALL_ACTION_ATTRIBUTION_WINDOWS,
    batch_size: int = 50,
    request_timeout: int = 300,
    app_api_version: str = None,
    start_date: pendulum.DateTime | None = None,
    end_date: pendulum.DateTime | None = None,
    insights_max_wait_to_finish_seconds: int = 60 * 60 * 4,
    insights_max_wait_to_start_seconds: int = 60 * 30,
    insights_max_async_sleep_seconds: int = 20,
) -> DltResource:
    """Incrementally loads insight reports for multiple account IDs.

    Args:
        account_ids (list[str]): List of account IDs to fetch insights for.
        access_token (str): Access token associated with the Business Facebook App.
        initial_load_past_days (int, optional): How many past days to initially load. Defaults to 1.
        dimensions (Sequence[str], optional): Breakdown dimensions.
        fields (Sequence[str], optional): Fields to include in reports.
        time_increment_days (int, optional): Report aggregation window in days. Defaults to 1.
        action_breakdowns (Sequence[str], optional): Action aggregation types.
        level (TInsightsLevels, optional): Granularity level. Defaults to "ad".
        action_attribution_windows (Sequence[str], optional): Attribution windows for actions.
        batch_size (int, optional): Page size. Defaults to 50.
        request_timeout (int, optional): Connection timeout. Defaults to 300.
        app_api_version (str, optional): Facebook API version.
        start_date: Start date for insights.
        end_date: End date for insights.

    Returns:
        DltResource: facebook_insights
    """
    accounts = [
        get_ads_account(acc_id, access_token, request_timeout, app_api_version)
        for acc_id in account_ids
    ]

    if start_date is None:
        start_date = pendulum.today().subtract(days=initial_load_past_days)

    if dimensions is None:
        dimensions = []
    if fields is None:
        fields = []

    return _create_facebook_insights_resource(
        accounts=accounts,
        start_date=start_date,
        end_date=end_date,
        dimensions=dimensions,
        fields=fields,
        level=level,
        action_breakdowns=action_breakdowns,
        batch_size=batch_size,
        time_increment_days=time_increment_days,
        action_attribution_windows=action_attribution_windows,
        insights_max_async_sleep_seconds=insights_max_async_sleep_seconds,
        insights_max_wait_to_finish_seconds=insights_max_wait_to_finish_seconds,
        insights_max_wait_to_start_seconds=insights_max_wait_to_start_seconds,
    )
