"""Helper functions for Docebo API data processing."""

from datetime import datetime
from typing import Any, Dict, Union


def normalize_date_field(date_value: Any) -> Union[datetime, str, None]:
    """
    Normalize a single date field that may contain invalid dates.

    Args:
        date_value: The date value to normalize (string, datetime, or None)

    Returns:
        Normalized datetime object or None for invalid/empty dates
    """
    # Unix epoch datetime (1970-01-01 00:00:00 UTC)
    epoch_datetime = datetime(1970, 1, 1)

    # Handle string dates
    if isinstance(date_value, str):
        # Handle '0000-00-00' or '0000-00-00 00:00:00'
        if date_value.startswith("0000-00-00"):
            return epoch_datetime
        # Handle other invalid date formats
        elif date_value in ["", "0", "null", "NULL"]:
            return None
        # Try to parse valid date strings
        else:
            try:
                # Try common date formats
                for fmt in [
                    "%Y-%m-%d %H:%M:%S",
                    "%Y-%m-%d",
                    "%Y/%m/%d %H:%M:%S",
                    "%Y/%m/%d",
                ]:
                    try:
                        return datetime.strptime(date_value, fmt)
                    except ValueError:
                        continue
                # If no format matches, return the original string
                return date_value
            except Exception:
                return date_value
    # Handle datetime objects - pass through
    elif isinstance(date_value, datetime):
        return date_value
    # Handle cases where the field might be None or empty
    elif not date_value:
        return None

    # Return the original value for other types
    return date_value


def normalize_docebo_dates(item: Dict[str, Any]) -> Dict[str, Any]:
    """
    Normalize Docebo date fields that contain '0000-00-00' to Unix epoch (1970-01-01).

    Args:
        item: Dictionary containing data from Docebo API

    Returns:
        Dictionary with normalized date fields
    """
    # Date fields that might contain '0000-00-00'
    # Add more fields as needed for different resources
    date_fields = [
        "last_access_date",
        "last_update",
        "creation_date",
        "date_begin",  # Course field
        "date_end",  # Course field
        "date_publish",  # Course field
        "date_unpublish",  # Course field
        "enrollment_date",  # Enrollment field
        "completion_date",  # Enrollment field
        "date_assigned",  # Assignment field
        "date_completed",  # Completion field
        "survey_date",  # Survey field
        "start_date",  # Course/Plan field
        "end_date",  # Course/Plan field
        "date_created",  # Generic creation date
        "created_on",  # Learning plan field
        "updated_on",  # Learning plan field
        "date_modified",  # Generic modification date
        "expire_date",  # Expiration date
        "date_last_updated",  # Update date
        "date",  # Generic date field (used in survey answers)
    ]

    for field in date_fields:
        if field in item:
            item[field] = normalize_date_field(item[field])

    return item
