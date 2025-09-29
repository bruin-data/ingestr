"""
Configuration settings and constants for Intercom API integration.
"""

from datetime import datetime
from typing import Dict, List, Tuple

# API Version - REQUIRED for all requests
API_VERSION = "2.14"

# Default start date for incremental loading
DEFAULT_START_DATE = datetime(2020, 1, 1)

# Pagination settings
DEFAULT_PAGE_SIZE = 150
MAX_PAGE_SIZE = 150  # Intercom's maximum
SCROLL_EXPIRY_SECONDS = 60  # Scroll sessions expire after 1 minute

# Rate limiting settings
RATE_LIMIT_PER_10_SECONDS = 166
RATE_LIMIT_RETRY_AFTER_DEFAULT = 10

# Regional API endpoints
REGIONAL_ENDPOINTS = {
    "us": "https://api.intercom.io",
    "eu": "https://api.eu.intercom.io",
    "au": "https://api.au.intercom.io",
}

# Resource configuration for automatic generation
# Format: resource_name -> config dict
RESOURCE_CONFIGS = {
    # Search-based incremental resources
    "contacts": {
        "type": "search",
        "incremental": True,
        "transform_func": "transform_contact",
        "columns": {
            "custom_attributes": {"data_type": "json"},
            "tags": {"data_type": "json"},
        },
    },
    "conversations": {
        "type": "search",
        "incremental": True,
        "transform_func": "transform_conversation",
        "columns": {
            "custom_attributes": {"data_type": "json"},
            "tags": {"data_type": "json"},
        },
    },
    # Pagination-based incremental resources
    "companies": {
        "type": "pagination",
        "endpoint": "/companies",
        "data_key": "data",
        "pagination_type": "cursor",
        "incremental": True,
        "transform_func": "transform_company",
        "params": {"per_page": 50},
        "columns": {
            "custom_attributes": {"data_type": "json"},
            "tags": {"data_type": "json"},
        },
    },
    "articles": {
        "type": "pagination",
        "endpoint": "/articles",
        "data_key": "data",
        "pagination_type": "cursor",
        "incremental": True,
        "transform_func": None,
        "params": None,
        "columns": {},
    },
    # Special case - tickets
    "tickets": {
        "type": "tickets",
        "incremental": True,
        "transform_func": None,
        "columns": {
            "ticket_attributes": {"data_type": "json"},
        },
    },
    # Simple replace resources (non-incremental)
    "tags": {
        "type": "simple",
        "endpoint": "/tags",
        "data_key": "data",
        "pagination_type": "simple",
        "incremental": False,
        "transform_func": None,
        "columns": {},
    },
    "segments": {
        "type": "simple",
        "endpoint": "/segments",
        "data_key": "segments",
        "pagination_type": "cursor",
        "incremental": False,
        "transform_func": None,
        "columns": {},
    },
    "teams": {
        "type": "simple",
        "endpoint": "/teams",
        "data_key": "teams",
        "pagination_type": "simple",
        "incremental": False,
        "transform_func": None,
        "columns": {},
    },
    "admins": {
        "type": "simple",
        "endpoint": "/admins",
        "data_key": "admins",
        "pagination_type": "simple",
        "incremental": False,
        "transform_func": None,
        "columns": {},
    },
    "data_attributes": {
        "type": "simple",
        "endpoint": "/data_attributes",
        "data_key": "data",
        "pagination_type": "cursor",
        "incremental": False,
        "transform_func": None,
        "columns": {
            "id": {"data_type": "bigint", "nullable": True},
        },
    },
}

# Core endpoints with their configuration (kept for backwards compatibility)
# Format: (endpoint_path, data_key, supports_incremental, pagination_type)
CORE_ENDPOINTS: Dict[str, Tuple[str, str, bool, str]] = {
    "contacts": ("/contacts", "data", True, "cursor"),
    "companies": ("/companies", "data", True, "cursor"),
    "conversations": ("/conversations", "conversations", True, "cursor"),
    "tickets": ("/tickets", "tickets", True, "cursor"),
    "admins": ("/admins", "admins", False, "simple"),
    "teams": ("/teams", "teams", False, "simple"),
    "tags": ("/tags", "data", False, "simple"),
    "segments": ("/segments", "segments", False, "cursor"),
    "articles": ("/articles", "data", True, "cursor"),
    "collections": ("/help_center/collections", "data", False, "cursor"),
    "data_attributes": ("/data_attributes", "data", False, "cursor"),
}

# Incremental endpoints using search API
SEARCH_ENDPOINTS: Dict[str, str] = {
    "contacts_search": "/contacts/search",
    "companies_search": "/companies/search",
    "conversations_search": "/conversations/search",
}

# Special endpoints requiring different handling
SCROLL_ENDPOINTS: List[str] = [
    "companies",  # Can use scroll for large exports
]

# Event tracking endpoint
EVENTS_ENDPOINT = "/events"

# Ticket fields endpoint for custom field mapping
TICKET_FIELDS_ENDPOINT = "/ticket_types/{ticket_type_id}/attributes"

# Default fields to retrieve for each resource type
DEFAULT_CONTACT_FIELDS = [
    "id",
    "type",
    "external_id",
    "email",
    "phone",
    "name",
    "created_at",
    "updated_at",
    "signed_up_at",
    "last_seen_at",
    "last_contacted_at",
    "last_email_opened_at",
    "last_email_clicked_at",
    "browser",
    "browser_language",
    "browser_version",
    "location",
    "os",
    "role",
    "custom_attributes",
    "tags",
    "companies",
]

DEFAULT_COMPANY_FIELDS = [
    "id",
    "type",
    "company_id",
    "name",
    "plan",
    "size",
    "website",
    "industry",
    "created_at",
    "updated_at",
    "monthly_spend",
    "session_count",
    "user_count",
    "custom_attributes",
    "tags",
]

DEFAULT_CONVERSATION_FIELDS = [
    "id",
    "type",
    "created_at",
    "updated_at",
    "waiting_since",
    "snoozed_until",
    "state",
    "open",
    "read",
    "priority",
    "admin_assignee_id",
    "team_assignee_id",
    "tags",
    "conversation_rating",
    "source",
    "contacts",
    "teammates",
    "custom_attributes",
    "first_contact_reply",
    "sla_applied",
    "statistics",
    "conversation_parts",
]

DEFAULT_TICKET_FIELDS = [
    "id",
    "type",
    "ticket_id",
    "category",
    "ticket_attributes",
    "ticket_state",
    "ticket_type",
    "created_at",
    "updated_at",
    "ticket_parts",
    "contacts",
    "admin_assignee_id",
    "team_assignee_id",
    "open",
    "snoozed_until",
]

# Resources that support custom attributes
SUPPORTS_CUSTOM_ATTRIBUTES = [
    "contacts",
    "companies",
    "conversations",
]

# Maximum limits
MAX_CUSTOM_ATTRIBUTES_PER_RESOURCE = 100
MAX_EVENT_TYPES_PER_WORKSPACE = 120
MAX_CONVERSATION_PARTS = 500
MAX_SEARCH_RESULTS = 10000

# Field type mapping for custom attributes
INTERCOM_TO_DLT_TYPE_MAPPING = {
    "string": "text",
    "integer": "bigint",
    "float": "double",
    "boolean": "bool",
    "date": "timestamp",
    "datetime": "timestamp",
    "object": "json",
    "list": "json",
}
