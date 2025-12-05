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

"""A source that extracts data from Notion API"""

from typing import Dict, Iterator, List, Optional

import dlt
from dlt.sources import DltResource

from .helpers.client import NotionClient
from .helpers.database import NotionDatabase


@dlt.source(max_table_nesting=1)
def notion_databases(
    database_ids: Optional[List[Dict[str, str]]] = None,
    api_key: str = dlt.secrets.value,
) -> Iterator[DltResource]:
    """
    Retrieves data from Notion databases.

    Args:
        database_ids (List[Dict[str, str]], optional): A list of dictionaries
            each containing a database id and a name.
            Defaults to None. If None, the function will generate all databases
            in the workspace that are accessible to the integration.
        api_key (str): The Notion API secret key.

    Yields:
        DltResource: Data resources from Notion databases.
    """
    notion_client = NotionClient(api_key)

    if database_ids is None:
        search_results = notion_client.search(
            filter_criteria={"value": "database", "property": "object"}
        )
        database_ids = [
            {"id": result["id"], "use_name": result["title"][0]["plain_text"]}
            for result in search_results
        ]

    for database in database_ids:
        if "use_name" not in database:
            # Fetch the database details from Notion
            details = notion_client.get_database(database["id"])

            # Extract the name/title from the details
            database["use_name"] = details["title"][0]["plain_text"]

        notion_database = NotionDatabase(database["id"], notion_client)
        yield dlt.resource(  # type: ignore
            notion_database.query(),
            primary_key="id",
            name=database["use_name"],
            write_disposition="replace",
        )
