import re
from datetime import date, datetime
from typing import Any

from pyairtable.api.types import Fields

from .utils import date_to_iso_str, datetime_to_iso_str


def match(dict_values: Fields, *, match_any: bool = False) -> str:
    """
    Create one or more ``EQUAL()`` expressions for each provided dict value.
    If more than one assetions is included, the expressions are
    groupped together into using ``AND()`` (all values must match).

    If ``match_any=True``, expressions are grouped with ``OR()``, record is return
    if any of the values match.

    This function also handles escaping field names and casting python values
    to the appropriate airtable types using :func:`to_airtable_value` on all
    provided values to help generate the expected formula syntax.

    If you need more advanced matching you can build similar expressions using lower
    level forumula primitives.


    Args:
        dict_values: dictionary containing column names and values

    Keyword Args:
        match_any (``bool``, default: ``False``):
            If ``True``, matches if **any** of the provided values match.
            Otherwise, all values must match.

    Usage:
        >>> match({"First Name": "John", "Age": 21})
        "AND({First Name}='John',{Age}=21)"
        >>> match({"First Name": "John", "Age": 21}, match_any=True)
        "OR({First Name}='John',{Age}=21)"
        >>> match({"First Name": "John"})
        "{First Name}='John'"
        >>> match({"Registered": True})
        "{Registered}=1"
        >>> match({"Owner's Name": "Mike"})
        "{Owner\\'s Name}='Mike'"

    """
    expressions = []
    for key, value in dict_values.items():
        expression = EQUAL(FIELD(key), to_airtable_value(value))
        expressions.append(expression)

    if len(expressions) == 0:
        return ""
    elif len(expressions) == 1:
        return expressions[0]
    else:
        if not match_any:
            return AND(*expressions)
        else:
            return OR(*expressions)


def escape_quotes(value: str) -> str:
    r"""
    Ensures any quotes are escaped. Already escaped quotes are ignored.

    Args:
        value: text to be escaped

    Usage:
        >>> escape_quotes("Player's Name")
        Player\'s Name
        >>> escape_quotes("Player\'s Name")
        Player\'s Name
    """
    escaped_value = re.sub("(?<!\\\\)'", "\\'", value)
    return escaped_value


def to_airtable_value(value: Any) -> Any:
    """
    Cast value to appropriate airtable types and format.
    For example, to check ``bool`` values in formulas, you actually to compare
    to 0 and 1.

    .. list-table::
        :widths: 25 75
        :header-rows: 1

        * - Input
          - Output
        * - ``bool``
          - ``int``
        * - ``str``
          - ``str``; text is wrapped in `'single quotes'`; existing quotes are escaped.
        * - all others
          - unchanged

    Args:
        value: value to be cast.

    """
    if isinstance(value, bool):
        return int(value)
    elif isinstance(value, (int, float)):
        return value
    elif isinstance(value, str):
        return STR_VALUE(value)
    elif isinstance(value, datetime):
        return datetime_to_iso_str(value)
    elif isinstance(value, date):
        return date_to_iso_str(value)
    else:
        return value


def EQUAL(left: Any, right: Any) -> str:
    """
    Create an equality assertion

    >>> EQUAL(2,2)
    '2=2'
    """
    return "{}={}".format(left, right)


def FIELD(name: str) -> str:
    """
    Create a reference to a field. Quotes are escaped.

    Args:
        name: field name

    Usage:
        >>> FIELD("First Name")
        '{First Name}'
        >>> FIELD("Guest's Name")
        "{Guest\\'s Name}"
    """
    return "{%s}" % escape_quotes(name)


def STR_VALUE(value: str) -> str:
    """
    Wrap string in quotes. This is needed when referencing a string inside a formula.
    Quotes are escaped.

    >>> STR_VALUE("John")
    "'John'"
    >>> STR_VALUE("Guest's Name")
    "'Guest\\'s Name'"
    >>> EQUAL(STR_VALUE("John"), FIELD("First Name"))
    "'John'={First Name}"
    """
    return "'{}'".format(escape_quotes(str(value)))


def IF(logical: str, value1: str, value2: str) -> str:
    """
    Create an IF statement

    >>> IF(1=1, 0, 1)
    'IF(1=1, 0, 1)'
    """
    return "IF({}, {}, {})".format(logical, value1, value2)


def FIND(what: str, where: str, start_position: int = 0) -> str:
    """
    Create a FIND statement

    >>> FIND(STR_VALUE(2021), FIELD('DatetimeCol'))
    "FIND('2021', {DatetimeCol})"

    Args:
        what: String to search for
        where: Where to search. Could be a string, or a field reference.
        start_position: Index of where to start search. Default is 0.

    """
    if start_position:
        return "FIND({}, {}, {})".format(what, where, start_position)
    else:
        return "FIND({}, {})".format(what, where)


def AND(*args: str) -> str:
    """
    Create an AND Statement

    >>> AND(1, 2, 3)
    'AND(1, 2, 3)'
    """
    return "AND({})".format(",".join(args))


def OR(*args: str) -> str:
    """
    .. versionadded:: 1.2.0

    Creates an OR Statement

    >>> OR(1, 2, 3)
    'OR(1, 2, 3)'
    """
    return "OR({})".format(",".join(args))


def LOWER(value: str) -> str:
    """
    .. versionadded:: 1.3.0

    Creates the LOWER function, making a string lowercase.
    Can be used on a string or a field name and will lower all the strings in the field.

    >>> LOWER("TestValue")
    "LOWER(TestValue)"
    """
    return "LOWER({})".format(value)


def NOT_EQUAL(left: Any, right: Any) -> str:
    """
    Create an inequality assertion

    >>> NOT_EQUAL(2,2)
    '2!=2'
    """
    return "{}!={}".format(left, right)


def LESS_EQUAL(left: Any, right: Any) -> str:
    """
    Create a less than assertion

    >>> LESS_EQUAL(2,2)
    '2<=2'
    """
    return "{}<={}".format(left, right)


def GREATER_EQUAL(left: Any, right: Any) -> str:
    """
    Create a greater than assertion

    >>> GREATER_EQUAL(2,2)
    '2>=2'
    """
    return "{}>={}".format(left, right)


def LESS(left: Any, right: Any) -> str:
    """
    Create a less assertion

    >>> LESS(2,2)
    '2<2'
    """
    return "{}<{}".format(left, right)


def GREATER(left: Any, right: Any) -> str:
    """
    Create a greater assertion

    >>> GREATER(2,2)
    '2>2'
    """
    return "{}>{}".format(left, right)
