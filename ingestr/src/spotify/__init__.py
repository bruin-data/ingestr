from typing import Any, Dict, Iterator, Sequence

import dlt
from dlt.sources import DltResource

from .helpers import SpotifyClient


def _flatten_common(item: Dict[str, Any]) -> Dict[str, Any]:
    external_urls = item.pop("external_urls", None)
    if external_urls:
        item["spotify_url"] = external_urls.get("spotify", "")

    images = item.pop("images", None)
    if images:
        item["image_urls"] = ",".join(
            img.get("url", "") for img in images if img.get("url")
        )

    item.pop("available_markets", None)
    item.pop("restrictions", None)
    item.pop("href", None)
    item.pop("linked_from", None)

    languages = item.pop("languages", None)
    if languages:
        item["languages"] = ",".join(languages)

    return item


def flatten_track(item: Dict[str, Any]) -> Dict[str, Any]:
    item = _flatten_common(item)

    album = item.pop("album", None)
    item["album_name"] = album.get("name", "") if album else ""
    item["album_id"] = album.get("id", "") if album else ""
    item["album_type"] = album.get("album_type", "") if album else ""
    item["album_release_date"] = album.get("release_date", "") if album else ""
    item["album_total_tracks"] = album.get("total_tracks", 0) if album else 0
    item["album_uri"] = album.get("uri", "") if album else ""
    album_images = album.get("images", []) if album else []
    item["album_image_urls"] = ",".join(
        img.get("url", "") for img in album_images if img.get("url")
    )
    album_artists = album.get("artists", []) if album else []
    item["album_artist_names"] = ",".join(a.get("name", "") for a in album_artists)
    item["album_artist_ids"] = ",".join(a.get("id", "") for a in album_artists)

    artists = item.pop("artists", None)
    item["artist_names"] = ",".join(a.get("name", "") for a in artists) if artists else ""
    item["artist_ids"] = ",".join(a.get("id", "") for a in artists) if artists else ""

    external_ids = item.pop("external_ids", None)
    item["isrc"] = external_ids.get("isrc", "") if external_ids else ""
    item["ean"] = external_ids.get("ean", "") if external_ids else ""
    item["upc"] = external_ids.get("upc", "") if external_ids else ""

    return item


def flatten_artist(item: Dict[str, Any]) -> Dict[str, Any]:
    item = _flatten_common(item)

    followers = item.pop("followers", None)
    item["followers_total"] = followers.get("total", 0) if followers else 0

    genres = item.pop("genres", None)
    item["genres"] = ",".join(genres) if genres else ""

    return item


def flatten_album(item: Dict[str, Any]) -> Dict[str, Any]:
    item = _flatten_common(item)

    artists = item.pop("artists", None)
    item["artist_names"] = ",".join(a.get("name", "") for a in artists) if artists else ""
    item["artist_ids"] = ",".join(a.get("id", "") for a in artists) if artists else ""

    return item


def flatten_playlist(item: Dict[str, Any]) -> Dict[str, Any]:
    item = _flatten_common(item)

    owner = item.pop("owner", None)
    item["owner_name"] = owner.get("display_name", "") if owner else ""
    item["owner_id"] = owner.get("id", "") if owner else ""

    tracks_obj = item.pop("tracks", None)
    item["total_tracks"] = tracks_obj.get("total", 0) if tracks_obj and isinstance(tracks_obj, dict) else 0

    item.pop("items", None)

    return item


def flatten_show(item: Dict[str, Any]) -> Dict[str, Any]:
    item = _flatten_common(item)

    item.pop("html_description", None)
    item.pop("copyrights", None)

    return item


def flatten_episode(item: Dict[str, Any]) -> Dict[str, Any]:
    item = _flatten_common(item)

    item.pop("html_description", None)

    show = item.pop("show", None)
    item["show_name"] = show.get("name", "") if show else ""
    item["show_id"] = show.get("id", "") if show else ""
    item["show_publisher"] = show.get("publisher", "") if show else ""

    resume_point = item.pop("resume_point", None)
    item["fully_played"] = resume_point.get("fully_played", False) if resume_point else False
    item["resume_position_ms"] = resume_point.get("resume_position_ms", 0) if resume_point else 0

    return item


def flatten_audiobook(item: Dict[str, Any]) -> Dict[str, Any]:
    item = _flatten_common(item)

    item.pop("html_description", None)
    item.pop("copyrights", None)

    authors = item.pop("authors", None)
    item["author_names"] = ",".join(a.get("name", "") for a in authors) if authors else ""

    narrators = item.pop("narrators", None)
    item["narrator_names"] = ",".join(n.get("name", "") for n in narrators) if narrators else ""

    chapters = item.pop("chapters", None)
    item["total_chapters"] = chapters.get("total", 0) if chapters and isinstance(chapters, dict) else 0

    return item


@dlt.source(name="spotify", max_table_nesting=0)
def spotify_source(
    client_id: str,
    client_secret: str,
    query: str,
    market: str = "US",
) -> Sequence[DltResource]:
    client = SpotifyClient(client_id=client_id, client_secret=client_secret)

    @dlt.resource(name="tracks", write_disposition="replace", primary_key="id")
    def tracks() -> Iterator[Dict[str, Any]]:
        for page in client.fetch_all(query, "track", "tracks", market=market):
            for item in page:
                yield flatten_track(item)

    @dlt.resource(name="artists", write_disposition="replace", primary_key="id")
    def artists() -> Iterator[Dict[str, Any]]:
        for page in client.fetch_all(query, "artist", "artists", market=market):
            for item in page:
                yield flatten_artist(item)

    @dlt.resource(name="albums", write_disposition="replace", primary_key="id")
    def albums() -> Iterator[Dict[str, Any]]:
        for page in client.fetch_all(query, "album", "albums", market=market):
            for item in page:
                yield flatten_album(item)

    @dlt.resource(name="playlists", write_disposition="replace", primary_key="id")
    def playlists() -> Iterator[Dict[str, Any]]:
        for page in client.fetch_all(query, "playlist", "playlists", market=market):
            for item in page:
                yield flatten_playlist(item)

    @dlt.resource(name="shows", write_disposition="replace", primary_key="id")
    def shows() -> Iterator[Dict[str, Any]]:
        for page in client.fetch_all(query, "show", "shows", market=market):
            for item in page:
                yield flatten_show(item)

    @dlt.resource(name="episodes", write_disposition="replace", primary_key="id")
    def episodes() -> Iterator[Dict[str, Any]]:
        for page in client.fetch_all(query, "episode", "episodes", market=market):
            for item in page:
                yield flatten_episode(item)

    @dlt.resource(name="audiobooks", write_disposition="replace", primary_key="id")
    def audiobooks() -> Iterator[Dict[str, Any]]:
        for page in client.fetch_all(query, "audiobook", "audiobooks", market=market):
            for item in page:
                yield flatten_audiobook(item)

    return [tracks, artists, albums, playlists, shows, episodes, audiobooks]
