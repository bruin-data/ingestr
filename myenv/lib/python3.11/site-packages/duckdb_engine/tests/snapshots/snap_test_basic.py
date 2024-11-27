# snapshottest: v1 - https://goo.gl/zC4yUc

from snapshottest import GenericRepr, Snapshot

snapshots = Snapshot()

snapshots["test_get_columns 1"] = [
    {
        "autoincrement": False,
        "comment": None,
        "default": None,
        "name": "id",
        "nullable": True,
        "type": GenericRepr("INTEGER()"),
    }
]
