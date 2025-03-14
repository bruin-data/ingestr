import warnings
from typing import Tuple, TypeAlias
from urllib.parse import (
    ParseResult,
    urlparse
)

BucketName: TypeAlias = str
FileGlob: TypeAlias = str


def parse_uri(uri: ParseResult, table: str) -> Tuple[BucketName, FileGlob]:
    """
    parse the URI of a blob storage and
    return the bucket name and the file glob.

    Supports the following Forms:
    - uri: "gs://"
      table: "bucket-name/file-glob"
    - uri: "gs://uri-bucket-name" (uri-bucket-name is preferred)
      table: "gs://table-bucket-name/file-glob"
    - uri: "gs://"
      table: "gs://bucket-name/file-glob"
    - uri: gs://bucket-name/file-glob
      table: None
    - uri: "gs://bucket-name"
      table: "file-glob"

    The first form is the prefered method. Other forms are supported but discouraged.
    """

    table = urlparse(table.strip())
    host = uri.netloc.strip()

    if table == "" or uri.path.strip() != "":
        warnings.warn(
            f"Using the form '{uri.scheme}://bucket-name/file-glob' is deprecated and will be removed in future versions.",
            DeprecationWarning,
            stacklevel=2,
        )
        return host, uri.path.lstrip("/")

    if host != "":
        return host, table.path.lstrip("/")

    if table.hostname:
        return table.hostname, table.path.lstrip("/")
    
    parts = table.path.lstrip("/").split("/", maxsplit=1)
    if len(parts) != 2:
        return "", parts[0]

    return parts[0], parts[1]
