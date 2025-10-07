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

# GraphQL query for fetching users
USERS_QUERY = """
query ($limit: Int!, $page: Int!) {
    users(limit: $limit, page: $page) {
        id
        name
        email
        enabled
        is_admin
        is_guest
        is_pending
        is_view_only
        created_at
        birthday
        country_code
        join_date
        location
        mobile_phone
        phone
        photo_original
        photo_thumb
        photo_tiny
        time_zone_identifier
        title
        url
        utc_hours_diff
        current_language
        account {
            id
        }
    }
}
"""

# GraphQL query for fetching boards
BOARDS_QUERY = """
query ($limit: Int!, $page: Int!) {
    boards(limit: $limit, page: $page) {
        id
        workspace_id
    }
}
"""

# GraphQL query for fetching workspaces by IDs
WORKSPACES_QUERY = """
query ($ids: [ID!]) {
    workspaces(ids: $ids) {
        id
        name
        kind
        description
        created_at
        is_default_workspace
        state
        account_product {
            id
        }
    }
}
"""

# GraphQL query for fetching webhooks by board ID
WEBHOOKS_QUERY = """
query ($board_id: ID!) {
    webhooks(board_id: $board_id) {
        id
        event
        board_id
        config
    }
}
"""

# Maximum number of results per page
MAX_PAGE_SIZE = 100
