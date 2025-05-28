import sys
import unittest
from unittest.mock import MagicMock, patch

import smartsheet  # type: ignore
from smartsheet.models import Cell, Column, Row, Sheet  # type: ignore

from ingestr.src.smartsheets import _get_sheet_data, smartsheet_source


def pp(x):
    print(x, file=sys.stderr)


class TestSmartsheetSource(unittest.TestCase):
    @patch("ingestr.src.smartsheets.smartsheet.Smartsheet")
    def test_smartsheet_source_success(self, mock_smartsheet_client):
        # Mock Smartsheet client and its methods
        mock_client_instance = mock_smartsheet_client.return_value

        # Mock sheet details response
        mock_sheet_details = Sheet(
            {
                "id": 123,
                "name": "Test Sheet 1",
                "columns": [
                    Column(
                        {"id": 1, "title": "Col A", "type": "TEXT_NUMBER", "index": 0}
                    ),
                    Column(
                        {"id": 2, "title": "Col B", "type": "TEXT_NUMBER", "index": 1}
                    ),
                ],
                "rows": [
                    Row(
                        {
                            "id": 101,
                            "sheetId": 123,
                            "cells": [
                                Cell({"columnId": 1, "value": "r1c1"}),
                                Cell({"columnId": 2, "value": "r1c2"}),
                            ],
                        }
                    ),
                    Row(
                        {
                            "id": 102,
                            "sheetId": 123,
                            "cells": [
                                Cell({"columnId": 1, "value": "r2c1"}),
                                Cell({"columnId": 2, "value": "r2c2"}),
                            ],
                        }
                    ),
                ],
            }
        )
        mock_client_instance.Sheets.get_sheet.return_value = mock_sheet_details

        resource = smartsheet_source(access_token="test_token", sheet_id="123")
        data = list(resource)
        self.assertEqual(len(data), 2)
        self.assertEqual(data[0], {"Col A": "r1c1", "Col B": "r1c2"})
        self.assertEqual(data[1], {"Col A": "r2c1", "Col B": "r2c2"})

        mock_smartsheet_client.assert_called_once_with("test_token")
        mock_client_instance.Sheets.get_sheet.assert_any_call(
            123, include=["objectValue"]
        )  # for resource name
        mock_client_instance.Sheets.get_sheet.assert_any_call(
            123
        )  # for _get_sheet_data

    @patch("ingestr.src.smartsheets.smartsheet.Smartsheet")
    def test_smartsheet_source_api_error(self, mock_smartsheet_client):
        mock_client_instance = mock_smartsheet_client.return_value
        mock_client_instance.Sheets.get_sheet.side_effect = (
            smartsheet.exceptions.ApiError("API Error", 500)
        )

        with self.assertRaises(smartsheet.exceptions.ApiError):
            source = smartsheet_source(access_token="test_token", sheet_id="123")
            # Consume the generator to trigger the API call
            list(source)

    def test_get_sheet_data(self):
        mock_smartsheet_client_instance = MagicMock()
        mock_sheet = Sheet(
            {
                "id": 456,
                "name": "Data Sheet",
                "columns": [
                    Column(
                        {"id": 10, "title": "ID", "type": "TEXT_NUMBER", "index": 0}
                    ),
                    Column(
                        {"id": 20, "title": "Value", "type": "TEXT_NUMBER", "index": 1}
                    ),
                ],
                "rows": [
                    Row(
                        {
                            "id": 201,
                            "sheetId": 456,
                            "cells": [
                                Cell({"columnId": 10, "value": 1}),
                                Cell({"columnId": 20, "value": "Alpha"}),
                            ],
                        }
                    ),
                    Row(
                        {
                            "id": 202,
                            "sheetId": 456,
                            "cells": [
                                Cell({"columnId": 10, "value": 2}),
                                Cell({"columnId": 20, "value": "Beta"}),
                            ],
                        }
                    ),
                ],
            }
        )
        mock_smartsheet_client_instance.Sheets.get_sheet.return_value = mock_sheet

        data_generator = _get_sheet_data(mock_smartsheet_client_instance, 456)
        data = list(data_generator)

        self.assertEqual(len(data), 2)
        self.assertEqual(data[0], {"ID": 1, "Value": "Alpha"})
        self.assertEqual(data[1], {"ID": 2, "Value": "Beta"})
        mock_smartsheet_client_instance.Sheets.get_sheet.assert_called_once_with(456)


if __name__ == "__main__":
    unittest.main()
