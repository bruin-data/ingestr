from typing import Any, Dict, Iterable, Iterator

import dlt
import pendulum

from .helpers import _make_request, _paginate


@dlt.source(name="revenuecat", max_table_nesting=0)
def revenuecat_source(
    api_key: str,
    project_id: str,
) -> Iterable[dlt.sources.DltResource]:
    """
    RevenueCat source for extracting data from RevenueCat API v2.
    
    Args:
        api_key: RevenueCat API v2 secret key with Bearer token format
        project_id: RevenueCat project ID
    
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
                yield project
    
    @dlt.resource(name="customers", primary_key="id", write_disposition="merge")
    def customers() -> Iterator[Dict[str, Any]]:
        """Get list of customers."""
        endpoint = f"/projects/{project_id}/customers"
        
        for customer in _paginate(api_key, endpoint):
            yield customer
    
    @dlt.resource(name="products", primary_key="id", write_disposition="merge")
    def products() -> Iterator[Dict[str, Any]]:
        """Get list of products."""
        endpoint = f"/projects/{project_id}/products"
        
        for product in _paginate(api_key, endpoint):
            yield product
    
    @dlt.resource(name="subscriptions", primary_key="id", write_disposition="merge")
    def subscriptions() -> Iterator[Dict[str, Any]]:
        """Get list of subscriptions by iterating through customers."""
        customers_endpoint = f"/projects/{project_id}/customers"
        
        # First get all customers, then get their subscriptions
        for customer in _paginate(api_key, customers_endpoint):
            customer_id = customer["id"]
            subscriptions_endpoint = f"/projects/{project_id}/customers/{customer_id}/subscriptions"
            
            for subscription in _paginate(api_key, subscriptions_endpoint):
                subscription["customer_id"] = customer_id
                yield subscription
    
    @dlt.resource(name="purchases", primary_key="id", write_disposition="merge")
    def purchases() -> Iterator[Dict[str, Any]]:
        """Get list of purchases by iterating through customers."""
        customers_endpoint = f"/projects/{project_id}/customers"
        
        # First get all customers, then get their purchases
        for customer in _paginate(api_key, customers_endpoint):
            customer_id = customer["id"]
            purchases_endpoint = f"/projects/{project_id}/customers/{customer_id}/purchases"
            
            for purchase in _paginate(api_key, purchases_endpoint):
                purchase["customer_id"] = customer_id
                yield purchase
    
    return [
        projects,
        customers,
        products,
        subscriptions,
        purchases,
    ]