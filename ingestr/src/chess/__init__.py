"""A source loading player profiles and games from chess.com api"""

from typing import Any, Callable, Dict, Iterator, List, Sequence

import dlt
from dlt.common import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from dlt.sources.helpers import requests

from .helpers import get_path_with_retry, get_url_with_retry, validate_month_string
from .settings import UNOFFICIAL_CHESS_API_URL


@dlt.source(name="chess")
def source(
    players: List[str], start_month: str = None, end_month: str = None
) -> Sequence[DltResource]:
    """
    A dlt source for the chess.com api. It groups several resources (in this case chess.com API endpoints) containing
    various types of data: user profiles or chess match results
    Args:
        players (List[str]): A list of the player usernames for which to get the data.
        start_month (str, optional): Filters out all the matches happening before `start_month`. Defaults to None.
        end_month (str, optional): Filters out all the matches happening after `end_month`. Defaults to None.
    Returns:
        Sequence[DltResource]: A sequence of resources that can be selected from including players_profiles,
        players_archives, players_games, players_online_status
    """
    return (
        players_profiles(players),
        players_archives(players),
        players_games(players, start_month=start_month, end_month=end_month),
        players_online_status(players),
    )


@dlt.resource(
    write_disposition="replace",
    columns={
        "last_online": {"data_type": "timestamp"},
        "joined": {"data_type": "timestamp"},
    },
)
def players_profiles(players: List[str]) -> Iterator[TDataItem]:
    """
    Yields player profiles for a list of player usernames.
    Args:
        players (List[str]): List of player usernames to retrieve profiles for.
    Yields:
        Iterator[TDataItem]: An iterator over player profiles data.
    """

    # get archives in parallel by decorating the http request with defer
    @dlt.defer
    def _get_profile(username: str) -> TDataItem:
        return get_path_with_retry(f"player/{username}")

    for username in players:
        yield _get_profile(username)


@dlt.resource(write_disposition="replace", selected=False)
def players_archives(players: List[str]) -> Iterator[List[TDataItem]]:
    """
    Yields url to game archives for specified players.
    Args:
        players (List[str]): List of player usernames to retrieve archives for.
    Yields:
        Iterator[List[TDataItem]]: An iterator over list of player archive data.
    """
    for username in players:
        data = get_path_with_retry(f"player/{username}/games/archives")
        yield data.get("archives", [])


@dlt.resource(
    write_disposition="append", columns={"end_time": {"data_type": "timestamp"}}
)
def players_games(
    players: List[str], start_month: str = None, end_month: str = None
) -> Iterator[Callable[[], List[TDataItem]]]:
    """
    Yields `players` games that happened between `start_month` and `end_month`.
    Args:
        players (List[str]): List of player usernames to retrieve games for.
        start_month (str, optional): The starting month in the format "YYYY/MM". Defaults to None.
        end_month (str, optional): The ending month in the format "YYYY/MM". Defaults to None.
    Yields:
        Iterator[Callable[[], List[TDataItem]]]: An iterator over callables that return a list of games for each player.
    """  # do a simple validation to prevent common mistakes in month format
    validate_month_string(start_month)
    validate_month_string(end_month)

    # get a list of already checked archives
    # from your point of view, the state is python dictionary that will have the same content the next time this function is called
    checked_archives = dlt.current.resource_state().setdefault("archives", [])
    # get player archives, note that you can call the resource like any other function and just iterate it like a list
    archives = players_archives(players)

    # get archives in parallel by decorating the http request with defer
    @dlt.defer
    def _get_archive(url: str) -> List[TDataItem]:
        print(f"Getting archive from {url}")
        try:
            games = get_url_with_retry(url).get("games", [])
            return games  # type: ignore
        except requests.HTTPError as http_err:
            # sometimes archives are not available and the error seems to be permanent
            if http_err.response.status_code == 404:
                return []
            raise

    # enumerate the archives
    for url in archives:
        # the `url` format is https://api.chess.com/pub/player/{username}/games/{YYYY}/{MM}
        if start_month and url[-7:] < start_month:
            continue
        if end_month and url[-7:] > end_month:
            continue
        # do not download archive again
        if url in checked_archives:
            continue
        checked_archives.append(url)
        # get the filtered archive
        yield _get_archive(url)


@dlt.resource(write_disposition="append")
def players_online_status(players: List[str]) -> Iterator[TDataItem]:
    """
    Returns current online status for a list of players.
    Args:
        players (List[str]): List of player usernames to check online status for.
    Yields:
        Iterator[TDataItem]: An iterator over the online status of each player.
    """
    # we'll use unofficial endpoint to get online status, the official seems to be removed
    for player in players:
        status = get_url_with_retry(f"{UNOFFICIAL_CHESS_API_URL}user/popup/{player}")
        # return just relevant selection
        yield {
            "username": player,
            "onlineStatus": status["onlineStatus"],
            "lastLoginDate": status["lastLoginDate"],
            "check_time": pendulum.now(),  # dlt can deal with native python dates
        }


@dlt.source
def chess_dlt_config_example(
    secret_str: str = dlt.secrets.value,
    secret_dict: Dict[str, Any] = dlt.secrets.value,
    config_int: int = dlt.config.value,
) -> DltResource:
    """
    An example of a source that uses dlt to provide secrets and config values.
    Args:
        secret_str (str, optional): Secret string provided by dlt.secrets.value. Defaults to dlt.secrets.value.
        secret_dict (Dict[str, Any], optional): Secret dictionary provided by dlt.secrets.value. Defaults to dlt.secrets.value.
        config_int (int, optional): Config integer provided by dlt.config.value. Defaults to dlt.config.value.
    Returns:
        DltResource: Returns a resource yielding the configured values.
    """

    # returns a resource yielding the configured values - it is just a test
    return dlt.resource([secret_str, secret_dict, config_int], name="config_values")
