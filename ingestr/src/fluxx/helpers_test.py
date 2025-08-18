"""
Pytest tests for fluxx/helpers.py functions.

Run tests with:
    pytest ingestr/src/fluxx/helpers_test.py -v

Or run specific tests:
    pytest ingestr/src/fluxx/helpers_test.py::test_connection_ids_single_number_to_array -v
"""

import pytest

from ingestr.src.fluxx.helpers import normalize_fluxx_item


@pytest.fixture
def sample_fields_config():
    """Fixture providing standard field configurations for testing."""
    return {
        "id": {"data_type": "bigint", "field_type": "column"},
        "name": {"data_type": "text", "field_type": "column"},
        "description": {"data_type": "text", "field_type": "string"},
        "amount_requested": {"data_type": "decimal", "field_type": "column"},
        "granted": {"data_type": "bool", "field_type": "column"},
        "created_at": {"data_type": "timestamp", "field_type": "column"},
        "updated_at": {"data_type": "date", "field_type": "column"},
        "connection_ids": {"data_type": "json", "field_type": "relation"},
        "organization_id": {"data_type": "bigint", "field_type": "relation"},
        "alert_emails": {"data_type": "json", "field_type": "relation"},
        "metadata": {"data_type": "json", "field_type": "relation"},
    }


def test_normalize_with_all_field_types(sample_fields_config):
    """Test normalization with all supported field types."""
    input_item = {
        "id": 123,
        "name": "Test Grant",
        "description": "A test grant description",
        "amount_requested": 50000.00,
        "granted": True,
        "created_at": "2023-01-15T10:30:00Z",
        "updated_at": "2023-12-01",
        "connection_ids": [1, 2, 3],
        "organization_id": 456,
        "alert_emails": ["test@example.com", "admin@example.com"],
        "metadata": {"key": "value"},
        "extra_field": "should be ignored",
    }

    expected = {
        "id": 123,
        "name": "Test Grant",
        "description": "A test grant description",
        "amount_requested": 50000.00,
        "granted": True,
        "created_at": "2023-01-15T10:30:00Z",
        "updated_at": "2023-12-01",
        "connection_ids": [1, 2, 3],
        "organization_id": 456,
        "alert_emails": ["test@example.com", "admin@example.com"],
        "metadata": {"key": "value"},
    }

    result = normalize_fluxx_item(input_item, sample_fields_config)
    assert result == expected


def test_normalize_single_values_for_json_fields(sample_fields_config):
    """Test that single values are wrapped in arrays for json fields."""
    input_item = {
        "id": 124,
        "connection_ids": 789,  # Single number should become [789]
        "alert_emails": "single@example.com",  # Single string should become ["single@example.com"]
        "organization_id": 456,
    }

    expected = {
        "id": 124,
        "connection_ids": [789],
        "alert_emails": ["single@example.com"],
        "organization_id": 456,
    }

    result = normalize_fluxx_item(input_item, sample_fields_config)
    assert result == expected


def test_normalize_empty_strings_and_null_values(sample_fields_config):
    """Test handling of empty strings and null values."""
    input_item = {
        "id": 125,
        "name": "",  # Empty string for text field
        "description": "",  # Empty string for string field
        "created_at": "",  # Empty string for timestamp
        "updated_at": "",  # Empty string for date
        "connection_ids": None,  # Null for json field
        "alert_emails": "",  # Empty string for json field
        "organization_id": None,  # Null for relation
    }

    expected = {
        "id": 125,
        "name": None,
        "description": None,
        "created_at": None,
        "updated_at": None,
        "connection_ids": None,
        "alert_emails": None,
        "organization_id": None,
    }

    result = normalize_fluxx_item(input_item, sample_fields_config)
    assert result == expected


def test_normalize_edge_cases_for_json_fields(sample_fields_config):
    """Test edge cases for json fields (zero, boolean, string values)."""
    input_item = {
        "id": 126,
        "connection_ids": 0,  # Zero value
        "alert_emails": False,  # Boolean value
        "metadata": "string_value",  # String for json field
    }

    expected = {
        "id": 126,
        "connection_ids": [0],
        "alert_emails": [False],
        "metadata": ["string_value"],
    }

    result = normalize_fluxx_item(input_item, sample_fields_config)
    assert result == expected


def test_normalize_missing_fields_in_item(sample_fields_config):
    """Test normalization when input item has missing fields."""
    input_item = {
        "id": 127,
        "name": "Partial Grant",
        # Many fields missing
    }

    expected = {
        "id": 127,
        "name": "Partial Grant",
    }

    result = normalize_fluxx_item(input_item, sample_fields_config)
    assert result == expected


def test_normalize_mixed_data_types(sample_fields_config):
    """Test normalization with mixed/unexpected data types."""
    input_item = {
        "id": "128",  # String ID
        "name": 12345,  # Number as name
        "granted": "true",  # String boolean
        "amount_requested": "50000",  # String number
        "connection_ids": [1, "2", 3.0],  # Mixed array
    }

    expected = {
        "id": "128",
        "name": 12345,
        "granted": "true",
        "amount_requested": "50000",
        "connection_ids": [1, "2", 3.0],
    }

    result = normalize_fluxx_item(input_item, sample_fields_config)
    assert result == expected


def test_normalize_no_field_configuration():
    """Test that function returns input as-is when no field configuration provided."""
    input_item = {"id": 999, "name": "Test", "connection_ids": 123}

    result = normalize_fluxx_item(input_item, None)
    assert result == input_item


def test_normalize_connection_ids_single_number_to_array():
    """Test the specific issue: connection_ids single number should become array."""
    fields_config = {"connection_ids": {"data_type": "json", "field_type": "relation"}}

    input_item = {"connection_ids": 123}
    result = normalize_fluxx_item(input_item, fields_config)

    assert result["connection_ids"] == [123]
    assert isinstance(result["connection_ids"], list)


def test_normalize_alert_emails_single_string_to_array():
    """Test that single string alert_emails becomes an array."""
    fields_config = {"alert_emails": {"data_type": "json", "field_type": "relation"}}

    input_item = {"alert_emails": "test@example.com"}
    result = normalize_fluxx_item(input_item, fields_config)

    assert result["alert_emails"] == ["test@example.com"]
    assert isinstance(result["alert_emails"], list)


def test_normalize_preserve_existing_arrays_and_dicts():
    """Test that existing arrays and dictionaries are preserved."""
    fields_config = {
        "connection_ids": {"data_type": "json", "field_type": "relation"},
        "metadata": {"data_type": "json", "field_type": "relation"},
    }

    input_item = {
        "connection_ids": [1, 2, 3],
        "metadata": {"key": "value", "count": 42},
    }

    result = normalize_fluxx_item(input_item, fields_config)

    assert result["connection_ids"] == [1, 2, 3]
    assert result["metadata"] == {"key": "value", "count": 42}


def test_normalize_text_field_empty_string_to_none():
    """Test that empty strings in text fields become None."""
    fields_config = {
        "name": {"data_type": "text", "field_type": "column"},
        "description": {"data_type": "text", "field_type": "string"},
    }

    input_item = {"name": "", "description": ""}

    result = normalize_fluxx_item(input_item, fields_config)

    assert result["name"] is None
    assert result["description"] is None


def test_normalize_date_timestamp_empty_string_to_none():
    """Test that empty strings in date/timestamp fields become None."""
    fields_config = {
        "created_at": {"data_type": "timestamp", "field_type": "column"},
        "updated_at": {"data_type": "date", "field_type": "column"},
    }

    input_item = {"created_at": "", "updated_at": ""}

    result = normalize_fluxx_item(input_item, fields_config)

    assert result["created_at"] is None
    assert result["updated_at"] is None


def test_normalize_id_field_always_included():
    """Test that id field is always included when present in input."""
    fields_config = {"name": {"data_type": "text", "field_type": "column"}}

    input_item = {"id": 999, "name": "Test", "other_field": "ignored"}

    result = normalize_fluxx_item(input_item, fields_config)

    assert "id" in result
    assert result["id"] == 999
    assert "other_field" not in result
