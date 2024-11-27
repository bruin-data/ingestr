#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#
from __future__ import annotations

from functools import total_ordering
from hashlib import md5
from logging import getLogger
from threading import Lock
from typing import Any, Iterable

from sortedcontainers import SortedSet

logger = getLogger(__name__)


@total_ordering
class QueryContextElement:
    def __init__(
        self, id: int, read_timestamp: int, priority: int, context: str
    ) -> None:
        # entry with id = 0 is the main entry
        self.id = id
        self.read_timestamp = read_timestamp
        # priority values are 0..N with 0 being the highest priority
        self.priority = priority
        # OpaqueContext field will be base64 encoded in GS, but it is opaque to client side. Client side should not do decoding/encoding and just store the raw data.
        self.context = context

    def __eq__(self, other: object) -> bool:
        if not isinstance(other, QueryContextElement):
            return False
        return (
            self.id == other.id
            and self.read_timestamp == other.read_timestamp
            and self.priority == other.priority
            and self.context == other.context
        )

    def __lt__(self, other: Any) -> bool:
        if not isinstance(other, QueryContextElement):
            raise TypeError(
                f"cannot compare QueryContextElement with object of type {type(other)}"
            )
        return self.priority < other.priority

    def __hash__(self) -> int:
        _hash = 31

        _hash = _hash * 31 + self.id
        _hash += (_hash * 31) + self.read_timestamp
        _hash += (_hash * 31) + self.priority
        if self.context:
            _hash += (_hash * 31) + int.from_bytes(
                md5(self.context.encode("utf-8")).digest(), "big"
            )
        return _hash

    def __str__(self) -> str:
        return f"({self.id}, {self.read_timestamp}, {self.priority})"


class QueryContextCache:
    def __init__(self, capacity: int) -> None:
        self.capacity = capacity
        self._id_map: dict[int, QueryContextElement] = {}
        self._priority_map: dict[int, QueryContextElement] = {}
        self._intermediate_priority_map: dict[int, QueryContextElement] = {}

        # stores elements sorted by priority. Element with
        # least priority value has the highest priority
        self._tree_set: set[QueryContextElement] = SortedSet()
        self._lock = Lock()
        self._data: str = None

    def _add_qce(self, qce: QueryContextElement) -> None:
        """Adds qce element in tree_set, id_map and intermediate_priority_map.
        We still need to add _sync_priority_map after all the new qce have been merged
        into the cache.
        """
        self._tree_set.add(qce)
        self._id_map[qce.id] = qce
        self._intermediate_priority_map[qce.priority] = qce

    def _remove_qce(self, qce: QueryContextElement) -> None:
        self._id_map.pop(qce.id)
        self._priority_map.pop(qce.priority)
        self._tree_set.remove(qce)

    def _replace_qce(
        self, old_qce: QueryContextElement, new_qce: QueryContextElement
    ) -> None:
        """This is just a convenience function to call a remove and add operation back-to-back"""
        self._remove_qce(old_qce)
        self._add_qce(new_qce)

    def _sync_priority_map(self):
        """
        Sync the _intermediate_priority_map with the _priority_map at the end of the current round of inserts.
        """
        logger.debug(
            f"sync_priority_map called priority_map size = {len(self._priority_map)}, new_priority_map size = {len(self._intermediate_priority_map)}"
        )

        self._priority_map.update(self._intermediate_priority_map)
        # Clear the _intermediate_priority_map for the next round of QCC insert (a round consists of multiple entries)
        self._intermediate_priority_map.clear()

    def insert(self, id: int, read_timestamp: int, priority: int, context: str) -> None:
        if id in self._id_map:
            qce = self._id_map[id]
            if (read_timestamp > qce.read_timestamp) or (
                read_timestamp == qce.read_timestamp and priority != qce.priority
            ):
                # when id if found in cache and we are operating on a more recent timestamp. We do not update in-place here.
                new_qce = QueryContextElement(id, read_timestamp, priority, context)
                self._replace_qce(qce, new_qce)
        else:
            new_qce = QueryContextElement(id, read_timestamp, priority, context)
            if priority in self._priority_map:
                old_qce = self._priority_map[priority]
                self._replace_qce(old_qce, new_qce)
            else:
                self._add_qce(new_qce)

    def trim_cache(self) -> None:
        logger.debug(
            f"trim_cache() called. treeSet size is {len(self._tree_set)} and cache capacity is {self.capacity}"
        )

        while len(self) > self.capacity:
            # remove the qce with highest priority value => element with least priority
            qce = self._last()
            self._remove_qce(qce)

        logger.debug(
            f"trim_cache() returns. treeSet size is {len(self._tree_set)} and cache capacity is {self.capacity}"
        )

    def clear_cache(self) -> None:
        logger.debug("clear_cache() called")
        self._id_map.clear()
        self._priority_map.clear()
        self._tree_set.clear()
        self._intermediate_priority_map.clear()

    def _get_elements(self) -> Iterable[QueryContextElement]:
        return self._tree_set

    def _last(self) -> QueryContextElement:
        return self._tree_set[-1]

    def serialize_to_dict(self) -> dict:
        with self._lock:
            logger.debug("serialize_to_dict() called")
            self.log_cache_entries()

            if len(self._tree_set) == 0:
                return {}  # we should return an empty dict

            try:
                data = {
                    "entries": [
                        {
                            "id": qce.id,
                            "timestamp": qce.read_timestamp,
                            "priority": qce.priority,
                            "context": (
                                {"base64Data": qce.context}
                                if qce.context is not None
                                else {}
                            ),
                        }
                        for qce in self._tree_set
                    ]
                }
                # Because on GS side, `context` field is an object with `base64Data`  string member variable,
                # we should serialize `context` field to an object instead of string directly to stay consistent with GS side.

                logger.debug(f"serialize_to_dict(): data to send to server {data}")

                # query context shoule be an object field of the HTTP request body JSON and on GS side. here we should only return a dict
                # and let the outer HTTP request body to convert the entire big dict to a single JSON.
                return data
            except Exception as e:
                logger.debug(f"serialize_to_dict(): Exception {e}")
                return {}

    def deserialize_json_dict(self, data: dict) -> None:
        with self._lock:
            logger.debug(f"deserialize_json_dict() called: data from server: {data}")
            self.log_cache_entries()

            if data is None or len(data) == 0:
                self.clear_cache()
                logger.debug("deserialize_json_dict() returns")
                self.log_cache_entries()
                return

            try:
                # Deserialize the entries. The first entry with priority 0 is the main entry. On python
                # connector side, we save all entries into one list to simplify the logic. When python
                # connector receives HTTP response, the data["queryContext"] field has been converted
                # from JSON to dict type automatically, so for this function we deserialize from python
                # dict directly. Below is an example QueryContext dict.
                # {
                #   "entries": [
                #    {
                #     "id": 0,
                #     "read_timestamp": 123456789,
                #     "priority": 0,
                #     "context": "base64 encoded context"
                #    },
                #     {
                #       "id": 1,
                #       "read_timestamp": 123456789,
                #       "priority": 1,
                #       "context": "base64 encoded context"
                #     },
                #     {
                #       "id": 2,
                #       "read_timestamp": 123456789,
                #       "priority": 2,
                #       "context": "base64 encoded context"
                #     }
                #   ]
                # }

                # Deserialize entries
                entries = data.get("entries", list())
                for entry in entries:
                    logger.debug(f"deserialize {entry}")
                    if not isinstance(entry.get("id"), int):
                        logger.debug("id type error")
                        raise TypeError(
                            f"Invalid type for 'id' field: Expected int, got {type(entry['id'])}"
                        )
                    if not isinstance(entry.get("timestamp"), int):
                        logger.debug("timestamp type error")
                        raise TypeError(
                            f"Invalid type for 'timestamp' field: Expected int, got {type(entry['timestamp'])}"
                        )
                    if not isinstance(entry.get("priority"), int):
                        logger.debug("priority type error")
                        raise TypeError(
                            f"Invalid type for 'priority' field: Expected int, got {type(entry['priority'])}"
                        )

                    # OpaqueContext field currently is empty from GS side.
                    context = entry.get("context", None)
                    if context and not isinstance(entry.get("context"), str):
                        logger.debug("context type error")
                        raise TypeError(
                            f"Invalid type for 'context' field: Expected str, got {type(entry['context'])}"
                        )
                    self.insert(
                        entry.get("id"),
                        entry.get("timestamp"),
                        entry.get("priority"),
                        context,
                    )

                # Sync the priority map at the end of for loop insert.
                self._sync_priority_map()
            except Exception as e:
                logger.debug(f"deserialize_json_dict: Exception = {e}")
                # clear cache due to incomplete insert
                self.clear_cache()

            self.trim_cache()
            logger.debug("deserialize_json_dict() returns")
            self.log_cache_entries()

    def log_cache_entries(self) -> None:
        for qce in self._tree_set:
            logger.debug(f"Cache Entry: {str(qce)}")

    def __len__(self) -> int:
        return len(self._tree_set)
