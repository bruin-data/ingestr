import enum
import typing
from binascii import hexlify
from codecs import decode as codecs_decode
from collections import defaultdict
from datetime import date
from datetime import datetime as Datetime
from datetime import time
from datetime import timedelta as Timedelta
from datetime import timezone as Timezone
from decimal import Decimal
from enum import Enum
from json import loads
from struct import Struct

from redshift_connector.config import (
    EPOCH,
    EPOCH_SECONDS,
    EPOCH_TZ,
    FC_BINARY,
    FC_TEXT,
    _client_encoding,
    timegm,
)
from redshift_connector.interval import (
    Interval,
    IntervalDayToSecond,
    IntervalYearToMonth,
)
from redshift_connector.pg_types import (
    PGEnum,
    PGJson,
    PGJsonb,
    PGText,
    PGTsvector,
    PGVarchar,
)
from redshift_connector.utils.oids import RedshiftOID


def pack_funcs(fmt: str) -> typing.Tuple[typing.Callable, typing.Callable]:
    struc: Struct = Struct("!" + fmt)
    return struc.pack, struc.unpack_from


i_pack, i_unpack = pack_funcs("i")
I_pack, I_unpack = pack_funcs("I")
h_pack, h_unpack = pack_funcs("h")
q_pack, q_unpack = pack_funcs("q")
d_pack, d_unpack = pack_funcs("d")
f_pack, f_unpack = pack_funcs("f")
iii_pack, iii_unpack = pack_funcs("iii")
ii_pack, ii_unpack = pack_funcs("ii")
qhh_pack, qhh_unpack = pack_funcs("qhh")
qii_pack, qii_unpack = pack_funcs("qii")
dii_pack, dii_unpack = pack_funcs("dii")
ihihih_pack, ihihih_unpack = pack_funcs("ihihih")
ci_pack, ci_unpack = pack_funcs("ci")
bh_pack, bh_unpack = pack_funcs("bh")
cccc_pack, cccc_unpack = pack_funcs("cccc")
qq_pack, qq_unpack = pack_funcs("qq")


def text_recv(data: bytes, offset: int, length: int) -> str:
    return str(data[offset : offset + length], _client_encoding)


def bool_recv(data: bytes, offset: int, length: int) -> bool:
    return data[offset] == 1


# bytea
def bytea_recv(data: bytearray, offset: int, length: int) -> bytearray:
    return data[offset : offset + length]


def int8_recv(data: bytes, offset: int, length: int) -> int:
    return q_unpack(data, offset)[0]


def int2_recv(data: bytes, offset: int, length: int) -> int:
    return h_unpack(data, offset)[0]


def vector_in(data: bytes, idx: int, length: int) -> typing.List:
    return eval("[" + data[idx : idx + length].decode(_client_encoding).replace(" ", ",") + "]")


def int4_recv(data: bytes, offset: int, length: int) -> int:
    return i_unpack(data, offset)[0]


def int_in(data: bytes, offset: int, length: int) -> int:
    return int(data[offset : offset + length])


def oid_recv(data: bytes, offset: int, length: int) -> int:
    return I_unpack(data, offset)[0]


def json_in(data: bytes, offset: int, length: int) -> typing.Dict[str, typing.Any]:
    return loads(str(data[offset : offset + length], _client_encoding))


def float4_recv(data: bytes, offset: int, length: int) -> float:
    return f_unpack(data, offset)[0]


def float8_recv(data: bytes, offset: int, length: int) -> float:
    return d_unpack(data, offset)[0]


def varbyte_send(v: bytearray) -> bytes:
    return v.hex().encode(_client_encoding)


# def uuid_send(v: UUID) -> bytes:
#     return v.bytes


def bool_send(v: bool) -> bytes:
    return b"\x01" if v else b"\x00"


NULL: bytes = i_pack(-1)
NULL_BYTE: bytes = b"\x00"


def null_send(v) -> bytes:
    return NULL


# data is 64-bit integer representing microseconds since 2000-01-01
def timestamp_recv_integer(data: bytes, offset: int, length: int) -> typing.Union[Datetime, str, float]:
    micros: float = q_unpack(data, offset)[0]
    try:
        return EPOCH + Timedelta(microseconds=micros)
    except OverflowError:
        epoch_delta: Timedelta = Timedelta(seconds=EPOCH_SECONDS)
        d_delta: Timedelta = Timedelta(microseconds=micros)
        if d_delta < epoch_delta:
            return Datetime.min
        else:
            return Datetime.max


def abstime_recv(data: bytes, offset: int, length: int) -> Datetime:
    """
    Converts abstime values represented as integer, representing seconds, to Python datetime.Datetime using UTC.
    """
    if length != 4:
        raise Exception("Malformed column value of type abstime received")

    time: float = i_unpack(data, offset)[0]
    time *= 1000000  # Time in micro secs
    secs: float = time / 1000000
    nanos: int = int(time - secs * 1000000)
    if nanos < 0:
        secs -= 1
        nanos += 1000000

    nanos *= 1000

    millis: float = secs * float(1000)

    total_secs: float = (millis / 1e3) + (nanos / 1e9)
    server_date: Datetime = Datetime.fromtimestamp(total_secs)
    # As of Version 3.0, times are no longer read and written using Greenwich Mean Time;
    # the input and output routines default to the local time zone.
    # Ref https://www.postgresql.org/docs/6.3/c0804.htm#abstime
    return server_date.astimezone(Timezone.utc)


# data is 64-bit integer representing microseconds since 2000-01-01
def timestamp_send_integer(v: Datetime) -> bytes:
    return q_pack(int((timegm(v.timetuple()) - EPOCH_SECONDS) * 1e6) + v.microsecond)


def timestamptz_send_integer(v: Datetime) -> bytes:
    # timestamps should be sent as UTC.  If they have zone info,
    # convert them.
    return timestamp_send_integer(v.astimezone(Timezone.utc).replace(tzinfo=None))


# return a timezone-aware datetime instance if we're reading from a
# "timestamp with timezone" type.  The timezone returned will always be
# UTC, but providing that additional information can permit conversion
# to local.
def timestamptz_recv_integer(data: bytes, offset: int, length: int) -> typing.Union[str, Datetime, int]:
    micros: int = q_unpack(data, offset)[0]
    try:
        return EPOCH_TZ + Timedelta(microseconds=micros)
    except OverflowError:
        epoch_delta: Timedelta = Timedelta(seconds=EPOCH_SECONDS)
        d_delta: Timedelta = Timedelta(microseconds=micros)
        if d_delta < epoch_delta:
            return Datetime.min
        else:
            return Datetime.max


def interval_send_integer(v: typing.Union[Timedelta, Interval]) -> bytes:
    microseconds: int = int(v.total_seconds() * 1e6)

    try:
        months = v.months  # type: ignore
    except AttributeError:
        months = 0

    return typing.cast(bytes, qhh_pack(microseconds, 0, months))


def intervaly2m_send_integer(v: IntervalYearToMonth) -> bytes:
    months = v.months  # type: ignore

    return typing.cast(bytes, i_pack(months))


def intervald2s_send_integer(v: IntervalDayToSecond) -> bytes:
    microseconds = v.microseconds  # type: ignore

    return typing.cast(bytes, q_pack(microseconds))


glbls: typing.Dict[str, type] = {"Decimal": Decimal}
trans_tab = dict(zip(map(ord, "{}"), "[]"))


# def array_in(data: bytes, idx: int, length: int) -> typing.List:
#     arr: typing.List[str] = []
#     prev_c = None
#     for c in data[idx:idx + length].decode(
#             _client_encoding).translate(
#         trans_tab).replace('NULL', 'None'):
#         if c not in ('[', ']', ',', 'N') and prev_c in ('[', ','):
#             arr.extend("Decimal('")
#         elif c in (']', ',') and prev_c not in ('[', ']', ',', 'e'):
#             arr.extend("')")
#
#         arr.append(c)
#         prev_c = c
#     return typing.cast(typing.List, eval(''.join(arr), glbls))


def numeric_in_binary(data: bytes, offset: int, length: int, scale: int) -> Decimal:
    raw_value: int

    if length == 8 or length == 16:
        raw_value = int.from_bytes(data[offset : offset + length], byteorder="big", signed=True)
    else:
        raise Exception("Malformed column value of type numeric received")

    return Decimal(raw_value).scaleb(-1 * scale)


def numeric_to_float_binary(data: bytes, offset: int, length: int, scale: int) -> float:
    raw_value: int

    if length == 8 or length == 16:
        raw_value = int.from_bytes(data[offset : offset + length], byteorder="big", signed=True)
    else:
        raise Exception("Malformed column value of type numeric received")

    return raw_value * 10 ** (-1 * scale)


def numeric_in(data: bytes, offset: int, length: int) -> Decimal:
    return Decimal(data[offset : offset + length].decode(_client_encoding))


def numeric_to_float_in(data: bytes, offset: int, length: int) -> float:
    return float(data[offset : offset + length].decode(_client_encoding))


# def uuid_recv(data: bytes, offset: int, length: int) -> UUID:
#     return UUID(bytes=data[offset:offset+length])


def interval_recv_integer(data: bytes, offset: int, length: int) -> typing.Union[Timedelta, Interval]:
    microseconds, days, months = typing.cast(typing.Tuple[int, ...], qhh_unpack(data, offset))
    seconds, micros = divmod(microseconds, 1e6)
    if months != 0:
        return Interval(microseconds, days, months)
    else:
        return Timedelta(days, seconds, micros)


def intervaly2m_recv_integer(data: bytes, offset: int, length: int) -> IntervalYearToMonth:
    (months,) = typing.cast(typing.Tuple[int], i_unpack(data, offset))
    return IntervalYearToMonth(months)


def intervald2s_recv_integer(data: bytes, offset: int, length: int) -> IntervalDayToSecond:
    (microseconds,) = typing.cast(typing.Tuple[int], q_unpack(data, offset))
    return IntervalDayToSecond(microseconds)


def timetz_recv_binary(data: bytes, offset: int, length: int) -> time:
    return time_recv_binary(data, offset, length).replace(tzinfo=Timezone.utc)


# data is 64-bit integer representing microseconds
def time_recv_binary(data: bytes, offset: int, length: int) -> time:
    millis: float = q_unpack(data, offset)[0] / 1000

    if length == 12:
        time_offset: int = i_unpack(data, offset + 8)[0]  # tz lives after time
        time_offset *= -1000
        millis -= time_offset

    q, r = divmod(millis, 1000)
    micros: float = r * 1000  # maximum of six digits of precision for fractional seconds.
    q, r = divmod(q, 60)
    seconds: float = r
    q, r = divmod(q, 60)
    minutes: float = r
    hours: float = q
    return time(hour=int(hours), minute=int(minutes), second=int(seconds), microsecond=int(micros))


def time_in(data: bytes, offset: int, length: int) -> time:
    hour: int = int(data[offset : offset + 2])
    minute: int = int(data[offset + 3 : offset + 5])
    sec: Decimal = Decimal(data[offset + 6 : offset + length].decode(_client_encoding))
    return time(hour, minute, int(sec), int((sec - int(sec)) * 1000000))


def timetz_in(data: bytes, offset: int, length: int) -> time:
    hour: int = int(data[offset : offset + 2])
    minute: int = int(data[offset + 3 : offset + 5])
    sec: Decimal = Decimal(data[offset + 6 : offset + 8].decode(_client_encoding))
    microsec: int = int((sec - int(sec)) * 1000000)

    if length != 8:
        idx_tz: int = offset + 8
        # if microsec present, they start with '.'
        if data[idx_tz : idx_tz + 1] == b".":
            end_microseconds: int = length + offset
            for idx in range(idx_tz + 1, len(data)):
                if data[idx] == 43 or data[idx] == 45:  # +/- char indicates start of tz offset
                    end_microseconds = idx
                    break

            microsec += int(data[idx_tz + 1 : end_microseconds])
    return time(hour, minute, int(sec), microsec, tzinfo=Timezone.utc)


def date_recv_binary(data: bytes, offset: int, length: int) -> date:
    # 86400 seconds per day
    seconds: float = i_unpack(data, offset)[0] * 86400

    # Julian/Gregorian calendar cutoff point
    if seconds < -12219292800:  # October 4, 1582 -> October 15, 1582
        seconds += 864000  # add 10 days worth of seconds
        if seconds < -14825808000:  # 1500-02-28 -> 1500-03-01
            extraLeaps: float = (seconds + 14825808000) / 3155760000
            extraLeaps -= 1
            extraLeaps -= extraLeaps / 4
            seconds += extraLeaps * 86400

    microseconds: float = seconds * 1e6

    try:
        return (EPOCH + Timedelta(microseconds=microseconds)).date()
    except OverflowError:
        if Timedelta(microseconds=microseconds) < Timedelta(seconds=EPOCH_SECONDS):
            return date.min
        else:
            return date.max
    except Exception as e:
        raise e


def date_in(data: bytes, offset: int, length: int) -> date:
    d: str = data[offset : offset + length].decode(_client_encoding)

    # datetime module does not support BC dates, so return min date
    if d[-1] == "C":
        return date.min
    try:
        return date(int(d[:4]), int(d[5:7]), int(d[8:10]))
    except ValueError:
        # likely occurs if a date > datetime.datetime.max
        return date.max


class ArrayState(Enum):
    InString = 1
    InEscape = 2
    InValue = 3
    Out = 4


# parses an array received in text format. currently all elements are returned as strings.


def _parse_array(adapter: typing.Optional[typing.Callable], data: bytes, offset: int, length: int) -> typing.List:
    state: ArrayState = ArrayState.Out
    stack: typing.List = [[]]
    val: typing.List[str] = []
    str_data: str = text_recv(data, offset, length)

    for c in str_data:
        if state == ArrayState.InValue:
            if c in ("}", ","):
                value: typing.Optional[str] = "".join(val)
                if value == "NULL":
                    value = None
                elif adapter is not None:
                    value = adapter(value)
                stack[-1].append(value)
                state = ArrayState.Out
            else:
                val.append(c)

        if state == ArrayState.Out:
            if c == "{":
                a: typing.List = []
                stack[-1].append(a)
                stack.append(a)
            elif c == "}":
                stack.pop()
            elif c == ",":
                pass
            elif c == '"':
                val = []
                state = ArrayState.InString
            else:
                val = [c]
                state = ArrayState.InValue

        elif state == ArrayState.InString:
            if c == '"':
                value = "".join(val)
                if adapter is not None:
                    value = adapter(value)  # type: ignore
                stack[-1].append(value)
                state = ArrayState.Out
            elif c == "\\":
                state = ArrayState.InEscape
            else:
                val.append(c)
        elif state == ArrayState.InEscape:
            val.append(c)
            state = ArrayState.InString

    return stack[0][0]


def _array_in(adapter: typing.Optional[typing.Callable] = None):
    def f(data: bytes, offset: int, length: int):
        return _parse_array(adapter, data, offset, length)

    return f


array_recv_text: typing.Callable = _array_in()
int_array_recv: typing.Callable = _array_in(lambda data: int(data))
float_array_recv: typing.Callable = _array_in(lambda data: float(data))


def array_recv_binary(data: bytes, idx: int, length: int) -> typing.List:
    final_idx: int = idx + length
    dim, hasnull, typeoid = iii_unpack(data, idx)
    idx += 12

    # get type conversion method for typeoid
    conversion: typing.Callable = redshift_types[typeoid][1]

    # Read dimension info
    dim_lengths: typing.List = []
    for i in range(dim):
        dim_lengths.append(ii_unpack(data, idx)[0])
        idx += 8

    # Read all array values
    values: typing.List = []
    while idx < final_idx:
        (element_len,) = i_unpack(data, idx)
        idx += 4
        if element_len == -1:
            values.append(None)
        else:
            values.append(conversion(data, idx, element_len))
            idx += element_len

    # at this point, {{1,2,3},{4,5,6}}::int[][] looks like
    # [1,2,3,4,5,6]. go through the dimensions and fix up the array
    # contents to match expected dimensions
    for length in reversed(dim_lengths[1:]):
        values = list(map(list, zip(*[iter(values)] * length)))
    return values


ascii_invalid_value: int = 0x7F


def hexencoding_lookup_no_case(input_value: int) -> int:
    if input_value == 48:
        return 0x00
    elif input_value == 49:
        return 0x01
    elif input_value == 50:
        return 0x02
    elif input_value == 51:
        return 0x03
    elif input_value == 52:
        return 0x04
    elif input_value == 53:
        return 0x05
    elif input_value == 54:
        return 0x06
    elif input_value == 55:
        return 0x07
    elif input_value == 56:
        return 0x08
    elif input_value == 57:
        return 0x09
    elif (input_value == 65) or (input_value == 97):
        return 0x0A
    elif (input_value == 66) or (input_value == 98):
        return 0x0B
    elif (input_value == 67) or (input_value == 99):
        return 0x0C
    elif (input_value == 68) or (input_value == 100):
        return 0x0D
    elif (input_value == 69) or (input_value == 101):
        return 0x0E
    elif (input_value == 70) or (input_value == 102):
        return 0x0F
    else:
        return ascii_invalid_value


def geographyhex_recv(data: bytes, idx: int, length: int) -> str:
    return hexlify(data[idx : idx + length]).decode(_client_encoding)


def geometryhex_recv(data: bytes, idx: int, length: int) -> str:
    error_flag: bool = False
    pointer: int = idx

    if data is None:
        return ""
    elif length == 0:
        return ""
    else:
        # EWT is always hex encoded
        # check to see if byte is expected length
        if 1 == ((idx + length - pointer) % 2):
            return data[idx : idx + length].hex()

        result: bytearray = bytearray((idx + length - pointer) // 2)

        i: int = 0
        while pointer < (idx + length):
            # get the ascii number encoded
            stage: int = hexencoding_lookup_no_case(data[pointer]) << 4
            # error check
            error_flag = (stage == ascii_invalid_value) | error_flag
            pointer += 1
            stage2 = hexencoding_lookup_no_case(data[pointer])
            error_flag = (stage2 == ascii_invalid_value) | error_flag
            pointer += 1

            result[i] = stage | stage2
            i += 1

        if error_flag:
            return data[idx : idx + length].hex()

        return result.hex()


def varbytehex_recv(data: bytes, idx: int, length: int) -> str:
    return codecs_decode(data[idx : idx + length], "hex_codec").decode(_client_encoding)


# def inet_in(data: bytes, offset: int, length: int) -> typing.Union[IPv4Address, IPv6Address, IPv4Network, IPv6Network]:
#     inet_str: str = data[offset: offset + length].decode(
#         _client_encoding)
#     if '/' in inet_str:
#         return typing.cast(typing.Union[IPv4Network, IPv6Network], ip_network(inet_str, False))
#     else:
#         return typing.cast(typing.Union[IPv4Address, IPv6Address], ip_address(inet_str))


redshift_types: typing.DefaultDict[int, typing.Tuple[int, typing.Callable]] = defaultdict(
    lambda: (FC_TEXT, text_recv),
    {
        RedshiftOID.ABSTIME: (FC_BINARY, abstime_recv),  # abstime
        RedshiftOID.BOOLEAN: (FC_BINARY, bool_recv),  # boolean
        # 17: (FC_BINARY, bytea_recv),  # bytea
        RedshiftOID.NAME: (FC_BINARY, text_recv),  # name type
        RedshiftOID.BIGINT: (FC_BINARY, int8_recv),  # int8
        RedshiftOID.SMALLINT: (FC_BINARY, int2_recv),  # int2
        RedshiftOID.SMALLINT_VECTOR: (FC_TEXT, vector_in),  # int2vector
        RedshiftOID.INTEGER: (FC_BINARY, int4_recv),  # int4
        RedshiftOID.REGPROC: (FC_BINARY, oid_recv),  # regproc
        RedshiftOID.TEXT: (FC_BINARY, text_recv),  # TEXT type
        RedshiftOID.OID: (FC_BINARY, oid_recv),  # oid
        RedshiftOID.XID: (FC_TEXT, int_in),  # xid
        RedshiftOID.JSON: (FC_TEXT, json_in),  # json
        RedshiftOID.REAL: (FC_BINARY, float4_recv),  # float4
        RedshiftOID.FLOAT: (FC_BINARY, float8_recv),  # float8
        RedshiftOID.UNKNOWN: (FC_BINARY, text_recv),  # unknown
        # 829: (FC_TEXT, text_recv),  # MACADDR type
        # 869: (FC_TEXT, inet_in),  # inet
        # 1000: (FC_BINARY, array_recv),  # BOOL[]
        # 1003: (FC_BINARY, array_recv),  # NAME[]
        RedshiftOID.SMALLINT_ARRAY: (FC_BINARY, array_recv_binary),  # INT2[]
        RedshiftOID.INTEGER_ARRAY: (FC_BINARY, array_recv_binary),  # INT4[]
        RedshiftOID.TEXT_ARRAY: (FC_BINARY, array_recv_binary),  # TEXT[]
        RedshiftOID.CHAR_ARRAY: (FC_BINARY, array_recv_binary),  # CHAR[]
        # 1014: (FC_BINARY, array_recv_text),  # BPCHAR[]
        RedshiftOID.OID_ARRAY: (FC_BINARY, int_array_recv),  # OID[]
        RedshiftOID.ACLITEM: (FC_BINARY, text_recv),  # ACLITEM
        RedshiftOID.ACLITEM_ARRAY: (FC_BINARY, array_recv_binary),  # ACLITEM[]
        RedshiftOID.VARCHAR_ARRAY: (FC_BINARY, array_recv_binary),  # VARCHAR[]
        # 1016: (FC_BINARY, array_recv),  # INT8[]
        RedshiftOID.REAL_ARRAY: (FC_BINARY, array_recv_binary),  # FLOAT4[]
        # 1022: (FC_BINARY, array_recv),  # FLOAT8[]
        RedshiftOID.CHAR: (FC_BINARY, text_recv),  # CHAR type
        RedshiftOID.BPCHAR: (FC_BINARY, text_recv),  # BPCHAR type
        RedshiftOID.STRING: (FC_BINARY, text_recv),  # VARCHAR type
        RedshiftOID.DATE: (FC_BINARY, date_recv_binary),  # date
        RedshiftOID.TIME: (FC_BINARY, time_recv_binary),  # time
        RedshiftOID.TIMESTAMP: (FC_BINARY, timestamp_recv_integer),  # timestamp
        RedshiftOID.TIMESTAMPTZ: (FC_BINARY, timestamptz_recv_integer),  # timestamptz
        RedshiftOID.TIMETZ: (FC_BINARY, timetz_recv_binary),  # timetz
        RedshiftOID.INTERVAL: (FC_BINARY, interval_recv_integer),
        RedshiftOID.INTERVALY2M: (FC_BINARY, intervaly2m_recv_integer),
        RedshiftOID.INTERVALD2S: (FC_BINARY, intervald2s_recv_integer),
        # 1231: (FC_TEXT, array_in),  # NUMERIC[]
        # 1263: (FC_BINARY, array_recv),  # cstring[]
        RedshiftOID.NUMERIC: (FC_BINARY, numeric_in_binary),  # NUMERIC
        # 2275: (FC_BINARY, text_recv),  # cstring
        # 2950: (FC_BINARY, uuid_recv),  # uuid
        RedshiftOID.GEOGRAPHY: (FC_BINARY, geographyhex_recv),  # GEOGRAPHY
        RedshiftOID.GEOMETRY: (FC_TEXT, text_recv),  # GEOMETRY
        RedshiftOID.GEOMETRYHEX: (FC_TEXT, geometryhex_recv),  # GEOMETRYHEX
        # 3802: (FC_TEXT, json_in),  # jsonb
        RedshiftOID.SUPER: (FC_TEXT, text_recv),  # SUPER
        RedshiftOID.VARBYTE: (FC_TEXT, varbytehex_recv),  # VARBYTE
    },
)


def text_out(v: typing.Union[PGText, PGVarchar, PGJson, PGJsonb, PGTsvector, str]) -> bytes:
    return v.encode(_client_encoding)


def enum_out(v: typing.Union[PGEnum, enum.Enum]) -> bytes:
    return str(v.value).encode(_client_encoding)


def time_out(v: time) -> bytes:
    return v.isoformat().encode(_client_encoding)


def date_out(v: date) -> bytes:
    return v.isoformat().encode(_client_encoding)


def unknown_out(v) -> bytes:
    return str(v).encode(_client_encoding)


def numeric_out(d: Decimal) -> bytes:
    return str(d).encode(_client_encoding)


# def inet_out(v: typing.Union[IPv4Address, IPv6Address, IPv4Network, IPv6Network]) -> bytes:
#     return str(v).encode(_client_encoding)


py_types: typing.Dict[typing.Union[type, int], typing.Tuple[int, int, typing.Callable]] = {
    type(None): (-1, FC_BINARY, null_send),  # null
    bool: (RedshiftOID.BOOLEAN, FC_BINARY, bool_send),
    # bytearray: (17, FC_BINARY, bytea_send),  # bytea
    RedshiftOID.BIGINT: (RedshiftOID.BIGINT, FC_BINARY, q_pack),  # int8
    RedshiftOID.SMALLINT: (RedshiftOID.SMALLINT, FC_BINARY, h_pack),  # int2
    RedshiftOID.INTEGER: (RedshiftOID.INTEGER, FC_BINARY, i_pack),  # int4
    PGText: (RedshiftOID.TEXT, FC_TEXT, text_out),  # text
    float: (RedshiftOID.FLOAT, FC_BINARY, d_pack),  # float8
    PGEnum: (RedshiftOID.UNKNOWN, FC_TEXT, enum_out),
    date: (RedshiftOID.DATE, FC_TEXT, date_out),  # date
    time: (RedshiftOID.TIME, FC_TEXT, time_out),  # time
    RedshiftOID.TIMESTAMP: (RedshiftOID.TIMESTAMP, FC_BINARY, timestamp_send_integer),  # timestamp
    # timestamp w/ tz
    PGVarchar: (RedshiftOID.STRING, FC_TEXT, text_out),  # varchar
    RedshiftOID.TIMESTAMPTZ: (RedshiftOID.TIMESTAMPTZ, FC_BINARY, timestamptz_send_integer),
    PGJson: (RedshiftOID.JSON, FC_TEXT, text_out),
    # PGJsonb: (3802, FC_TEXT, text_out),
    Timedelta: (RedshiftOID.INTERVAL, FC_BINARY, interval_send_integer),  # interval
    Interval: (RedshiftOID.INTERVAL, FC_BINARY, interval_send_integer),
    IntervalYearToMonth: (RedshiftOID.INTERVALY2M, FC_BINARY, intervaly2m_send_integer),
    IntervalDayToSecond: (RedshiftOID.INTERVALD2S, FC_BINARY, intervald2s_send_integer),
    Decimal: (RedshiftOID.NUMERIC, FC_TEXT, numeric_out),  # Decimal
    PGTsvector: (3614, FC_TEXT, text_out),
    # UUID: (2950, FC_BINARY, uuid_send),  # uuid
    bytes: (RedshiftOID.UNKNOWN, FC_TEXT, varbyte_send),  # varbyte
    str: (RedshiftOID.UNKNOWN, FC_TEXT, text_out),  # unknown
    enum.Enum: (RedshiftOID.UNKNOWN, FC_TEXT, enum_out),
    # IPv4Address: (869, FC_TEXT, inet_out),  # inet
    # IPv6Address: (869, FC_TEXT, inet_out),  # inet
    # IPv4Network: (869, FC_TEXT, inet_out),  # inet
    # IPv6Network: (869, FC_TEXT, inet_out)  # inet
}
