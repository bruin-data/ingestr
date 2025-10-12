"""
Mailchimp API endpoint configurations.
"""

# Endpoints with merge disposition (have both primary_key and incremental_key)
# Format: (resource_name, endpoint_path, data_key, primary_key, incremental_key)
MERGE_ENDPOINTS = [
    ("audiences", "lists", "lists", "id", "date_created"),
    ("automations", "automations", "automations", "id", "create_time"),
    ("campaigns", "campaigns", "campaigns", "id", "create_time"),
    ("connected_sites", "connected-sites", "sites", "id", "updated_at"),
    ("conversations", "conversations", "conversations", "id", "last_message.timestamp"),
    ("ecommerce_stores", "ecommerce/stores", "stores", "id", "updated_at"),
    ("facebook_ads", "facebook-ads", "facebook_ads", "id", "updated_at"),
    ("landing_pages", "landing-pages", "landing_pages", "id", "updated_at"),
    ("reports", "reports", "reports", "id", "send_time"),
]

# Endpoints with replace disposition
# Format: (resource_name, endpoint_path, data_key, primary_key)
REPLACE_ENDPOINTS: list[tuple[str, str, str, str | None]] = [
    ("account_exports", "account-exports", "exports", None),
    ("authorized_apps", "authorized-apps", "apps", "id"),
    ("batches", "batches", "batches", None),
    ("campaign_folders", "campaign-folders", "folders", "id"),
    ("chimp_chatter", "activity-feed/chimp-chatter", "chimp_chatter", None),
]

# Nested endpoints (depend on parent resources)
# Format: (parent_name, parent_path, parent_key, parent_id_field, nested_name, nested_path, nested_key, pk)
NESTED_ENDPOINTS: list[tuple[str, str, str, str, str, str, str | None, str | None]] = [
    # Reports nested endpoints
    (
        "reports",
        "reports",
        "reports",
        "id",
        "reports_advice",
        "reports/{id}/advice",
        None,
        None,
    ),
    (
        "reports",
        "reports",
        "reports",
        "id",
        "reports_domain_performance",
        "reports/{id}/domain-performance",
        "domains",
        None,
    ),
    (
        "reports",
        "reports",
        "reports",
        "id",
        "reports_locations",
        "reports/{id}/locations",
        "locations",
        None,
    ),
    (
        "reports",
        "reports",
        "reports",
        "id",
        "reports_sent_to",
        "reports/{id}/sent-to",
        "sent_to",
        None,
    ),
    (
        "reports",
        "reports",
        "reports",
        "id",
        "reports_sub_reports",
        "reports/{id}/sub-reports",
        None,
        None,
    ),
    (
        "reports",
        "reports",
        "reports",
        "id",
        "reports_unsubscribed",
        "reports/{id}/unsubscribed",
        "unsubscribes",
        None,
    ),
    # Lists/Audiences nested endpoints
    (
        "audiences",
        "lists",
        "lists",
        "id",
        "lists_activity",
        "lists/{id}/activity",
        "activity",
        None,
    ),
    (
        "audiences",
        "lists",
        "lists",
        "id",
        "lists_clients",
        "lists/{id}/clients",
        "clients",
        None,
    ),
    (
        "audiences",
        "lists",
        "lists",
        "id",
        "lists_growth_history",
        "lists/{id}/growth-history",
        "history",
        None,
    ),
    (
        "audiences",
        "lists",
        "lists",
        "id",
        "lists_interest_categories",
        "lists/{id}/interest-categories",
        "categories",
        None,
    ),
    (
        "audiences",
        "lists",
        "lists",
        "id",
        "lists_locations",
        "lists/{id}/locations",
        "locations",
        None,
    ),
    (
        "audiences",
        "lists",
        "lists",
        "id",
        "lists_merge_fields",
        "lists/{id}/merge-fields",
        "merge_fields",
        None,
    ),
    (
        "audiences",
        "lists",
        "lists",
        "id",
        "lists_segments",
        "lists/{id}/segments",
        "segments",
        None,
    ),
]
