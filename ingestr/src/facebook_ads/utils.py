from typing import Dict

import dlt
from dlt.common.configuration.inject import with_config
from dlt.sources.helpers import requests


@with_config(sections=("sources", "facebook_ads"))
def debug_access_token(
    access_token: str = dlt.secrets.value,
    client_id: str = dlt.secrets.value,
    client_secret: str = dlt.secrets.value,
) -> str:
    """Debugs the `access_token` providing info on expiration time, scopes etc. If arguments are not provides, `dlt` will inject them from configuration"""
    debug_url = f"https://graph.facebook.com/debug_token?input_token={access_token}&access_token={client_id}|{client_secret}"
    response = requests.get(debug_url)
    data: Dict[str, str] = response.json()

    if "error" in data:
        raise Exception(f"Error debugging token: {data['error']}")

    return data["data"]


@with_config(sections=("sources", "facebook_ads"))
def get_long_lived_token(
    access_token: str = dlt.secrets.value,
    client_id: str = dlt.secrets.value,
    client_secret: str = dlt.secrets.value,
) -> str:
    """Gets the long lived access token (60 days) from `access_token`. If arguments are not provides, `dlt` will inject them from configuration"""
    exchange_url = f"https://graph.facebook.com/v13.0/oauth/access_token?grant_type=fb_exchange_token&client_id={client_id}&client_secret={client_secret}&fb_exchange_token={access_token}"
    response = requests.get(exchange_url)
    data: Dict[str, str] = response.json()

    if "error" in data:
        raise Exception(f"Error refreshing token: {data['error']}")

    return data["access_token"]
