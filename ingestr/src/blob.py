import warnings
from typing import Tuple, TypeAlias
from urllib.parse import ParseResult, urlparse

BucketName: TypeAlias = str
FileGlob: TypeAlias = str


class UnsupportedEndpointError(Exception):
    pass


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

    table = table.strip()
    host = uri.netloc.strip()

    if table == "" or uri.path.strip() != "":
        warnings.warn(
            f"Using the form '{uri.scheme}://bucket-name/file-glob' is deprecated and will be removed in future versions.",
            DeprecationWarning,
            stacklevel=2,
        )
        return host, uri.path.lstrip("/")

    table_uri = urlparse(table)

    if host != "":
        return host, table_uri.path.lstrip("/")

    if table_uri.hostname:
        return table_uri.hostname, table_uri.path.lstrip("/")

    parts = table_uri.path.lstrip("/").split("/", maxsplit=1)
    if len(parts) != 2:
        return "", parts[0]

    return parts[0], parts[1]


def parse_endpoint(path: str) -> str:
    """
    Parse the endpoint kind from the URI.

    kind is a file format. one of [csv, jsonl, parquet]
    """
    file_extension = path.split(".")[-1]
    if file_extension == "gz":
        file_extension = path.split(".")[-2]
    if file_extension == "csv":
        endpoint = "read_csv"
    elif file_extension == "jsonl":
        endpoint = "read_jsonl"
    elif file_extension == "parquet":
        endpoint = "read_parquet"
    else:
        raise UnsupportedEndpointError(f"Unsupported file format: {file_extension}")
    return endpoint
