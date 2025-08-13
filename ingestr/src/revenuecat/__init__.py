from typing import Any, Dict, Iterable, Iterator

import dlt

from .helpers import _make_request, _paginate, convert_timestamps_to_iso


@dlt.source(name="revenuecat", max_table_nesting=0)
def revenuecat_source(
    api_key: str,
    project_id: str = None,
) -> Iterable[dlt.sources.DltResource]:
    """
    RevenueCat source for extracting data from RevenueCat API v2.
    
    Args:
        api_key: RevenueCat API v2 secret key with Bearer token format
        project_id: RevenueCat project ID (required for customers, products, subscriptions, purchases)
    
    Returns:
        Iterable of DLT resources for customers, products, purchases, subscriptions, and projects
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
    
    @dlt.resource(name="customers", primary_key="id", write_disposition="merge")
    def customers() -> Iterator[Dict[str, Any]]:
        """Get list of customers."""
        if project_id is None:
            raise ValueError("project_id is required for customers resource")
        endpoint = f"/projects/{project_id}/customers"
        
        for customer in _paginate(api_key, endpoint):
            customer = convert_timestamps_to_iso(customer, ["first_seen_at", "last_seen_at"])
            yield customer
    
    @dlt.resource(name="products", primary_key="id", write_disposition="merge")
    def products() -> Iterator[Dict[str, Any]]:
        """Get list of products."""
        if project_id is None:
            raise ValueError("project_id is required for products resource")
        endpoint = f"/projects/{project_id}/products"
        
        for product in _paginate(api_key, endpoint):
            product = convert_timestamps_to_iso(product, ["created_at", "updated_at"])
            yield product
    
    @dlt.resource(name="subscriptions", primary_key="id", write_disposition="merge")
    def subscriptions() -> Iterator[Dict[str, Any]]:
        """Get list of subscriptions by iterating through customers."""
        if project_id is None:
            raise ValueError("project_id is required for subscriptions resource")
        customers_endpoint = f"/projects/{project_id}/customers"
        
        # First get all customers, then get their subscriptions
        for customer in _paginate(api_key, customers_endpoint):
            customer_id = customer["id"]
            subscriptions_endpoint = f"/projects/{project_id}/customers/{customer_id}/subscriptions"
            
            for subscription in _paginate(api_key, subscriptions_endpoint):
                subscription["customer_id"] = customer_id
                subscription = convert_timestamps_to_iso(subscription, ["purchased_at", "expires_at", "grace_period_expires_at"])
                yield subscription
    
    @dlt.resource(name="purchases", primary_key="id", write_disposition="merge")
    def purchases() -> Iterator[Dict[str, Any]]:
        """Get list of purchases by iterating through customers."""
        if project_id is None:
            raise ValueError("project_id is required for purchases resource")
        customers_endpoint = f"/projects/{project_id}/customers"
        
        # First get all customers, then get their purchases
        for customer in _paginate(api_key, customers_endpoint):
            customer_id = customer["id"]
            purchases_endpoint = f"/projects/{project_id}/customers/{customer_id}/purchases"
            
            for purchase in _paginate(api_key, purchases_endpoint):
                purchase["customer_id"] = customer_id
                purchase = convert_timestamps_to_iso(purchase, ["purchased_at", "expires_at"])
                yield purchase
    
    return [
        projects,
        customers,
        products,
        subscriptions,
        purchases,
    ]