# from utils import pack_funcs
import typing
from calendar import timegm
from datetime import datetime as Datetime
from datetime import timezone as Timezone
from enum import Enum, IntEnum

FC_TEXT: int = 0
FC_BINARY: int = 1
_client_encoding: str = "utf8"


class ClientProtocolVersion(IntEnum):
    BASE_SERVER = 0
    EXTENDED_RESULT_METADATA = 1
    BINARY = 2

    @classmethod
    def list(cls) -> typing.List[int]:
        return list(map(lambda p: p.value, cls))  # type: ignore

    @classmethod
    def get_name(cls, i: int) -> str:
        try:
            return ClientProtocolVersion(i).name
        except ValueError:
            return str(i)


DEFAULT_PROTOCOL_VERSION: int = ClientProtocolVersion.BINARY.value


class DbApiParamstyle(Enum):
    QMARK = "qmark"
    NUMERIC = "numeric"
    NAMED = "named"
    FORMAT = "format"
    PYFORMAT = "pyformat"

    @classmethod
    def list(cls) -> typing.List[int]:
        return list(map(lambda p: p.value, cls))  # type: ignore


min_int2: int = -(2**15)
max_int2: int = 2**15
min_int4: int = -(2**31)
max_int4: int = 2**31
min_int8: int = -(2**63)
max_int8: int = 2**63
EPOCH: Datetime = Datetime(2000, 1, 1)
EPOCH_TZ: Datetime = EPOCH.replace(tzinfo=Timezone.utc)
EPOCH_SECONDS: int = timegm(EPOCH.timetuple())
INFINITY_MICROSECONDS: int = 2**63 - 1
MINUS_INFINITY_MICROSECONDS: int = -1 * INFINITY_MICROSECONDS - 1

# pg element oid -> pg array typeoid
pg_array_types: typing.Dict[int, int] = {
    16: 1000,
    # 25: 1009,    # TEXT[]
    701: 1022,
    1043: 1009,
    # 1700: 1231,  # NUMERIC[]
}

# PostgreSQL encodings:
#   http://www.postgresql.org/docs/8.3/interactive/multibyte.html
# Python encodings:
#   http://www.python.org/doc/2.4/lib/standard-encodings.html
#
# Commented out encodings don't require a name change between Amazon Redshift and
# Python.  If the py side is None, then the encoding isn't supported.
pg_to_py_encodings: typing.Dict[str, typing.Optional[str]] = {
    # Not supported:
    "mule_internal": None,
    "euc_tw": None,
    # Name fine as-is:
    # "euc_jp",
    # "euc_jis_2004",
    # "euc_kr",
    # "gb18030",
    # "gbk",
    # "johab",
    # "sjis",
    # "shift_jis_2004",
    # "uhc",
    # "utf8",
    # Different name:
    "euc_cn": "gb2312",
    "iso_8859_5": "is8859_5",
    "iso_8859_6": "is8859_6",
    "iso_8859_7": "is8859_7",
    "iso_8859_8": "is8859_8",
    "koi8": "koi8_r",
    "latin1": "iso8859-1",
    "latin2": "iso8859_2",
    "latin3": "iso8859_3",
    "latin4": "iso8859_4",
    "latin5": "iso8859_9",
    "latin6": "iso8859_10",
    "latin7": "iso8859_13",
    "latin8": "iso8859_14",
    "latin9": "iso8859_15",
    "sql_ascii": "ascii",
    "win866": "cp886",
    "win874": "cp874",
    "win1250": "cp1250",
    "win1251": "cp1251",
    "win1252": "cp1252",
    "win1253": "cp1253",
    "win1254": "cp1254",
    "win1255": "cp1255",
    "win1256": "cp1256",
    "win1257": "cp1257",
    "win1258": "cp1258",
    "unicode": "utf-8",  # Needed for Amazon Redshift
}

table_type_clauses: typing.Dict[str, typing.Optional[typing.Dict[str, str]]] = {
    "TABLE": {
        "SCHEMAS": "c.relkind = 'r' AND n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'",
        "NOSCHEMAS": "c.relkind = 'r' AND c.relname !~ '^pg_'",
    },
    "VIEW": {
        "SCHEMAS": "c.relkind = 'v' AND n.nspname <> 'pg_catalog' AND n.nspname <> 'information_schema'",
        "NOSCHEMAS": "c.relkind = 'v' AND c.relname !~ '^pg_'",
    },
    "INDEX": {
        "SCHEMAS": "c.relkind = 'i' AND n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'",
        "NOSCHEMAS": "c.relkind = 'i' AND c.relname !~ '^pg_'",
    },
    "SEQUENCE": {"SCHEMAS": "c.relkind = 'S'", "NOSCHEMAS": "c.relkind = 'S'"},
    "TYPE": {
        "SCHEMAS": "c.relkind = 'c' AND n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'",
        "NOSCHEMAS": "c.relkind = 'c' AND c.relname !~ '^pg_'",
    },
    "SYSTEM TABLE": {
        "SCHEMAS": "c.relkind = 'r' AND (n.nspname = 'pg_catalog' OR n.nspname = 'information_schema')",
        "NOSCHEMAS": "c.relkind = 'r' AND c.relname ~ '^pg_' AND c.relname !~ '^pg_toast_' AND c.relname !~ '^pg_temp_'",
    },
    "SYSTEM TOAST TABLE": {
        "SCHEMAS": "c.relkind = 'r' AND n.nspname = 'pg_toast'",
        "NOSCHEMAS": "c.relkind = 'r' AND c.relname ~ '^pg_toast_'",
    },
    "SYSTEM TOAST INDEX": {
        "SCHEMAS": "c.relkind = 'i' AND n.nspname = 'pg_toast'",
        "NOSCHEMAS": "c.relkind = 'i' AND c.relname ~ '^pg_toast_'",
    },
    "SYSTEM VIEW": {
        "SCHEMAS": "c.relkind = 'v' AND (n.nspname = 'pg_catalog' OR n.nspname = 'information_schema') ",
        "NOSCHEMAS": "c.relkind = 'v' AND c.relname ~ '^pg_'",
    },
    "SYSTEM INDEX": {
        "SCHEMAS": "c.relkind = 'i' AND (n.nspname = 'pg_catalog' OR n.nspname = 'information_schema') ",
        "NOSCHEMAS": "c.relkind = 'v' AND c.relname ~ '^pg_' AND c.relname !~ '^pg_toast_' AND c.relname !~ '^pg_temp_'",
    },
    "TEMPORARY TABLE": {
        "SCHEMAS": "c.relkind IN ('r','p') AND n.nspname ~ '^pg_temp_' ",
        "NOSCHEMAS": "c.relkind IN ('r','p') AND c.relname ~ '^pg_temp_' ",
    },
    "TEMPORARY INDEX": {
        "SCHEMAS": "c.relkind = 'i' AND n.nspname ~ '^pg_temp_' ",
        "NOSCHEMAS": "c.relkind = 'i' AND c.relname ~ '^pg_temp_' ",
    },
    "TEMPORARY VIEW": {
        "SCHEMAS": "c.relkind = 'v' AND n.nspname ~ '^pg_temp_' ",
        "NOSCHEMAS": "c.relkind = 'v' AND c.relname ~ '^pg_temp_' ",
    },
    "TEMPORARY SEQUENCE": {
        "SCHEMAS": "c.relkind = 'S' AND n.nspname ~ '^pg_temp_' ",
        "NOSCHEMAS": "c.relkind = 'S' AND c.relname ~ '^pg_temp_' ",
    },
    "EXTERNAL TABLE": None,
    "SHARED TABLE": None,
}
