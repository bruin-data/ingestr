"""
Allium source for data extraction via REST API.

This source provides access to Allium blockchain data via asynchronous query execution.
"""

import time
from typing import Any, Iterator

import dlt

from ingestr.src.http_client import create_client


@dlt.source(max_table_nesting=0, name="allium_source")
def allium_source(
    api_key: str,
    query_id: str,
    parameters: dict[str, Any] | None = None,
    limit: int | None = None,
    compute_profile: str | None = None,
) -> Any:
    """
    Allium data source for blockchain data extraction.

    This source connects to Allium API, runs async queries, and fetches results.

    Args:
        api_key: Allium API key for authentication
        query_id: The query ID to execute (e.g., 'abc123')
        parameters: Optional parameters for the query (e.g., {'start_date': '2025-02-01', 'end_date': '2025-02-02'})
        limit: Limit the number of rows in the result (max 250,000)
        compute_profile: Compute profile identifier

    Yields:
        DltResource: Data resources for Allium query results
    """
    base_url = "https://api.allium.so/api/v1/explorer"
    session = create_client()
    headers = {"X-API-Key": api_key}

    @dlt.resource(
        name="query_results",
        write_disposition="replace",
    )
    def fetch_query_results() -> Iterator[dict[str, Any]]:
        """
        Fetch query results from Allium.

        This function:
        1. Starts an async query execution
        2. Polls for completion status
        3. Fetches and yields the results
        """
        # Step 1: Start async query execution
        run_config: dict[str, Any] = {}
        if limit is not None:
            run_config["limit"] = limit
        if compute_profile is not None:
            run_config["compute_profile"] = compute_profile

        run_payload = {"parameters": parameters or {}, "run_config": run_config}

        run_response = session.post(
            f"{base_url}/queries/{query_id}/run-async",
            json=run_payload,
            headers=headers,
        )

        run_data = run_response.json()

        if "run_id" not in run_data:
            raise ValueError(f"Failed to start query execution: {run_data}")

        run_id = run_data["run_id"]

        # Step 2: Poll for completion
        max_retries = 8640  # Max 12 hours with 5-second intervals
        retry_count = 0
        poll_interval = 5  # seconds

        while retry_count < max_retries:
            status_response = session.get(
                f"{base_url}/query-runs/{run_id}/status",
                headers=headers,
            )
            status_response.raise_for_status()
            status_data = status_response.json()

            # Handle both string and dict responses
            if isinstance(status_data, str):
                status = status_data
            else:
                status = status_data.get("status")

            if status == "success":
                break
            elif status == "failed":
                error_msg = (
                    status_data.get("error", "Unknown error")
                    if isinstance(status_data, dict)
                    else "Unknown error"
                )
                raise ValueError(f"Query execution failed: {error_msg}")
            elif status in ["pending", "running", "queued"]:
                time.sleep(poll_interval)
                retry_count += 1
            else:
                raise ValueError(f"Unknown status: {status}")

        if retry_count >= max_retries:
            raise TimeoutError(
                f"Query execution timed out after {max_retries * poll_interval} seconds"
            )

        # Step 3: Fetch results
        results_response = session.get(
            f"{base_url}/query-runs/{run_id}/results",
            headers=headers,
            params={"f": "json"},
        )
        results_response.raise_for_status()
        query_output = results_response.json()

        # Extract and yield all data
        yield query_output.get("data", [])

    return (fetch_query_results,)
