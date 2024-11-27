from typing import Any
from typing import Optional

from ... import exc as exc
from ...sql import sqltypes as sqltypes
from ...sql import type_api as type_api
from ... import util as util

class _NumericType:
    unsigned: Any = ...
    zerofill: Any = ...
    def __init__(
        self, unsigned: bool = ..., zerofill: bool = ..., **kw: Any
    ) -> None: ...

class _FloatType(_NumericType, sqltypes.Float):
    scale: Any = ...
    def __init__(
        self,
        precision: Optional[Any] = ...,
        scale: Optional[Any] = ...,
        asdecimal: bool = ...,
        **kw: Any,
    ) -> None: ...

class _IntegerType(_NumericType, sqltypes.Integer):
    display_width: Any = ...
    def __init__(
        self, display_width: Optional[Any] = ..., **kw: Any
    ) -> None: ...

class _StringType(sqltypes.String):
    charset: Any = ...
    ascii: Any = ...
    unicode: Any = ...
    binary: Any = ...
    national: Any = ...
    def __init__(
        self,
        charset: Optional[Any] = ...,
        collation: Optional[Any] = ...,
        ascii: bool = ...,
        binary: bool = ...,
        unicode: bool = ...,
        national: bool = ...,
        **kw: Any,
    ) -> None: ...

class _MatchType(sqltypes.Float, sqltypes.MatchType):
    def __init__(self, **kw: Any) -> None: ...

class NUMERIC(_NumericType, sqltypes.NUMERIC):
    def __init__(
        self,
        precision: Optional[Any] = ...,
        scale: Optional[Any] = ...,
        asdecimal: bool = ...,
        **kw: Any,
    ) -> None: ...

class DECIMAL(_NumericType, sqltypes.DECIMAL):
    def __init__(
        self,
        precision: Optional[Any] = ...,
        scale: Optional[Any] = ...,
        asdecimal: bool = ...,
        **kw: Any,
    ) -> None: ...

class DOUBLE(_FloatType):
    def __init__(
        self,
        precision: Optional[Any] = ...,
        scale: Optional[Any] = ...,
        asdecimal: bool = ...,
        **kw: Any,
    ) -> None: ...

class REAL(_FloatType, sqltypes.REAL):
    def __init__(
        self,
        precision: Optional[Any] = ...,
        scale: Optional[Any] = ...,
        asdecimal: bool = ...,
        **kw: Any,
    ) -> None: ...

class FLOAT(_FloatType, sqltypes.FLOAT):
    def __init__(
        self,
        precision: Optional[Any] = ...,
        scale: Optional[Any] = ...,
        asdecimal: bool = ...,
        **kw: Any,
    ) -> None: ...
    def bind_processor(self, dialect: Any) -> None: ...

class INTEGER(_IntegerType, sqltypes.INTEGER):
    def __init__(
        self, display_width: Optional[Any] = ..., **kw: Any
    ) -> None: ...

class BIGINT(_IntegerType, sqltypes.BIGINT):
    def __init__(
        self, display_width: Optional[Any] = ..., **kw: Any
    ) -> None: ...

class MEDIUMINT(_IntegerType):
    def __init__(
        self, display_width: Optional[Any] = ..., **kw: Any
    ) -> None: ...

class TINYINT(_IntegerType):
    def __init__(
        self, display_width: Optional[Any] = ..., **kw: Any
    ) -> None: ...

class SMALLINT(_IntegerType, sqltypes.SMALLINT):
    def __init__(
        self, display_width: Optional[Any] = ..., **kw: Any
    ) -> None: ...

class BIT(type_api.TypeEngine):
    length: Any = ...
    def __init__(self, length: Optional[Any] = ...) -> None: ...
    def result_processor(self, dialect: Any, coltype: Any): ...

class TIME(sqltypes.TIME):
    fsp: Any = ...
    def __init__(
        self, timezone: bool = ..., fsp: Optional[Any] = ...
    ) -> None: ...
    def result_processor(self, dialect: Any, coltype: Any): ...

class TIMESTAMP(sqltypes.TIMESTAMP):
    fsp: Any = ...
    def __init__(
        self, timezone: bool = ..., fsp: Optional[Any] = ...
    ) -> None: ...

class DATETIME(sqltypes.DATETIME):
    fsp: Any = ...
    def __init__(
        self, timezone: bool = ..., fsp: Optional[Any] = ...
    ) -> None: ...

class YEAR(type_api.TypeEngine):
    display_width: Any = ...
    def __init__(self, display_width: Optional[Any] = ...) -> None: ...

class TEXT(_StringType, sqltypes.TEXT):
    def __init__(self, length: Optional[Any] = ..., **kw: Any) -> None: ...

class TINYTEXT(_StringType):
    def __init__(self, **kwargs: Any) -> None: ...

class MEDIUMTEXT(_StringType):
    def __init__(self, **kwargs: Any) -> None: ...

class LONGTEXT(_StringType):
    def __init__(self, **kwargs: Any) -> None: ...

class VARCHAR(_StringType, sqltypes.VARCHAR):
    def __init__(self, length: Optional[Any] = ..., **kwargs: Any) -> None: ...

class CHAR(_StringType, sqltypes.CHAR):
    def __init__(self, length: Optional[Any] = ..., **kwargs: Any) -> None: ...

class NVARCHAR(_StringType, sqltypes.NVARCHAR):
    def __init__(self, length: Optional[Any] = ..., **kwargs: Any) -> None: ...

class NCHAR(_StringType, sqltypes.NCHAR):
    def __init__(self, length: Optional[Any] = ..., **kwargs: Any) -> None: ...

class TINYBLOB(sqltypes.TypingBinary): ...
class MEDIUMBLOB(sqltypes.TypingBinary): ...
class LONGBLOB(sqltypes.TypingBinary): ...

TypingStringType = _StringType
