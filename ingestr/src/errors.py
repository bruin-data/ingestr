class MissingValueError(Exception):
    def __init__(self, value, source):
        super().__init__(f"{value} is required to connect to {source}")


class UnsupportedResourceError(Exception):
    def __init__(self, resource, source):
        super().__init__(
            f"Resource '{resource}' is not supported for {source} source yet, if you are interested in it please create a GitHub issue at https://github.com/bruin-data/ingestr"
        )
