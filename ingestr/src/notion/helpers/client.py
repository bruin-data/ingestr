from typing import Any, Dict, Iterator, Optional

from dlt.sources.helpers import requests

from ..settings import API_URL


class NotionClient:
    """A client to interact with the Notion API.

    Attributes:
        api_key (str): The Notion API secret key.
    """

    def __init__(self, api_key: Optional[str] = None):
        self.api_key = api_key

    def _create_headers(self) -> Dict[str, str]:
        headers = {
            "accept": "application/json",
            "Notion-Version": "2022-06-28",
            "Authorization": f"Bearer {self.api_key}",
        }
        return headers

    def _filter_out_none_values(self, dict_in: Dict[str, Any]) -> Dict[str, Any]:
        return {k: v for k, v in dict_in.items() if v is not None}

    def get_endpoint(
        self, resource: str, resource_id: str, subresource: Optional[str] = None
    ) -> str:
        """Returns the endpoint for a given resource.

        Args:
            resource (str): The resource to get the endpoint for.
            resource_id (str): The id of the resource.
            subresource (str, optional): The subresource to get the endpoint for.

        Returns:
            str: The endpoint for the resource.
        """
        url = f"{API_URL}/{resource}/{resource_id}"
        if subresource:
            url += f"/{subresource}"
        return url

    def fetch_resource(
        self, resource: str, resource_id: str, subresource: Optional[str] = None
    ) -> Any:
        """Fetches a resource from the Notion API.

        Args:
            resource (str): The resource to fetch.
            resource_id (str): The id of the resource.
            subresource (str, optional): The subresource to fetch. Defaults to None.

        Returns:
            Any: The resource from the Notion API.
        """
        url = self.get_endpoint(resource, resource_id, subresource)
        headers = self._create_headers()
        response = requests.get(url, headers=headers)
        response.raise_for_status()
        return response.json()

    def send_payload(
        self,
        resource: str,
        resource_id: str,
        subresource: Optional[str] = None,
        query_params: Optional[Dict[str, Any]] = None,
        payload: Optional[Dict[str, Any]] = None,
    ) -> Any:
        """Sends a payload to the Notion API using the POST method.

        Args:
            resource (str): The resource to send the payload to.
            resource_id (str): The id of the resource.
            subresource (str, optional): The subresource to send the payload to.
                Defaults to None.
            query_params (Dict[str, Any], optional): The query parameters to send
                with the payload. Defaults to None.
            payload (Dict[str, Any], optional): The payload to send. Defaults to None.

        Returns:
            Any: The response from the Notion API.

        Raises:
            requests.HTTPError: If the response from the Notion API is not 200.
        """

        url = self.get_endpoint(resource, resource_id, subresource)
        headers = self._create_headers()

        if payload is None:
            payload = {}

        filtered_payload = self._filter_out_none_values(payload)

        response = requests.post(
            url, headers=headers, params=query_params, json=filtered_payload
        )
        response.raise_for_status()
        return response.json()

    def search(
        self,
        query: Optional[str] = None,
        filter_criteria: Optional[Dict[str, Any]] = None,
        sort: Optional[Dict[str, Any]] = None,
        start_cursor: Optional[str] = None,
        page_size: Optional[int] = None,
    ) -> Iterator[Dict[str, Any]]:
        """Searches all parent or child pages and databases that have been
        shared with an integration.

        Notion API Reference. Search:
            https://developers.notion.com/reference/post-search

        Args:
            query (str, optional): The string to search for. Defaults to None.
            filter_criteria (Dict[str, Any], optional): The filter to apply to
                the results.
            sort (Dict[str, Any], optional): The sort to apply to the results.
            start_cursor (str, optional): The cursor to start the query at.
                Defaults to None.
            page_size (int, optional): The number of results to return.
                Defaults to None.

        Yields:
            Dict[str, Any]: A result from the search.
        """
        has_more = True

        while has_more:
            payload = {
                "query": query,
                "sort": sort,
                "filter": filter_criteria,
                "start_cursor": start_cursor,
                "page_size": page_size,
            }

            filtered_payload = self._filter_out_none_values(payload)

            response = self.send_payload("search", "", payload=filtered_payload)

            for result in response.get("results", []):
                yield result

            next_cursor = response.get("next_cursor")
            has_more = next_cursor is not None
            start_cursor = next_cursor

    def get_database(self, database_id: str) -> Any:
        """Fetches the details of a specific database by its ID.

        Args:
            database_id (str): The ID of the database to fetch.

        Returns:
            Any: The details of the database.
        """
        return self.fetch_resource("databases", database_id)
