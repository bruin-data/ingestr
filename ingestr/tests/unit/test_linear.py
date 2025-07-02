import unittest
from unittest.mock import patch
import pendulum

from ingestr.src.linear import linear_source, ISSUES_QUERY


class TestLinearSource(unittest.TestCase):
    @patch("ingestr.src.linear._paginate")
    def test_issues_resource(self, mock_paginate):
        mock_paginate.return_value = iter([
            {
                "id": "1",
                "title": "Issue 1",
                "description": "d1",
                "createdAt": "2023-01-01T00:00:00Z",
                "updatedAt": "2023-01-01T00:00:00Z",
            },
            {
                "id": "2",
                "title": "Issue 2",
                "description": "d2",
                "createdAt": "2023-01-02T00:00:00Z",
                "updatedAt": "2023-01-02T00:00:00Z",
            },
        ])
        start = pendulum.datetime(2023, 1, 1, tz="UTC")
        source = linear_source(api_key="key", start_date=start)
        data = list(source.resources["issues"])

        self.assertEqual(len(data), 2)
        self.assertEqual(data[0]["id"], "1")
        self.assertEqual(data[1]["id"], "2")
        mock_paginate.assert_called_with("key", ISSUES_QUERY, "issues")

    @patch("ingestr.src.linear._paginate")
    def test_issues_date_filter(self, mock_paginate):
        mock_paginate.return_value = iter(
            [
                {
                    "id": "1",
                    "updatedAt": "2023-01-01T00:00:00Z",
                },
                {
                    "id": "2",
                    "updatedAt": "2023-01-05T00:00:00Z",
                },
            ]
        )
        start = pendulum.datetime(2023, 1, 2, tz="UTC")
        end = pendulum.datetime(2023, 1, 3, tz="UTC")
        source = linear_source(api_key="key", start_date=start, end_date=end)
        data = list(source.resources["issues"])

        self.assertEqual(len(data), 0)
        mock_paginate.assert_called_with("key", ISSUES_QUERY, "issues")


if __name__ == "__main__":
    unittest.main()

