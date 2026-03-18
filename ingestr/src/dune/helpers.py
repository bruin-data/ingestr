import time
from typing import Any, Iterator

BASE_URL = "https://api.dune.com/api/v1"


def poll_execution(session, headers: dict, execution_id: str) -> None:
    max_retries = 8640  # Max 12 hours with 5-second intervals
    retry_count = 0
    poll_interval = 5

    while retry_count < max_retries:
        status_response = session.get(
            f"{BASE_URL}/execution/{execution_id}/status",
            headers=headers,
        )
        status_response.raise_for_status()
        status_data = status_response.json()
        state = status_data.get("state")

        if state == "QUERY_STATE_COMPLETED":
            return
        elif state == "QUERY_STATE_FAILED":
            error = status_data.get("error", {})
            error_msg = (
                error.get("message", "Unknown error")
                if isinstance(error, dict)
                else str(error)
            )
            raise ValueError(f"Query execution failed: {error_msg}")
        elif state in ("QUERY_STATE_PENDING", "QUERY_STATE_EXECUTING"):
            time.sleep(poll_interval)
            retry_count += 1
        elif state == "QUERY_STATE_CANCELLED":
            raise ValueError("Query execution was cancelled")
        elif state == "QUERY_STATE_EXPIRED":
            raise ValueError("Query execution expired")
        else:
            raise ValueError(f"Unknown query state: {state}")

    raise TimeoutError(
        f"Query execution timed out after {max_retries * poll_interval} seconds"
    )


def fetch_results(
    session, headers: dict, execution_id: str
) -> Iterator[dict[str, Any]]:
    offset = 0
    page_limit = 1000

    while True:
        params: dict[str, Any] = {
            "limit": page_limit,
            "offset": offset,
        }

        results_response = session.get(
            f"{BASE_URL}/execution/{execution_id}/results",
            headers=headers,
            params=params,
        )
        results_response.raise_for_status()
        results_data = results_response.json()

        result = results_data.get("result", {})
        rows = result.get("rows", [])

        if not rows:
            break

        yield from rows

        next_offset = results_data.get("next_offset")
        if next_offset is None:
            break

        offset = next_offset


def fetch_queries(session, headers: dict) -> Iterator[dict[str, Any]]:
    offset = 0
    page_limit = 100

    while True:
        params: dict[str, Any] = {
            "limit": page_limit,
            "offset": offset,
        }

        response = session.get(
            f"{BASE_URL}/queries",
            headers=headers,
            params=params,
        )
        response.raise_for_status()
        data = response.json()

        rows = data.get("queries", [])
        if not rows:
            break

        yield from rows

        total = data.get("total", 0)
        offset += len(rows)
        if offset >= total:
            break
