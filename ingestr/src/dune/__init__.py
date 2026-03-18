"""
Dune source for data extraction via REST API.

This source provides access to Dune blockchain analytics data via SQL query execution.
"""

from typing import Any, Iterator

import dlt

from ingestr.src.dune.helpers import (
    BASE_URL,
    fetch_queries,
    fetch_results,
    poll_execution,
)
from ingestr.src.http_client import create_client


@dlt.source(max_table_nesting=0, name="dune_source")
def dune_source(
    api_key: str,
    sql: str,
    query_id: str | None = None,
    performance: str = "medium",
    query_parameters: dict[str, Any] | None = None,
) -> Any:
    """
    Dune data source for blockchain analytics data extraction.

    Args:
        api_key: Dune API key for authentication
        sql: The SQL query to execute
        query_id: Optional saved query ID to execute via /query/{id}/execute
        performance: Engine tier - "medium" (default) or "large"
        query_parameters: Optional key-value pairs for parameterized queries
    """
    session = create_client()
    headers = {"X-DUNE-API-KEY": api_key}

    @dlt.resource(
        name="execute_sql",
        write_disposition="replace",
    )
    def execute_sql() -> Iterator[dict[str, Any]]:
        execute_payload: dict[str, Any] = {
            "sql": sql,
            "performance": performance,
        }

        execute_response = session.post(
            f"{BASE_URL}/sql/execute",
            json=execute_payload,
            headers=headers,
        )
        execute_response.raise_for_status()
        execute_data = execute_response.json()

        execution_id = execute_data.get("execution_id")
        if not execution_id:
            raise ValueError(f"Failed to start query execution: {execute_data}")

        poll_execution(session, headers, execution_id)
        yield from fetch_results(session, headers, execution_id)

    @dlt.resource(
        name="execute_query",
        write_disposition="replace",
    )
    def execute_query() -> Iterator[dict[str, Any]]:
        execute_payload: dict[str, Any] = {
            "performance": performance,
        }
        if query_parameters:
            execute_payload["query_parameters"] = query_parameters

        execute_response = session.post(
            f"{BASE_URL}/query/{query_id}/execute",
            json=execute_payload,
            headers=headers,
        )
        execute_response.raise_for_status()
        execute_data = execute_response.json()

        execution_id = execute_data.get("execution_id")
        if not execution_id:
            raise ValueError(f"Failed to start query execution: {execute_data}")

        poll_execution(session, headers, execution_id)
        yield from fetch_results(session, headers, execution_id)

    @dlt.resource(
        name="queries",
        write_disposition="replace",
        primary_key="id",
    )
    def queries() -> Iterator[dict[str, Any]]:
        yield from fetch_queries(session, headers)

    if query_id:
        return execute_query
    if sql:
        return execute_sql
    return (queries,)
