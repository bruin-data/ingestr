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
