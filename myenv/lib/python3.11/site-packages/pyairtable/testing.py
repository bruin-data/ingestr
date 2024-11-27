"""
Helper functions for writing tests that use the pyairtable library.
"""

import datetime
import random
import string
from typing import Any, Optional

from pyairtable.api.types import AttachmentDict, CollaboratorDict, Fields, RecordDict


def fake_id(type: str = "rec", value: Any = None) -> str:
    """
    Generate a fake Airtable-style ID.

    Args:
        type: the object type prefix, defaults to "rec"
        value: any value to use as the ID, defaults to random letters and digits

    >>> fake_id()
    'rec...'
    >>> fake_id('tbl')
    'tbl...'
    >>> fake_id(value='12345')
    'rec00000000012345'
    """
    if value is None:
        value = "".join(random.sample(string.ascii_letters + string.digits, 14))
    return type + f"{value:0>14}"[:14]


def fake_meta(
    base_id: str = "appFakeTestingApp",
    table_name: str = "tblFakeTestingTbl",
    api_key: str = "patFakePersonalAccessToken",
) -> type:
    """
    Generate a ``Meta`` class for inclusion in a ``Model`` subclass.
    """
    attrs = {"base_id": base_id, "table_name": table_name, "api_key": api_key}
    return type("Meta", (), attrs)


def fake_record(
    fields: Optional[Fields] = None,
    id: Optional[str] = None,
    **other_fields: Any,
) -> RecordDict:
    """
    Generate a fake record dict with the given field values.

    >>> fake_record({"Name": "Alice"})
    {'id': '...', 'createdTime': '...', 'fields': {'Name': 'Alice'}}

    >>> fake_record(name='Alice', address='123 Fake St')
    {'id': '...', 'createdTime': '...', 'fields': {'name': 'Alice', 'address': '123 Fake St'}}

    >>> fake_record(name='Alice', id='123')
    {'id': 'rec00000000000123', 'createdTime': '...', 'fields': {'name': 'Alice'}}
    """
    return {
        "id": fake_id(value=id),
        "createdTime": datetime.datetime.now().isoformat() + "Z",
        "fields": {**(fields or {}), **other_fields},
    }


def fake_user(value: Any = None) -> CollaboratorDict:
    """
    Generate a fake user dict with the given value for an email prefix.

    >>> fake_user("alice")
    {'id': 'usr000000000Alice', 'email': 'alice@example.com', 'name': 'Fake User'}
    """
    id = fake_id("usr", value)
    return {
        "id": id,
        "email": f"{value or id}@example.com",
        "name": "Fake User",
    }


def fake_attachment() -> AttachmentDict:
    """
    Generate a fake attachment dict.
    """
    return {
        "id": fake_id("att"),
        "url": "https://example.com/",
        "filename": "foo.txt",
        "size": 100,
        "type": "text/plain",
    }
