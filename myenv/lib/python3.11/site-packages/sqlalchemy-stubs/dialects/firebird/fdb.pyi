from typing import Any

from .kinterbasdb import FBDialect_kinterbasdb as FBDialect_kinterbasdb
from ... import util as util

class FBDialect_fdb(FBDialect_kinterbasdb):
    def __init__(
        self, enable_rowcount: bool = ..., retaining: bool = ..., **kwargs: Any
    ) -> None: ...
    @classmethod
    def dbapi(cls): ...
    def create_connect_args(self, url: Any): ...

dialect = FBDialect_fdb
