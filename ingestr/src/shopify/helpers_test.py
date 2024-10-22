import pytest

from .helpers import remove_nodes_key


@pytest.mark.parametrize(
    "input_data, expected_output",
    [
        ({"nodes": [{"id": 1}, {"id": 2}]}, [{"id": 1}, {"id": 2}]),
        ({"data": {"nodes": [{"id": 1}, {"id": 2}]}}, {"data": [{"id": 1}, {"id": 2}]}),
        (
            {
                "data": {
                    "nodes": [
                        {"id": 1, "nested": {"nodes": [{"id": "a"}, {"id": "b"}]}}
                    ]
                }
            },
            {"data": [{"id": 1, "nested": [{"id": "a"}, {"id": "b"}]}]},
        ),
        (
            {"data": [{"nodes": [{"id": 1}]}, {"nodes": [{"id": 2}]}]},
            {"data": [[{"id": 1}], [{"id": 2}]]},
        ),
        (
            {"data": {"not_nodes": [{"id": 1}, {"id": 2}]}},
            {"data": {"not_nodes": [{"id": 1}, {"id": 2}]}},
        ),
        ({"nodes": "not a list"}, {"nodes": "not a list"}),
        ([1, 2, 3], [1, 2, 3]),
        ("string", "string"),
    ],
)
def test_remove_nodes_key(input_data, expected_output):
    assert remove_nodes_key(input_data) == expected_output
