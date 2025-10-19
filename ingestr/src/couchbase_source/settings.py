"""Couchbase source settings and constants"""

# Couchbase API request timeout in seconds
REQUEST_TIMEOUT = 300

# Default page size for paginated requests
DEFAULT_PAGE_SIZE = 100

# Maximum page size allowed by Couchbase API
MAX_PAGE_SIZE = 1000

# Rebalance retry settings endpoint path
REBALANCE_RETRY_ENDPOINT = "/settings/retryRebalance"
