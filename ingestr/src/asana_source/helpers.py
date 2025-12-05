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

"""Asana source helpers"""

from asana import Client as AsanaClient


def get_client(
    access_token: str,
) -> AsanaClient:
    """
    Returns an Asana API client.
    Args:
        access_token (str): The access token to authenticate the Asana API client.
    Returns:
        AsanaClient: The Asana API client.
    """
    return AsanaClient.access_token(access_token)
