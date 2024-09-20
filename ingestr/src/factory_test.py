from ingestr.src.factory import parse_scheme_from_uri, parse_columns
import pytest

def test_scheme_is_parsed_from_uri_correctly():
    assert parse_scheme_from_uri("bigquery://my-project") == "bigquery"
    assert parse_scheme_from_uri("http://localhost:8080") == "http"
    assert parse_scheme_from_uri("file:///tmp/myfile") == "file"
    assert parse_scheme_from_uri("https://example.com?query=123") == "https"
    assert parse_scheme_from_uri("ftp://ftp.example.com/downloads/file.zip") == "ftp"
    assert (
        parse_scheme_from_uri("redshift+psycopg2://user:pw@host") == "redshift+psycopg2"
    )
    assert parse_scheme_from_uri("mysql+pymysql://user:pw@host") == "mysql+pymysql"


def test_parse_columns_valid_input():
    input_columns = [
        "col1:text",
        "col2:double:nullable",
        "col3:bool"
    ]
    expected_output = {
        "col1": {"data_type": "text", "nullable": False},
        "col2": {"data_type": "double", "nullable": True},
        "col3": {"data_type": "bool", "nullable": False}
    }
    assert parse_columns(input_columns) == expected_output

def test_parse_columns_empty_input():
    assert parse_columns([]) == {}

def test_parse_columns_none_input():
    assert parse_columns(None) == {}

def test_parse_columns_all_data_types():
    input_columns = [
        "col1:text", "col2:double", "col3:bool", "col4:timestamp",
        "col5:bigint", "col6:binary", "col7:complex", "col8:decimal",
        "col9:wei", "col10:date", "col11:time"
    ]
    expected_output = {
        "col1": {"data_type": "text", "nullable": False},
        "col2": {"data_type": "double", "nullable": False},
        "col3": {"data_type": "bool", "nullable": False},
        "col4": {"data_type": "timestamp", "nullable": False},
        "col5": {"data_type": "bigint", "nullable": False},
        "col6": {"data_type": "binary", "nullable": False},
        "col7": {"data_type": "complex", "nullable": False},
        "col8": {"data_type": "decimal", "nullable": False},
        "col9": {"data_type": "wei", "nullable": False},
        "col10": {"data_type": "date", "nullable": False},
        "col11": {"data_type": "time", "nullable": False},
    }    

    result = parse_columns(input_columns)
    assert len(result) == 11
    assert result == expected_output


def test_parse_columns_invalid_format():
    with pytest.raises(ValueError, match="Argument format is incorrect"):
        parse_columns(["invalid_column"])
    with pytest.raises(ValueError, match="Invalid column name"):
        parse_columns(["1invalid:text"])
    with pytest.raises(ValueError, match="Invalid column name"):
        parse_columns(["-invalid:text"])
    with pytest.raises(ValueError, match="Invalid column name"):
        parse_columns(["invalid@column:text"])
    with pytest.raises(ValueError, match="Invalid data type"):
        parse_columns(["valid_name:invalid_type"])
    with pytest.raises(ValueError, match="Argument format is incorrect"):
        parse_columns(["valid_name:text:invalid"])

def test_parse_columns_mixed_valid_invalid():
    input_columns = [
        "valid1:text",
        "valid2:double:nullable",
        "invalid:wrong",
        "valid3:bool"
    ]
    with pytest.raises(ValueError):
        parse_columns(input_columns)

def test_parse_columns_case_sensitivity():
    input_columns = ["COL1:TEXT", "Col2:Double:NULLABLE"]
    expected_output = {
        "COL1": {"data_type": "text", "nullable": False},
        "Col2": {"data_type": "double", "nullable": True}
    }    
    assert parse_columns(input_columns) == expected_output

def test_parse_columns_underscore_in_name():
    input_columns = ["valid_column_name:text"]
    result = parse_columns(input_columns)
    assert "valid_column_name" in result

def test_parse_columns_duplicate_names():
    input_columns = ["col1:text", "col1:double"]
    result = parse_columns(input_columns)
    assert len(result) == 1
    assert result["col1"]["data_type"] == "double"

def test_parse_columns_empty_column_definition():
    with pytest.raises(ValueError):
        parse_columns([""])
