from typing import Any, Dict, Iterable, Iterator

import dlt
import pendulum

from .helpers import (
    _get_account,
    _get_campaign_budget,
    _get_campaign_details,
    _get_campaign_jobs,
    _get_campaign_properties,
    _get_campaign_stats,
    _get_oauth_token,
    _get_traffic_report,
    _paginate_campaigns,
)


@dlt.source(name="indeed", max_table_nesting=0)
def indeed_source(
    client_id: str,
    client_secret: str,
    employer_id: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None = None,
) -> Iterable[dlt.sources.DltResource]:
    token = _get_oauth_token(client_id, client_secret, employer_id)

    @dlt.resource(name="campaigns", write_disposition="replace")
    def campaigns() -> Iterator[Dict[str, Any]]:
        for campaign in _paginate_campaigns(token):
            yield campaign

    @dlt.transformer(
        name="campaign_details",
        write_disposition="replace",
        data_from=campaigns,
        parallelized=True,
    )
    def campaign_details(campaign: Dict[str, Any]) -> Iterator[Dict[str, Any]]:
        details = _get_campaign_details(token, campaign["Id"])
        yield details

    @dlt.transformer(
        name="campaign_budget",
        write_disposition="replace",
        data_from=campaigns,
        parallelized=True,
    )
    def campaign_budget(campaign: Dict[str, Any]) -> Iterator[Dict[str, Any]]:
        budget = _get_campaign_budget(token, campaign["Id"])
        if budget:
            yield budget

    @dlt.transformer(
        name="campaign_jobs",
        write_disposition="replace",
        data_from=campaigns,
        parallelized=True,
    )
    def campaign_jobs(campaign: Dict[str, Any]) -> Iterator[Dict[str, Any]]:
        for job in _get_campaign_jobs(token, campaign["Id"]):
            yield job

    @dlt.transformer(
        name="campaign_properties",
        write_disposition="replace",
        data_from=campaigns,
        parallelized=True,
    )
    def campaign_properties(campaign: Dict[str, Any]) -> Iterator[Dict[str, Any]]:
        props = _get_campaign_properties(token, campaign["Id"])
        if props:
            yield props

    @dlt.transformer(
        name="campaign_stats",
        write_disposition="merge",
        merge_key="Date",
        data_from=campaigns,
        parallelized=True,
    )
    def campaign_stats(
        campaign: Dict[str, Any],
        date: dlt.sources.incremental[str] = dlt.sources.incremental(
            "Date",
            initial_value=start_date.to_date_string(),
            end_value=end_date.to_date_string() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start = date.last_value or start_date.to_date_string()
        current_end = date.end_value or pendulum.now("UTC").to_date_string()

        for stat in _get_campaign_stats(
            token, campaign["Id"], current_start, current_end
        ):
            yield stat

    @dlt.resource(
        name="account",
        write_disposition="replace",
    )
    def account() -> Iterator[Dict[str, Any]]:
        data = _get_account(token)
        employer_id = data.get("employerId")
        contact = data.get("contact")
        company = data.get("company")
        email = data.get("email")

        for job_source in data.get("jobSourceList", []):
            yield {
                "employerId": employer_id,
                "contact": contact,
                "company": company,
                "email": email,
                "jobSourceId": job_source.get("id"),
                "jobSourceSiteName": job_source.get("siteName"),
            }

    @dlt.resource(name="traffic_stats", write_disposition="merge", merge_key="date")
    def traffic_stats(
        date: dlt.sources.incremental[str] = dlt.sources.incremental(
            "date",
            initial_value=start_date.to_date_string(),
            end_value=end_date.to_date_string() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start = date.last_value or start_date.to_date_string()
        current_end = date.end_value or pendulum.now("UTC").to_date_string()

        for row in _get_traffic_report(token, current_start, current_end):
            yield row

    return [
        campaigns,
        campaign_details,
        campaign_budget,
        campaign_jobs,
        campaign_properties,
        campaign_stats,
        account,
        traffic_stats,
    ]
