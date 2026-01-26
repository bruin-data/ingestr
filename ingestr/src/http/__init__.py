"""HTTP source for reading CSV, JSON, and Parquet files from public URLs"""

from typing import Any, Optional

import dlt
from dlt.sources import DltResource

from .readers import HttpReader


@dlt.source
def http_source(
    url: str,
    file_format: Optional[str] = None,
    column_names: Optional[list[str]] = None,
    **kwargs: Any,
) -> DltResource:
    """Source for reading files from HTTP URLs.

    Supports CSV, JSON, Parquet, and CSV without headers file formats.

    Args:
        url (str): The HTTP(S) URL to the file
        file_format (str, optional): File format ('csv', 'csv_headless', 'json', 'parquet').
            If not provided, will be inferred from URL extension.
        column_names (list[str], optional): Column names for csv_headless format.
            If not provided for csv_headless, columns will be named unknown_col_0, unknown_col_1, etc.
        **kwargs: Additional arguments passed to the reader functions

    Returns:
        DltResource: A dlt resource that yields the file data
    """
    reader = HttpReader(url, file_format, column_names)

    return dlt.resource(
        reader.read_file(**kwargs),
        name="http_data",
    )
