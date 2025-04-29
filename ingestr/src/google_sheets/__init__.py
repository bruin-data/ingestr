"""Loads Google Sheets data from tabs, named and explicit ranges. Contains the main source functions."""

from typing import Iterable, Sequence, Union

import dlt
from dlt.common import logger
from dlt.sources import DltResource
from dlt.sources.credentials import GcpOAuthCredentials, GcpServiceAccountCredentials

from .helpers import api_calls
from .helpers.api_calls import api_auth
from .helpers.data_processing import (
    get_data_types,
    get_range_headers,
    get_spreadsheet_id,
    process_range,
)


@dlt.source
def google_spreadsheet(
    spreadsheet_url_or_id: str = dlt.config.value,
    range_names: Sequence[str] = dlt.config.value,
    credentials: Union[
        GcpOAuthCredentials, GcpServiceAccountCredentials
    ] = dlt.secrets.value,
    get_sheets: bool = False,
    get_named_ranges: bool = True,
    max_api_retries: int = 5,
) -> Iterable[DltResource]:
    """
    The source for the dlt pipeline. It returns the following resources:
    - 1 dlt resource for every range in range_names.
    - Optionally, dlt resources for all sheets inside the spreadsheet and all named ranges inside the spreadsheet.

    Args:
        spreadsheet_url_or_id (str): The ID or URL of the spreadsheet.
        range_names (Sequence[str]): A list of ranges in the spreadsheet in the format used by Google Sheets. Accepts Named Ranges and Sheets (tabs) names.
            These are the ranges to be converted into tables.
        credentials (Union[GcpServiceAccountCredentials, GcpOAuthCredentials]): GCP credentials to the account
            with Google Sheets API access, defined in dlt.secrets.
        get_sheets (bool, optional): If True, load all the sheets inside the spreadsheet into the database.
            Defaults to False.
        get_named_ranges (bool, optional): If True, load all the named ranges inside the spreadsheet into the database.
            Defaults to True.
        max_api_retries (int, optional): Max number of retires to google sheets API. Actual behavior is internal to google client.

    Yields:
        Iterable[DltResource]: List of dlt resources.
    """
    # authenticate to the service using the helper function
    service = api_auth(credentials, max_api_retries=max_api_retries)
    # get spreadsheet id from url or id
    spreadsheet_id = get_spreadsheet_id(spreadsheet_url_or_id)
    all_range_names = set(range_names or [])
    # if no explicit ranges, get sheets and named ranges from metadata
    # get metadata with list of sheets and named ranges in the spreadsheet
    sheet_names, named_ranges, spreadsheet_title = api_calls.get_known_range_names(
        spreadsheet_id=spreadsheet_id, service=service
    )
    if not range_names:
        if get_sheets:
            all_range_names.update(sheet_names)
        if get_named_ranges:
            all_range_names.update(named_ranges)

    # first we get all data for all the ranges (explicit or named)
    all_range_data = api_calls.get_data_for_ranges(
        service=service,
        spreadsheet_id=spreadsheet_id,
        range_names=list(all_range_names),
    )
    assert len(all_range_names) == len(all_range_data), (
        "Google Sheets API must return values for all requested ranges"
    )

    # get metadata for two first rows of each range
    # first should contain headers
    # second row contains data which we'll use to sample data types.
    # google sheets return datetime and date types as lotus notes serial number. which is just a float so we cannot infer the correct types just from the data

    # warn and remove empty ranges
    range_data = []
    metadata_table = []
    for name, parsed_range, meta_range, values in all_range_data:
        # # pass all ranges to spreadsheet info - including empty
        # metadata_table.append(
        #     {
        #         "spreadsheet_id": spreadsheet_id,
        #         "title": spreadsheet_title,
        #         "range_name": name,
        #         "range": str(parsed_range),
        #         "range_parsed": parsed_range._asdict(),
        #         "skipped": True,
        #     }
        # )
        if values is None or len(values) == 0:
            logger.warning(f"Range {name} does not contain any data. Skipping.")
            continue
        if len(values) == 1:
            logger.warning(f"Range {name} contain only 1 row of data. Skipping.")
            continue
        if len(values[0]) == 0:
            logger.warning(
                f"First row of range {name} does not contain data. Skipping."
            )
            continue
        # metadata_table[-1]["skipped"] = False
        range_data.append((name, parsed_range, meta_range, values))

    meta_values = api_calls.get_meta_for_ranges(
        service, spreadsheet_id, [str(data[2]) for data in range_data]
    )
    for name, parsed_range, _, values in range_data:
        logger.info(f"Processing range {parsed_range} with name {name}")
        # here is a tricky part due to how Google Sheets API returns the metadata. We are not able to directly pair the input range names with returned metadata objects
        # instead metadata objects are grouped by sheet names, still each group order preserves the order of input ranges
        # so for each range we get a sheet name, we look for the metadata group for that sheet and then we consume first object on that list with pop
        metadata = next(
            sheet
            for sheet in meta_values["sheets"]
            if sheet["properties"]["title"] == parsed_range.sheet_name
        )["data"].pop(0)

        headers_metadata = metadata["rowData"][0]["values"]
        headers = get_range_headers(headers_metadata, name)
        if headers is None:
            # generate automatic headers and treat the first row as data
            headers = [f"col_{idx + 1}" for idx in range(len(headers_metadata))]
            data_row_metadata = headers_metadata
            rows_data = values[0:]
            logger.warning(
                f"Using automatic headers. WARNING: first row of the range {name} will be used as data!"
            )
        else:
            # first row contains headers and is skipped
            data_row_metadata = metadata["rowData"][1]["values"]
            rows_data = values[1:]

        data_types = get_data_types(data_row_metadata)

        yield dlt.resource(
            process_range(rows_data, headers=headers, data_types=data_types),
            name=name,
            write_disposition="replace",
        )
    yield dlt.resource(
        metadata_table,
        write_disposition="merge",
        name="spreadsheet_info",
        merge_key="spreadsheet_id",
    )
