from typing import Iterable

import dlt
import smartsheet  # type: ignore
from dlt.extract import DltResource
from smartsheet.models.enums import ColumnType  # type: ignore
from smartsheet.models.sheet import Sheet  # type: ignore

TYPE_MAPPING = {
    ColumnType.TEXT_NUMBER: "text",
    ColumnType.DATE: "date",
    ColumnType.DATETIME: "timestamp",
    ColumnType.CONTACT_LIST: "text",
    ColumnType.CHECKBOX: "bool",
    ColumnType.PICKLIST: "text",
    ColumnType.DURATION: "text",
    ColumnType.PREDECESSOR: "text",
    ColumnType.ABSTRACT_DATETIME: "timestamp",
    ColumnType.MULTI_CONTACT_LIST: "text",
    ColumnType.MULTI_PICKLIST: "text",
}


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
    sheet = smartsheet_client.Sheets.get_sheet(sheet_id_int)

    yield dlt.resource(
        _get_sheet_data(sheet),
        name=resource_name,
        columns=_generate_type_hints(sheet),
        write_disposition="replace",
    )


def _get_sheet_data(sheet: Sheet):
    """Helper function to get all rows from a sheet."""

    column_titles = [col.title for col in sheet.columns]
    for row in sheet.rows:
        row_data = {"_row_id": row.id}
        for i, cell in enumerate(row.cells):
            row_data[column_titles[i]] = cell.value
        yield row_data


def _generate_type_hints(sheet: Sheet):
    return {
        col.title: {
            "data_type": TYPE_MAPPING.get(col.type.value),
            "nullable": True,
        }
        for col in sheet.columns
        if col.type.value in TYPE_MAPPING
    }
