"""Test cases for Docebo helper functions."""

from datetime import datetime

from ingestr.src.docebo.helpers import normalize_date_field, normalize_docebo_dates


class TestNormalizeDateField:
    """Test cases for normalize_date_field function."""

    def test_zero_date_string(self):
        """Test that '0000-00-00' dates are converted to Unix epoch."""
        epoch = datetime(1970, 1, 1)
        assert normalize_date_field("0000-00-00") == epoch
        assert normalize_date_field("0000-00-00 00:00:00") == epoch
        assert normalize_date_field("0000-00-00 12:34:56") == epoch

    def test_invalid_string_formats(self):
        """Test that invalid string formats return None."""
        assert normalize_date_field("") is None
        assert normalize_date_field("0") is None
        assert normalize_date_field("null") is None
        assert normalize_date_field("NULL") is None

    def test_none_and_empty_values(self):
        """Test that None and falsy values return None."""
        assert normalize_date_field(None) is None
        assert normalize_date_field(0) is None
        assert normalize_date_field(False) is None
        assert normalize_date_field([]) is None
        assert normalize_date_field({}) is None

    def test_valid_datetime_passthrough(self):
        """Test that valid datetime objects pass through unchanged."""
        valid_date = datetime(2024, 1, 15, 10, 30, 0)
        assert normalize_date_field(valid_date) == valid_date

    def test_valid_string_passthrough(self):
        """Test that valid date strings are parsed to datetime."""
        valid_date_str = "2024-01-15 10:30:00"
        expected = datetime(2024, 1, 15, 10, 30, 0)
        assert normalize_date_field(valid_date_str) == expected

        valid_date_str2 = "2024-01-15"
        expected2 = datetime(2024, 1, 15)
        assert normalize_date_field(valid_date_str2) == expected2


class TestNormalizeDoceboDates:
    """Test cases for normalize_docebo_dates function."""

    def test_single_date_field_normalization(self):
        """Test normalization of a single date field."""
        epoch = datetime(1970, 1, 1)
        item = {"id": 123, "name": "Test User", "last_access_date": "0000-00-00"}
        result = normalize_docebo_dates(item)
        assert result["last_access_date"] == epoch
        assert result["id"] == 123
        assert result["name"] == "Test User"

    def test_multiple_date_fields_normalization(self):
        """Test normalization of multiple date fields."""
        epoch = datetime(1970, 1, 1)
        item = {
            "course_id": 456,
            "date_begin": "0000-00-00",
            "date_end": "0000-00-00 00:00:00",
            "enrollment_date": "null",
            "completion_date": "",
            "last_update": "2024-01-15",
        }
        result = normalize_docebo_dates(item)
        assert result["date_begin"] == epoch
        assert result["date_end"] == epoch
        assert result["enrollment_date"] is None
        assert result["completion_date"] is None
        assert result["last_update"] == datetime(2024, 1, 15)
        assert result["course_id"] == 456

    def test_survey_date_field(self):
        """Test normalization of survey-specific date field."""
        epoch = datetime(1970, 1, 1)
        survey_data = {
            "survey_id": 789,
            "date": "0000-00-00",
            "survey_date": "0000-00-00 00:00:00",
            "response": "Sample response",
        }
        result = normalize_docebo_dates(survey_data)
        assert result["date"] == epoch
        assert result["survey_date"] == epoch
        assert result["survey_id"] == 789
        assert result["response"] == "Sample response"

    def test_learning_plan_date_fields(self):
        """Test normalization of learning plan date fields."""
        item = {
            "learning_plan_id": 101,
            "created_on": "NULL",
            "updated_on": "",
            "start_date": "2024-02-01",
            "end_date": None,
        }
        result = normalize_docebo_dates(item)
        assert result["created_on"] is None
        assert result["updated_on"] is None
        assert result["start_date"] == datetime(2024, 2, 1)
        assert result["end_date"] is None
        assert result["learning_plan_id"] == 101

    def test_all_known_date_fields(self):
        """Test that all known date fields are handled."""
        epoch = datetime(1970, 1, 1)
        all_date_fields = {
            "last_access_date": "0000-00-00",
            "last_update": "0000-00-00",
            "creation_date": "0000-00-00",
            "date_begin": "0000-00-00",
            "date_end": "0000-00-00",
            "date_publish": "0000-00-00",
            "date_unpublish": "0000-00-00",
            "enrollment_date": "0000-00-00",
            "completion_date": "0000-00-00",
            "date_assigned": "0000-00-00",
            "date_completed": "0000-00-00",
            "survey_date": "0000-00-00",
            "start_date": "0000-00-00",
            "end_date": "0000-00-00",
            "date_created": "0000-00-00",
            "created_on": "0000-00-00",
            "updated_on": "0000-00-00",
            "date_modified": "0000-00-00",
            "expire_date": "0000-00-00",
            "date_last_updated": "0000-00-00",
            "date": "0000-00-00",
        }
        result = normalize_docebo_dates(all_date_fields)
        for field in all_date_fields:
            assert result[field] == epoch, f"Field {field} was not normalized to epoch"

    def test_non_date_fields_unchanged(self):
        """Test that non-date fields are not modified."""
        item = {
            "user_id": 999,
            "username": "testuser",
            "email": "test@example.com",
            "status": "active",
            "score": 95.5,
            "tags": ["tag1", "tag2"],
            "metadata": {"key": "value"},
            "last_update": "0000-00-00",  # Only this should change
        }
        result = normalize_docebo_dates(item)
        assert result["user_id"] == 999
        assert result["username"] == "testuser"
        assert result["email"] == "test@example.com"
        assert result["status"] == "active"
        assert result["score"] == 95.5
        assert result["tags"] == ["tag1", "tag2"]
        assert result["metadata"] == {"key": "value"}
        assert result["last_update"] == datetime(1970, 1, 1)

    def test_empty_dict(self):
        """Test that empty dictionary is handled correctly."""
        result = normalize_docebo_dates({})
        assert result == {}

    def test_dict_with_no_date_fields(self):
        """Test dictionary with no date fields."""
        item = {"id": 1, "name": "Test", "value": 100}
        result = normalize_docebo_dates(item)
        assert result == item

    def test_mixed_valid_invalid_dates(self):
        """Test mix of valid and invalid date values."""
        epoch = datetime(1970, 1, 1)
        valid_datetime = datetime(2024, 3, 15)
        item = {
            "date_begin": "0000-00-00",
            "date_end": valid_datetime,
            "enrollment_date": "2024-01-20",
            "completion_date": None,
            "last_update": "",
        }
        result = normalize_docebo_dates(item)
        assert result["date_begin"] == epoch
        assert result["date_end"] == valid_datetime  # datetime passes through
        assert result["enrollment_date"] == datetime(2024, 1, 20)
        assert result["completion_date"] is None
        assert result["last_update"] is None
