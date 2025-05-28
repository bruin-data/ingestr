from typing import Iterable

import dlt
import smartsheet  # type: ignore
from dlt.extract import DltResource


@dlt.source
def smartsheet_source(
    access_token: str,
    sheet_id: str,
) -> Iterable[DltResource]:
    """
    A DLT source for Smartsheet.

    Args:
        access_token: The Smartsheet API access token.
        sheet_id: The ID of the sheet to load.

    Returns:
        An iterable of DLT resources.
    """

    # Initialize Smartsheet client
    smartsheet_client = smartsheet.Smartsheet(access_token)
    smartsheet_client.errors_as_exceptions(True)

    # The SDK expects sheet_id to be an int
    sheet_id_int = int(sheet_id)
    # Sanitize the sheet name to be a valid resource name
    # We get objectValue to ensure `name` attribute is populated for the sheet
    sheet_details = smartsheet_client.Sheets.get_sheet(
        sheet_id_int, include=["objectValue"]
    )
    sheet_name = sheet_details.name
    resource_name = f"sheet_{sheet_name.replace(' ', '_').lower()}"

    yield dlt.resource(
        _get_sheet_data(smartsheet_client, sheet_id_int),
        name=resource_name,
        write_disposition="replace",
    )


def _get_sheet_data(smartsheet_client: smartsheet.Smartsheet, sheet_id: int):
    """Helper function to get all rows from a sheet."""
    sheet = smartsheet_client.Sheets.get_sheet(sheet_id)
    # Transform rows to a list of dictionaries
    column_titles = [col.title for col in sheet.columns]
    for row in sheet.rows:
        row_data = {}
        for i, cell in enumerate(row.cells):
            row_data[column_titles[i]] = cell.value
        yield row_data
