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
        name
        description
        state
        board_kind
        board_folder_id
        workspace_id
        permissions
        item_terminology
        items_count
        updated_at
        url
        communication
        object_type_unique_key
        type
        creator {
            id
        }
        owners {
            id
        }
        subscribers {
            id
        }
        team_owners {
            id
        }
        team_subscribers {
            id
        }
        tags {
            id
            
        }
    }
}
"""

# GraphQL query for fetching custom activities
CUSTOM_ACTIVITIES_QUERY = """
query {
    custom_activity {
        id
        name
        type
        color
        icon_id
    }
}
"""

# GraphQL query for fetching board columns
BOARD_COLUMNS_QUERY = """
query ($board_ids: [ID!]) {
    boards(ids: $board_ids) {
        id
        columns {
            id
            title
            type
            archived
            description
            settings_str
            width
        }
    }
}
"""

# GraphQL query for fetching board views
BOARD_VIEWS_QUERY = """
query ($board_ids: [ID!]) {
    boards(ids: $board_ids) {
        id
        views {
            id
            name
            type
            settings_str
            view_specific_data_str
            source_view_id
            access_level
        }
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
        owners_subscribers {
            id
        }
        team_owners_subscribers {
            id
        }
        teams_subscribers {
            id
        }
        users_subscribers {
            id
        }
        settings {
            icon
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

# GraphQL query for fetching updates
UPDATES_QUERY = """
query ($limit: Int!, $from_date: String, $to_date: String) {
    updates(limit: $limit, from_date: $from_date, to_date: $to_date) {
        id
        body
        text_body
        created_at
        updated_at
        edited_at
        creator_id
        item_id
        creator {
            id
        }
        item {
            id
        }
        assets {
            id
            name
            file_extension
            file_size
            public_url
            url
            url_thumbnail
            created_at
            original_geometry
            uploaded_by {
                id
            }
        }
        replies {
            id
            body
            text_body
            created_at
            updated_at
            creator_id
            creator {
                id
            }
        }
        likes {
            id
        }
        pinned_to_top {
            item_id
        }
        viewers {
            medium
            user_id
            user {
                id
            }
        }
    }
}
"""

# GraphQL query for fetching teams
TEAMS_QUERY = """
query {
    teams {
        id
        name
        picture_url
        users {
            id
            created_at
            phone
        }
    }
}
"""

# GraphQL query for fetching tags
TAGS_QUERY = """
query {
    tags {
        id
        name
        color
    }
}
"""

# Maximum number of results per page
MAX_PAGE_SIZE = 100
