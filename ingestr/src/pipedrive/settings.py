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

"""Pipedrive source settings and constants"""

ENTITY_MAPPINGS = [
    ("activity", "activityFields", {"user_id": 0}),
    ("organization", "organizationFields", None),
    ("person", "personFields", None),
    ("product", "productFields", None),
    ("deal", "dealFields", None),
    ("pipeline", None, None),
    ("stage", None, None),
    ("user", None, None),
]

RECENTS_ENTITIES = {
    "activity": "activities",
    "activityType": "activity_types",
    "deal": "deals",
    "file": "files",
    "filter": "filters",
    "note": "notes",
    "person": "persons",
    "organization": "organizations",
    "pipeline": "pipelines",
    "product": "products",
    "stage": "stages",
    "user": "users",
}
