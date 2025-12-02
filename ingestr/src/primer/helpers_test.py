import unittest

from dlt.common.pendulum import pendulum

from ingestr.src.primer.helpers import build_date_params


class TestBuildDateParams(unittest.TestCase):
    def test_no_dates(self):
        result = build_date_params()
        self.assertEqual(result, {})

    def test_start_date_only(self):
        start = pendulum.datetime(2025, 11, 15, 10, 30, 0)
        result = build_date_params(start_date=start)
        self.assertEqual(result, {"from_date": "2025-11-15T00:00:00Z"})

    def test_end_date_only(self):
        end = pendulum.datetime(2025, 11, 15, 10, 30, 0)
        result = build_date_params(end_date=end)
        self.assertEqual(result, {"to_date": "2025-11-16T00:00:00Z"})

    def test_both_dates(self):
        start = pendulum.datetime(2025, 11, 1, 0, 0, 0)
        end = pendulum.datetime(2025, 11, 30, 0, 0, 0)
        result = build_date_params(start_date=start, end_date=end)
        self.assertEqual(
            result,
            {
                "from_date": "2025-11-01T00:00:00Z",
                "to_date": "2025-12-01T00:00:00Z",
            },
        )

    def test_start_date_with_time_rounds_to_start_of_day(self):
        start = pendulum.datetime(2025, 11, 15, 14, 30, 45)
        result = build_date_params(start_date=start)
        self.assertEqual(result, {"from_date": "2025-11-15T00:00:00Z"})

    def test_end_date_at_start_of_day_adds_one_day(self):
        end = pendulum.datetime(2025, 11, 30, 0, 0, 0)
        result = build_date_params(end_date=end)
        self.assertEqual(result, {"to_date": "2025-12-01T00:00:00Z"})

    def test_end_date_at_end_of_day_adds_one_day(self):
        end = pendulum.datetime(2025, 11, 30, 23, 59, 59)
        result = build_date_params(end_date=end)
        self.assertEqual(result, {"to_date": "2025-12-01T00:00:00Z"})

    def test_end_date_mid_day_adds_one_day(self):
        end = pendulum.datetime(2025, 11, 30, 14, 30, 0)
        result = build_date_params(end_date=end)
        self.assertEqual(result, {"to_date": "2025-12-01T00:00:00Z"})

    def test_same_day_range_start_of_day(self):
        start = pendulum.datetime(2025, 12, 1, 0, 0, 0)
        end = pendulum.datetime(2025, 12, 1, 0, 0, 0)
        result = build_date_params(start_date=start, end_date=end)
        self.assertEqual(
            result,
            {
                "from_date": "2025-12-01T00:00:00Z",
                "to_date": "2025-12-02T00:00:00Z",
            },
        )

    def test_same_day_range_end_of_day(self):
        start = pendulum.datetime(2025, 12, 1, 0, 0, 0)
        end = pendulum.datetime(2025, 12, 1, 23, 59, 59)
        result = build_date_params(start_date=start, end_date=end)
        self.assertEqual(
            result,
            {
                "from_date": "2025-12-01T00:00:00Z",
                "to_date": "2025-12-02T00:00:00Z",
            },
        )


if __name__ == "__main__":
    unittest.main()
