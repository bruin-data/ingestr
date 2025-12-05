# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

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
