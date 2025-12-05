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

"""Github source settings and constants."""

START_DATE = "1970-01-01T00:00:00Z"

# rest queries
REST_API_BASE_URL = "https://api.github.com"
REPO_EVENTS_PATH = "/repos/%s/%s/events"

# graphql queries
GRAPHQL_API_BASE_URL = "https://api.github.com/graphql"
