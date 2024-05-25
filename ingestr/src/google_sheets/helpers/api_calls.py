"""Contains helper functions to extract data from spreadsheet API"""

from typing import Any, List, Tuple

from dlt.common.exceptions import MissingDependencyException
from dlt.common.typing import DictStrAny
from dlt.sources.credentials import GcpCredentials, GcpOAuthCredentials
from dlt.sources.helpers.requests.retry import DEFAULT_RETRY_STATUS
from tenacity import retry, retry_if_exception, stop_after_attempt, wait_exponential

from .data_processing import ParsedRange, trim_range_top_left

try:
    from apiclient.discovery import Resource, build
except ImportError:
    raise MissingDependencyException("Google API Client", ["google-api-python-client"])


def is_retry_status_code(exception: BaseException) -> bool:
    """Retry condition on HttpError"""
    from googleapiclient.errors import HttpError  # type: ignore

    # print(f"RETRY ON {str(HttpError)} = {isinstance(exception, HttpError) and exception.resp.status in DEFAULT_RETRY_STATUS}")
    # if isinstance(exception, HttpError):
    #     print(exception.resp.status)
    #     print(DEFAULT_RETRY_STATUS)
    return (
        isinstance(exception, HttpError)
        and exception.resp.status in DEFAULT_RETRY_STATUS
    )


retry_deco = retry(
    # Retry if it's a rate limit error (HTTP 429)
    retry=retry_if_exception(is_retry_status_code),
    # Use exponential backoff for the waiting time between retries, starting with 5 seconds
    wait=wait_exponential(multiplier=1.5, min=5, max=120),
    # Stop retrying after 10 attempts
    stop=stop_after_attempt(10),
    # Print out the retrying details
    reraise=True,
)


def api_auth(credentials: GcpCredentials, max_api_retries: int) -> Resource:
    """
    Uses GCP credentials to authenticate with Google Sheets API.

    Args:
        credentials (GcpCredentials): Credentials needed to log in to GCP.
        max_api_retries (int): Max number of retires to google sheets API. Actual behavior is internal to google client.

    Returns:
        Resource: Object needed to make API calls to Google Sheets API.
    """
    if isinstance(credentials, GcpOAuthCredentials):
        credentials.auth("https://www.googleapis.com/auth/spreadsheets.readonly")
    # Build the service object for Google sheets api.
    service = build(
        "sheets",
        "v4",
        credentials=credentials.to_native_credentials(),
        num_retries=max_api_retries,
    )
    return service


@retry_deco
def get_meta_for_ranges(
    service: Resource, spreadsheet_id: str, range_names: List[str]
) -> Any:
    """Retrieves `spreadsheet_id` cell metadata for `range_names`"""
    return (
        service.spreadsheets()
        .get(
            spreadsheetId=spreadsheet_id,
            ranges=range_names,
            includeGridData=True,
        )
        .execute()
    )


@retry_deco
def get_known_range_names(
    spreadsheet_id: str, service: Resource
) -> Tuple[List[str], List[str], str]:
    """
    Retrieves spreadsheet metadata and extracts a list of sheet names and named ranges

    Args:
        spreadsheet_id (str): The ID of the spreadsheet.
        service (Resource): Resource object used to make API calls to Google Sheets API.

    Returns:
        Tuple[List[str], List[str], str] sheet names, named ranges, spreadheet title
    """
    metadata = service.spreadsheets().get(spreadsheetId=spreadsheet_id).execute()
    sheet_names: List[str] = [s["properties"]["title"] for s in metadata["sheets"]]
    named_ranges: List[str] = [r["name"] for r in metadata.get("namedRanges", {})]
    title: str = metadata["properties"]["title"]
    return sheet_names, named_ranges, title


@retry_deco
def get_data_for_ranges(
    service: Resource, spreadsheet_id: str, range_names: List[str]
) -> List[Tuple[str, ParsedRange, ParsedRange, List[List[Any]]]]:
    """
    Calls Google Sheets API to get data in a batch. This is the most efficient way to get data for multiple ranges inside a spreadsheet.

    Args:
        service (Resource): Object to make API calls to Google Sheets.
        spreadsheet_id (str): The ID of the spreadsheet.
        range_names (List[str]): List of range names.

    Returns:
        List[DictStrAny]: A list of ranges with data in the same order as `range_names`
    """
    range_batch_resp = (
        service.spreadsheets()
        .values()
        .batchGet(
            spreadsheetId=spreadsheet_id,
            ranges=range_names,
            # un formatted returns typed values
            valueRenderOption="UNFORMATTED_VALUE",
            # will return formatted dates as a serial number
            dateTimeRenderOption="SERIAL_NUMBER",
        )
        .execute()
    )
    # if there are not ranges to be loaded, there's no "valueRanges"
    range_batch: List[DictStrAny] = range_batch_resp.get("valueRanges", [])
    # trim the empty top rows and columns from the left
    rv = []
    for name, range_ in zip(range_names, range_batch):
        parsed_range = ParsedRange.parse_range(range_["range"])
        values: List[List[Any]] = range_.get("values", None)
        if values:
            parsed_range, values = trim_range_top_left(parsed_range, values)
        # create a new range to get first two rows
        meta_range = parsed_range._replace(end_row=parsed_range.start_row + 1)
        # print(f"{name}:{parsed_range}:{meta_range}")
        rv.append((name, parsed_range, meta_range, values))
    return rv
