import asyncio
from typing import Any, Dict, Iterable, Iterator

import aiohttp
import dlt

from .helpers import (
    _make_request,
    _paginate,
    convert_timestamps_to_iso,
    create_project_resource,
    process_customer_with_nested_resources_async,
)


@dlt.source(name="revenuecat", max_table_nesting=0)
def revenuecat_source(
    api_key: str,
    project_id: str = None,
) -> Iterable[dlt.sources.DltResource]:
    """
    RevenueCat source for extracting data from RevenueCat API v2.

    Args:
        api_key: RevenueCat API v2 secret key with Bearer token format
        project_id: RevenueCat project ID (required for customers, products, entitlements, offerings, subscriptions, purchases)

    Returns:
        Iterable of DLT resources for customers, products, entitlements, offerings, purchases, subscriptions, and projects
    """

    @dlt.resource(name="projects", primary_key="id", write_disposition="merge")
    def projects() -> Iterator[Dict[str, Any]]:
        """Get list of projects."""
        # Get projects list
        data = _make_request(api_key, "/projects")
        if "items" in data:
            for project in data["items"]:
                project = convert_timestamps_to_iso(project, ["created_at"])
                yield project

    @dlt.resource(
        name="customers", primary_key="id", write_disposition="merge", parallelized=True
    )
    def customers() -> Iterator[Dict[str, Any]]:
        """Get list of customers with nested purchases and subscriptions."""
        if project_id is None:
            raise ValueError("project_id is required for customers resource")
        endpoint = f"/projects/{project_id}/customers"

        async def process_customer_batch(customer_batch):
            """Process a batch of customers with async operations."""
            async with aiohttp.ClientSession() as session:
                tasks = []
                for customer in customer_batch:
                    task = process_customer_with_nested_resources_async(
                        session, api_key, project_id, customer
                    )
                    tasks.append(task)

                return await asyncio.gather(*tasks)

        def process_customers_sync():
            """Process customers in batches using asyncio."""
            batch_size = 50  # Conservative batch size due to 60 req/min rate limit
            current_batch = []

            for customer in _paginate(api_key, endpoint):
                current_batch.append(customer)

                if len(current_batch) >= batch_size:
                    # Process the batch asynchronously
                    processed_customers = asyncio.run(
                        process_customer_batch(current_batch)
                    )
                    for processed_customer in processed_customers:
                        yield processed_customer
                    current_batch = []

            # Process any remaining customers in the final batch
            if current_batch:
                processed_customers = asyncio.run(process_customer_batch(current_batch))
                for processed_customer in processed_customers:
                    yield processed_customer

        # Yield each processed customer
        yield from process_customers_sync()

    # Create project-dependent resources dynamically
    project_resources = []
    resource_names = ["products", "entitlements", "offerings"]

    for resource_name in resource_names:

        @dlt.resource(name=resource_name, primary_key="id", write_disposition="merge")
        def create_resource(resource_name=resource_name) -> Iterator[Dict[str, Any]]:
            """Get list of project resource."""
            yield from create_project_resource(resource_name, api_key, project_id)

        # Set the function name for better identification
        create_resource.__name__ = resource_name
        project_resources.append(create_resource)

    return [
        projects,
        customers,
        *project_resources,
    ]
