"""Monday.com source settings and constants"""

# GraphQL query for fetching app installs
APP_INSTALLS_QUERY = """
query ($app_id: ID!, $account_id: ID, $limit: Int!, $page: Int!) {
    app_installs(
        app_id: $app_id
        account_id: $account_id
        limit: $limit
        page: $page
    ) {
        app_id
        timestamp
        app_install_account {
            id
        }
        app_install_user {
            id
        }
        app_version {
            major
            minor
            patch
            type
            text
        }
        permissions {
            approved_scopes
            required_scopes
        }
    }
}
"""

# GraphQL query for fetching account information
ACCOUNT_QUERY = """
query {
    account {
        id
        name
        slug
        tier
        country_code
        first_day_of_the_week
        show_timeline_weekends
        sign_up_product_kind
        active_members_count
        logo
        plan {
            max_users
            period
            tier
            version
        }
    }
}
"""

# GraphQL query for fetching account roles
ACCOUNT_ROLES_QUERY = """
query {
    account_roles {
        id
        name
        roleType
    }
}
"""

# Maximum number of results per page
MAX_PAGE_SIZE = 100
