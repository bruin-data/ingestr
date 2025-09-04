"""Docebo source for ingestr."""

from typing import Any, Dict, Iterator, Optional

import dlt
from dlt.sources import DltResource

from .client import DoceboClient, normalize_docebo_dates


@dlt.source(name="docebo", max_table_nesting=0)
def docebo_source(
    base_url: str,
    client_id: str,
    client_secret: str,
    username: Optional[str] = None,
    password: Optional[str] = None,
) -> Iterator[DltResource]:
    """
    Docebo source for fetching data from Docebo LMS API.
    
    Args:
        base_url: The base URL of your Docebo instance (e.g., https://yourcompany.docebosaas.com)
        client_id: OAuth2 client ID
        client_secret: OAuth2 client secret
        username: Username for authentication
        password: Password for authentication
    
    Yields:
        DltResource: Resources available from Docebo API
    """
    
    # Initialize client once for all resources
    client = DoceboClient(
        base_url=base_url,
        client_id=client_id,
        client_secret=client_secret,
        username=username,
        password=password,
    )
    
    @dlt.resource(
        name="users",
        write_disposition="replace",
        primary_key="user_id",
        columns={
            "user_id": {"data_type": "text", "nullable": False},
            "username": {"data_type": "text", "nullable": True},
            "first_name": {"data_type": "text", "nullable": True},
            "last_name": {"data_type": "text", "nullable": True},
            "email": {"data_type": "text", "nullable": True},
            "uuid": {"data_type": "text", "nullable": True},
            "is_manager": {"data_type": "bool", "nullable": True},
            "fullname": {"data_type": "text", "nullable": True},
            "last_access_date": {"data_type": "timestamp", "nullable": True},
            "last_update": {"data_type": "timestamp", "nullable": True},
            "creation_date": {"data_type": "timestamp", "nullable": True},
            "status": {"data_type": "text", "nullable": True},
            "avatar": {"data_type": "text", "nullable": True},
            "language": {"data_type": "text", "nullable": True},
            "lang_code": {"data_type": "text", "nullable": True},
            "level": {"data_type": "text", "nullable": True},
            "email_validation_status": {"data_type": "text", "nullable": True},
            "send_notification": {"data_type": "text", "nullable": True},
            "newsletter_optout": {"data_type": "text", "nullable": True},
            "encoded_username": {"data_type": "text", "nullable": True},
            "timezone": {"data_type": "text", "nullable": True},
            "active_subordinates_count": {"data_type": "bigint", "nullable": True},
            "expired": {"data_type": "bool", "nullable": True},
            "multidomains": {"data_type": "json", "nullable": True},
            "manager_names": {"data_type": "json", "nullable": True},
            "managers": {"data_type": "json", "nullable": True},
            "actions": {"data_type": "json", "nullable": True},
        }
    )
    def users() -> Iterator[Dict[str, Any]]:
        """Fetch all users from Docebo."""
        for users_batch in client.fetch_users():
            # Apply normalizer to each user and yield in batches
            normalized_users = [normalize_docebo_dates(user) for user in users_batch]
            yield normalized_users
    
    @dlt.resource(
        name="courses",
        write_disposition="replace",
        primary_key="id_course",
        columns={
            "id_course": {"data_type": "bigint", "nullable": False},
            "name": {"data_type": "text", "nullable": True},
            "uidCourse": {"data_type": "text", "nullable": True},
            "description": {"data_type": "text", "nullable": True},
            "date_last_updated": {"data_type": "date", "nullable": True},
            "course_type": {"data_type": "text", "nullable": True},
            "selling": {"data_type": "bool", "nullable": True},
            "code": {"data_type": "text", "nullable": True},
            "slug_name": {"data_type": "text", "nullable": True},
            "image": {"data_type": "text", "nullable": True},
            "duration": {"data_type": "bigint", "nullable": True},
            "language": {"data_type": "text", "nullable": True},
            "language_label": {"data_type": "text", "nullable": True},
            "multi_languages": {"data_type": "json", "nullable": True},
            "price": {"data_type": "text", "nullable": True},
            "is_new": {"data_type": "text", "nullable": True},
            "is_opened": {"data_type": "text", "nullable": True},
            "rating_option": {"data_type": "text", "nullable": True},
            "current_rating": {"data_type": "bigint", "nullable": True},
            "credits": {"data_type": "bigint", "nullable": True},
            "img_url": {"data_type": "text", "nullable": True},
            "can_rate": {"data_type": "bool", "nullable": True},
            "can_self_unenroll": {"data_type": "bool", "nullable": True},
            "start_date": {"data_type": "date", "nullable": True},
            "end_date": {"data_type": "date", "nullable": True},
            "category": {"data_type": "json", "nullable": True},
            "enrollment_policy": {"data_type": "bigint", "nullable": True},
            "max_attempts": {"data_type": "bigint", "nullable": True},
            "available_seats": {"data_type": "json", "nullable": True},
            "is_affiliate": {"data_type": "bool", "nullable": True},
            "partner_fields": {"data_type": "text", "nullable": True},
            "partner_data": {"data_type": "json", "nullable": True},
            "affiliate_price": {"data_type": "text", "nullable": True},
        }
    )
    def courses() -> Iterator[Dict[str, Any]]:
        """Fetch all courses from Docebo."""
        for courses_batch in client.fetch_courses():
            # Apply normalizer to each course and yield in batches
            normalized_courses = [normalize_docebo_dates(course) for course in courses_batch]
            # print(normalized_courses)
            yield normalized_courses
    
    return [users, courses]