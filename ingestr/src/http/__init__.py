"""HTTP source for reading CSV, JSON, and Parquet files from public URLs"""

from typing import Any, Optional

import dlt
from dlt.sources import DltResource

from .readers import HttpReader


@dlt.source
def http_source(
    url: str,
    file_format: Optional[str] = None,
    **kwargs: Any,
) -> DltResource:
    """Source for reading files from HTTP URLs.

    Supports CSV, JSON, and Parquet file formats.

    Args:
        url (str): The HTTP(S) URL to the file
        file_format (str, optional): File format ('csv', 'json', 'parquet').
            If not provided, will be inferred from URL extension.
        **kwargs: Additional arguments passed to the reader functions

    Returns:
        DltResource: A dlt resource that yields the file data
    """
    reader = HttpReader(url, file_format)

    return dlt.resource(
        reader.read_file(**kwargs),
        name="http_data",
    )
