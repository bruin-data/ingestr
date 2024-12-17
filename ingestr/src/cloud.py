import re
from typing import Optional
from urllib.parse import parse_qs, ParseResult

AWS_ENDPOINT_PATTERN = re.compile("dynamodb\.(.+)\.amazonaws\.com:443")

def infer_aws_region(uri: ParseResult) -> Optional[str]:

    # try to infer from URI
    matches = AWS_ENDPOINT_PATTERN.match(uri.netloc)
    if matches is not None:
        return matches[1]

    # else obtain region from query string
    region = parse_qs(uri.query).get("region")
    if region is None:
        return None
    return region[0]