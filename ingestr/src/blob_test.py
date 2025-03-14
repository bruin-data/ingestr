import pytest

from dataclasses import dataclass
from src.blob import parse_uri
from urllib.parse import urlparse

@dataclass
class URITestCase:
    uri: str
    table: str
    expect_bucket: str
    expect_glob: str

test_cases: list[URITestCase] = [
    URITestCase("s3://", "bucket/file", "bucket", "file"),
    URITestCase("s3://bucket", "file", "bucket", "file"),
    URITestCase("s3://bucket/file", "", "bucket", "file"),
    URITestCase("s3://primary", "s3://secondary/file", "primary", "file"),
    URITestCase("s3://primary", "s3://secondary/path/to/file", "primary", "path/to/file"),
    URITestCase("s3://", "s3://bucket/file", "bucket", "file"),
]

@pytest.mark.parametrize("test_case", test_cases)
def test_parse_uri(test_case: URITestCase):
    uri = urlparse(test_case.uri)
    (bucket, glob) = parse_uri(uri, test_case.table)
    assert bucket == test_case.expect_bucket
    assert glob == test_case.expect_glob