from typing import Any, Dict, Iterable, Iterator

import dlt
import pendulum

from .helpers import _make_request, _paginate


@dlt.source(name="revenuecat", max_table_nesting=0)
def revenuecat_source(
    api_key: str,
    project_id: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None = None,
) -> Iterable[dlt.sources.DltResource]:
    """
    RevenueCat source for extracting data from RevenueCat API v2.
    
    Args:
        api_key: RevenueCat API v2 secret key with Bearer token format
        project_id: RevenueCat project ID
        start_date: Start date for data extraction
        end_date: End date for data extraction (optional)
    
    Returns:
        Iterable of DLT resources for customers, products, purchases, subscriptions, and projects
    """
    
    @dlt.resource(name="projects", primary_key="id", write_disposition="merge")
    def projects(
        updated_at: dlt.sources.incremental[int] = dlt.sources.incremental(
            "created_at",
            initial_value=int(start_date.timestamp() * 1000),
            end_value=int(end_date.timestamp() * 1000) if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        """Get list of projects."""
        # Get projects list
        data = _make_request(api_key, "/projects")
        
        if "items" in data:
            for project in data["items"]:
                # RevenueCat API returns created_at as integer timestamp in milliseconds
                if "created_at" in project and updated_at.start_value is not None:
                    if project["created_at"] >= updated_at.start_value:
                        if updated_at.end_value is None or project["created_at"] <= updated_at.end_value:
                            yield project
                else:
                    yield project
    
    @dlt.resource(name="customers", primary_key="id", write_disposition="merge")
    def customers(
        updated_at: dlt.sources.incremental[int] = dlt.sources.incremental(
            "first_seen_at",
            initial_value=int(start_date.timestamp() * 1000),
            end_value=int(end_date.timestamp() * 1000) if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        """Get list of customers."""
        endpoint = f"/projects/{project_id}/customers"
        
        for customer in _paginate(api_key, endpoint):
         
            # RevenueCat API returns first_seen_at as integer timestamp in milliseconds
            if "first_seen_at" in customer and updated_at.start_value is not None:
                if customer["first_seen_at"] >= updated_at.start_value:
                    if updated_at.end_value is None or customer["first_seen_at"] <= updated_at.end_value:
                        yield customer
            else:
                yield customer
    
    @dlt.resource(name="products", primary_key="id", write_disposition="merge")
    def products(
        updated_at: dlt.sources.incremental[int] = dlt.sources.incremental(
            "created_at",
            initial_value=int(start_date.timestamp() * 1000),
            end_value=int(end_date.timestamp() * 1000) if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        """Get list of products."""
        endpoint = f"/projects/{project_id}/products"
        
        for product in _paginate(api_key, endpoint):
            # RevenueCat API returns created_at as integer timestamp in milliseconds
            if "created_at" in product and updated_at.start_value is not None:
                created_at = int(product["created_at"]) if isinstance(product["created_at"], str) else product["created_at"]
                if created_at >= updated_at.start_value:
                    if updated_at.end_value is None or created_at <= updated_at.end_value:
                        yield product
            else:
                yield product
    
    @dlt.resource(name="subscriptions", primary_key="id", write_disposition="merge")
    def subscriptions(
        updated_at: dlt.sources.incremental[int] = dlt.sources.incremental(
            "purchased_at",
            initial_value=int(start_date.timestamp() * 1000),
            end_value=int(end_date.timestamp() * 1000) if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        """Get list of subscriptions by iterating through customers."""
        customers_endpoint = f"/projects/{project_id}/customers"
        
        # First get all customers, then get their subscriptions
        for customer in _paginate(api_key, customers_endpoint):
            customer_id = customer["id"]
            subscriptions_endpoint = f"/projects/{project_id}/customers/{customer_id}/subscriptions"
            
            for subscription in _paginate(api_key, subscriptions_endpoint):
                # RevenueCat API returns purchased_at as integer timestamp in milliseconds
                if "purchased_at" in subscription and updated_at.start_value is not None:
                    if subscription["purchased_at"] >= updated_at.start_value:
                        if updated_at.end_value is None or subscription["purchased_at"] <= updated_at.end_value:
                            # Add customer_id to subscription for reference
                            subscription["customer_id"] = customer_id
                            yield subscription
                else:
                    subscription["customer_id"] = customer_id
                    yield subscription
    
    @dlt.resource(name="purchases", primary_key="id", write_disposition="merge")
    def purchases(
        updated_at: dlt.sources.incremental[int] = dlt.sources.incremental(
            "purchased_at",
            initial_value=int(start_date.timestamp() * 1000),
            end_value=int(end_date.timestamp() * 1000) if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        """Get list of purchases by iterating through customers."""
        customers_endpoint = f"/projects/{project_id}/customers"
        
        # First get all customers, then get their purchases
        for customer in _paginate(api_key, customers_endpoint):
            customer_id = customer["id"]
            purchases_endpoint = f"/projects/{project_id}/customers/{customer_id}/purchases"
            
            for purchase in _paginate(api_key, purchases_endpoint):
                # RevenueCat API returns purchased_at as integer timestamp in milliseconds
                if "purchased_at" in purchase and updated_at.start_value is not None:
                    if purchase["purchased_at"] >= updated_at.start_value:
                        if updated_at.end_value is None or purchase["purchased_at"] <= updated_at.end_value:
                            # Add customer_id to purchase for reference
                            purchase["customer_id"] = customer_id
                            yield purchase
                else:
                    purchase["customer_id"] = customer_id
                    yield purchase
    
    return [
        projects,
        customers,
        products,
        subscriptions,
        purchases,
    ]