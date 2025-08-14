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
        """Get list of customers with nested purchases and subscriptions."""
        if project_id is None:
            raise ValueError("project_id is required for customers resource")
        endpoint = f"/projects/{project_id}/customers"
        
        for customer in _paginate(api_key, endpoint):
            customer_id = customer["id"]
            
            # Convert customer timestamps
            customer = convert_timestamps_to_iso(customer, ["first_seen_at", "last_seen_at"])
            
            # If subscriptions not included in customer data, fetch separately
            if "subscriptions" not in customer or customer["subscriptions"] is None:
                subscriptions_endpoint = f"/projects/{project_id}/customers/{customer_id}/subscriptions"
                customer["subscriptions"] = []
                for subscription in _paginate(api_key, subscriptions_endpoint):
                    customer["subscriptions"].append(subscription)
            
            # Convert subscriptions timestamps
            if "subscriptions" in customer and customer["subscriptions"] is not None:
                for subscription in customer["subscriptions"]:
                    subscription = convert_timestamps_to_iso(subscription, ["purchased_at", "expires_at", "grace_period_expires_at"])
            
            # If purchases not included in customer data, fetch separately
            if "purchases" not in customer or customer["purchases"] is None:
                purchases_endpoint = f"/projects/{project_id}/customers/{customer_id}/purchases"
                customer["purchases"] = []
                for purchase in _paginate(api_key, purchases_endpoint):
                    customer["purchases"].append(purchase)
            
            # Convert purchases timestamps
            if "purchases" in customer and customer["purchases"] is not None:
                for purchase in customer["purchases"]:
                    purchase = convert_timestamps_to_iso(purchase, ["purchased_at", "expires_at"])
            
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
    
   
    
    return [
        projects,
        customers,
        products,
       
    ]