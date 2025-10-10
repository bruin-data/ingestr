import requests


class MissingValueError(Exception):
    def __init__(self, value, source):
        super().__init__(f"{value} is required to connect to {source}")


class UnsupportedResourceError(Exception):
    def __init__(self, resource, source):
        super().__init__(
            f"Resource '{resource}' is not supported for {source} source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
        )


class InvalidBlobTableError(Exception):
    def __init__(self, source):
        super().__init__(
            f"Invalid source table for {source} "
            "Ensure that the table is in the format {bucket-name}/{file glob}"
        )


class HTTPError(Exception):
    def __init__(self, source: requests.HTTPError):
        super().__init__(f"HTTP {source.response.status_code}: {source.response.text}")
