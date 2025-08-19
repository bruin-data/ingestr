import asyncio
from typing import Any, Dict, Iterable, Iterator

import aiohttp
import dlt

from .helpers import (
    _make_request,
    _paginate,
    convert_timestamps_to_iso,
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
        project_id: RevenueCat project ID (required for customers, products, entitlements, subscriptions, purchases)

    Returns:
        Iterable of DLT resources for customers, products, entitlements, purchases, subscriptions, and projects
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

    @dlt.resource(name="products", primary_key="id", write_disposition="merge")
    def products() -> Iterator[Dict[str, Any]]:
        """Get list of products."""
        if project_id is None:
            raise ValueError("project_id is required for products resource")
        endpoint = f"/projects/{project_id}/products"

        for product in _paginate(api_key, endpoint):
            product = convert_timestamps_to_iso(product, ["created_at", "updated_at"])
            yield product

    @dlt.resource(name="entitlements", primary_key="id", write_disposition="merge")
    def entitlements() -> Iterator[Dict[str, Any]]:
        """Get list of entitlements."""
        if project_id is None:
            raise ValueError("project_id is required for entitlements resource")
        endpoint = f"/projects/{project_id}/entitlements"

        for entitlement in _paginate(api_key, endpoint):
            entitlement = convert_timestamps_to_iso(entitlement, ["created_at", "updated_at"])
            yield entitlement

    return [
        projects,
        customers,
        products,
        entitlements,
    ]
