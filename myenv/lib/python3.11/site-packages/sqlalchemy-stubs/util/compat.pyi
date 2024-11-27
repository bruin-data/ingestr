import collections
from collections import namedtuple as namedtuple  # noqa
import contextlib
import operator
import sys
import threading as threading  # noqa
from types import CodeType
from typing import Any
from typing import Callable
from typing import Dict
from typing import Iterator
from typing import List
from typing import NamedTuple
from typing import Optional
from typing import Sequence
from typing import Tuple
from typing import Type
from typing import Union

if sys.version_info >= (3, 0):
    from _typeshed import SupportsLessThan

    _SortKeyFunction = Callable[[Any], SupportsLessThan]
else:
    _SortKeyFunction = Callable[[Any], Any]

py38: bool
py37: bool
py3k: bool
py2k: bool
pypy: bool
cpython: bool
win32: bool
osx: bool
arm: bool
has_refcount_gc: bool

contextmanager = contextlib.contextmanager
dottedgetter = operator.attrgetter
namedtuple = collections.namedtuple
next = next

class FullArgSpec(NamedTuple):
    args: List[str]
    varargs: Optional[str]
    varkw: Optional[str]
    defaults: Optional[Tuple[Any, ...]]
    kwonlyargs: List[str]
    kwonlydefaults: Optional[Dict[str, Any]]
    annotations: Dict[str, Any]

class nullcontext:
    enter_result: Optional[Any] = ...
    def __init__(self, enter_result: Optional[Any] = ...) -> None: ...
    def __enter__(self) -> Optional[Any]: ...
    def __exit__(self, *excinfo: Any) -> None: ...

def inspect_getfullargspec(func: Any) -> FullArgSpec: ...

string_types: Tuple[Type[Any], ...]
binary_types: Tuple[Type[Any], ...]
int_types: Tuple[Type[Any], ...]

if sys.version_info >= (3, 0):
    import builtins
    import configparser
    import itertools
    import pickle as pickle  # noqa

    from functools import reduce as reduce  # noqa
    from io import BytesIO as _byte_buffer
    from io import StringIO as StringIO  # noqa
    from itertools import zip_longest as zip_longest  # noqa
    from time import perf_counter as perf_counter  # noqa
    from urllib.parse import (
        quote_plus as quote_plus,
        unquote_plus as unquote_plus,
        parse_qsl as parse_qsl,
        quote as quote,
        unquote as unquote,
    )

    binary_type = bytes
    text_type = str
    iterbytes = iter
    long_type = int

    itertools_filterfalse = itertools.filterfalse
    itertools_filter = filter
    itertools_imap = map

    exec_ = builtins.exec
    import_ = builtins.__import__
    print_ = builtins.print

    byte_buffer = _byte_buffer
    def b64decode(x: text_type) -> binary_type: ...
    def b64encode(x: binary_type) -> text_type: ...
    def cmp(__a: Any, __b: Any) -> int: ...
    def u(s: text_type) -> text_type: ...
    def ue(s: text_type) -> text_type: ...
    from typing import TYPE_CHECKING as TYPE_CHECKING  # noqa

    callable = callable

    from abc import ABC as ABC  # noqa
    import collections.abc

    collections_abc = collections.abc
else:
    import base64
    import ConfigParser as configparser  # noqa
    import itertools
    from typing import Mapping

    from StringIO import StringIO as StringIO  # noqa
    from cStringIO import StringIO as _byte_buffer
    from itertools import izip_longest as _zip_longest
    from time import clock as _perf_counter
    from urllib import quote as quote  # noqa
    from urllib import quote_plus as quote_plus  # noqa
    from urllib import unquote as unquote  # noqa
    from urllib import unquote_plus as unquote_plus  # noqa
    from urlparse import parse_qsl as parse_qsl  # noqa

    from abc import ABCMeta
    class ABC(object):
        __metaclass__ = ABCMeta
    import pickle as pickle  # noqa

    binary_type = str
    text_type = unicode  # noqa: F821
    long_type = long  # noqa: F821

    callable = callable
    cmp = cmp
    reduce = reduce

    b64encode = base64.b64encode
    b64decode = base64.b64decode

    itertools_filterfalse = itertools.ifilterfalse
    itertools_filter = itertools.ifilter
    itertools_imap = itertools.imap

    byte_buffer = _byte_buffer
    perf_counter = _perf_counter
    zip_longest = _zip_longest
    def exec_(
        __source: Union[unicode, str, CodeType],  # noqa: F821
        __globals: Optional[Dict[str, Any]],
        __locals: Optional[Mapping[str, Any]],
    ) -> None: ...
    def iterbytes(__buf: binary_type) -> Iterator[int]: ...
    def import_(*args: Any) -> Any: ...
    def print_(*args: Any, **kwargs: Any) -> None: ...
    def u(s: binary_type) -> text_type: ...
    def ue(s: binary_type) -> text_type: ...
    def safe_bytestring(text: Any) -> Any: ...
    TYPE_CHECKING: bool

    collections_abc = collections

def b(s: Any) -> Any: ...
def decode_backslashreplace(text: binary_type, encoding: str) -> text_type: ...
def raise_(
    exception: Any,
    with_traceback: Optional[Any] = ...,
    replace_context: Optional[Any] = ...,
    from_: bool = ...,
) -> None: ...
def inspect_formatargspec(
    args: Any,
    varargs: Optional[Any] = ...,
    varkw: Optional[Any] = ...,
    defaults: Optional[Any] = ...,
    kwonlyargs: Any = ...,
    kwonlydefaults: Any = ...,
    annotations: Any = ...,
    formatarg: Any = ...,
    formatvarargs: Any = ...,
    formatvarkw: Any = ...,
    formatvalue: Any = ...,
    formatreturns: Any = ...,
    formatannotation: Any = ...,
) -> str: ...

if sys.version_info >= (3, 7):
    from dataclasses import Field
    def dataclass_fields(cls: Any) -> Sequence[Field[Any]]: ...
    def local_dataclass_fields(cls: Any) -> List[Field[Any]]: ...

else:
    def dataclass_fields(cls: Any) -> Sequence[Any]: ...
    def local_dataclass_fields(cls: Any) -> List[Any]: ...

def raise_from_cause(
    exception: Any, exc_info: Optional[Any] = ...
) -> None: ...
def reraise(
    tp: Any, value: Any, tb: Optional[Any] = ..., cause: Optional[Any] = ...
) -> None: ...
def with_metaclass(meta: Any, *bases: Any, **kw: Any) -> Type[Any]: ...

if sys.version_info >= (3, 0):
    from datetime import timezone as timezone  # noqa
else:
    from datetime import datetime
    from datetime import timedelta
    from datetime import tzinfo
    class timezone(tzinfo):
        def __init__(self, offset: timedelta) -> None: ...
        def __eq__(self, other: Any) -> Any: ...
        def __hash__(self) -> int: ...
        def utcoffset(self, dt: Optional[datetime]) -> timedelta: ...
        def tzname(self, dt: Optional[datetime]) -> str: ...
        def dst(self, dt: Optional[datetime]) -> None: ...
        def fromutc(self, dt: datetime) -> datetime: ...

TypingSortKeyFunction = _SortKeyFunction
