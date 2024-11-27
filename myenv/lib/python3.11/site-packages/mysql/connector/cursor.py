# Copyright (c) 2009, 2024, Oracle and/or its affiliates.
#
# This program is free software; you can redistribute it and/or modify
# it under the terms of the GNU General Public License, version 2.0, as
# published by the Free Software Foundation.
#
# This program is designed to work with certain software (including
# but not limited to OpenSSL) that is licensed under separate terms,
# as designated in a particular file or component or in included license
# documentation. The authors of MySQL hereby grant you an
# additional permission to link the program and your derivative works
# with the separately licensed software that they have either included with
# the program or referenced in the documentation.
#
# Without limiting anything contained in the foregoing, this file,
# which is part of MySQL Connector/Python, is also subject to the
# Universal FOSS Exception, version 1.0, a copy of which can be found at
# http://oss.oracle.com/licenses/universal-foss-exception.
#
# This program is distributed in the hope that it will be useful, but
# WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.
# See the GNU General Public License, version 2.0, for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program; if not, write to the Free Software Foundation, Inc.,
# 51 Franklin St, Fifth Floor, Boston, MA 02110-1301  USA

# mypy: disable-error-code="assignment,arg-type,attr-defined,index,override,call-overload"

"""Cursor classes."""
from __future__ import annotations

import re
import unicodedata
import warnings

from collections import deque, namedtuple
from decimal import Decimal
from typing import (
    TYPE_CHECKING,
    Any,
    Deque,
    Dict,
    Generator,
    Iterator,
    List,
    NoReturn,
    Optional,
    Sequence,
    Tuple,
    Union,
    cast,
)

from .abstracts import NAMED_TUPLE_CACHE, MySQLCursorAbstract
from .constants import ServerFlag
from .errors import (
    Error,
    InterfaceError,
    NotSupportedError,
    ProgrammingError,
    get_mysql_exception,
)
from .types import (
    DescriptionType,
    EofPacketType,
    ParamsDictType,
    ParamsSequenceOrDictType,
    ParamsSequenceType,
    ResultType,
    RowItemType,
    RowType,
    StrOrBytes,
    WarningType,
)

if TYPE_CHECKING:
    from .connection import MySQLConnection


SQL_COMMENT = r"\/\*.*?\*\/"
RE_SQL_COMMENT = re.compile(
    rf"""({SQL_COMMENT})|(["'`][^"'`]*?({SQL_COMMENT})[^"'`]*?["'`])""",
    re.I | re.M | re.S,
)
RE_SQL_ON_DUPLICATE = re.compile(
    r"""\s*ON\s+DUPLICATE\s+KEY(?:[^"'`]*["'`][^"'`]*["'`])*[^"'`]*$""",
    re.I | re.M | re.S,
)
RE_SQL_INSERT_STMT = re.compile(
    rf"({SQL_COMMENT}|\s)*INSERT({SQL_COMMENT}|\s)"
    r"*(?:IGNORE\s+)?INTO\s+[`'\"]?.+[`'\"]?(?:\.[`'\"]?.+[`'\"]?)"
    r"{0,2}\s+VALUES\s*(\(.+\)).*",
    re.I | re.M | re.S,
)
RE_SQL_INSERT_VALUES = re.compile(r".*VALUES\s*(\(.+\)).*", re.I | re.M | re.S)
RE_PY_PARAM = re.compile(b"(%s)")
RE_PY_MAPPING_PARAM = re.compile(
    rb"""
    %
    \((?P<mapping_key>[^)]+)\)
    (?P<conversion_type>[diouxXeEfFgGcrs%])
    """,
    re.X,
)
RE_SQL_SPLIT_STMTS = re.compile(
    b""";(?=(?:[^"'`]*(?:"[^"]*"|'[^']*'|`[^`]*`))*[^"'`]*$)"""
)
RE_SQL_FIND_PARAM = re.compile(b"""%s(?=(?:[^"'`]*["'`][^"'`]*["'`])*[^"'`]*$)""")
RE_SQL_PYTHON_REPLACE_PARAM = re.compile(r"%\(.*?\)s")
RE_SQL_PYTHON_CAPTURE_PARAM_NAME = re.compile(r"%\((.*?)\)s")

ERR_NO_RESULT_TO_FETCH = "No result set to fetch from"

MAX_RESULTS = 4294967295


def is_eol_comment(stmt: bytes) -> bool:
    """Checks if statement is an end-of-line comment.

    Double-dash comment style requires the second dash to be
    followed by at least one whitespace (Z) or control character (C) such
    as a space, tab, newline, and so on.

    Hash comment simply requires start from `#` and nothing else.

    Args:
        stmt: MySQL statement.

    Returns:
        Whether or not the statement is an end-of-line comment.

    References:
        [1]: https://dev.mysql.com/doc/refman/en/comments.html
    """
    is_double_dash_comment = (
        len(stmt) >= 3
        and stmt.startswith(b"--")
        and unicodedata.category(chr(stmt[2]))[0] in {"Z", "C"}
    )
    is_hash_comment = len(stmt) >= 2 and stmt.startswith(b"#")

    return is_double_dash_comment or is_hash_comment


def parse_multi_statement_query(multi_stmt: bytes) -> Deque[bytes]:
    """Parses a multi-statement query/operation.

    Parsing consists of removing empty (which includes just whitespaces and/or control
    characters) statements and EOL (end-of-line) comments.

    However, there's a caveat, by rule, the last EOL comment found in the stream isn't
    removed if and only if it's the last statement.

    Why? EOL comments do not generate results, however, when the last statement is an
    EOL comment the server returns an empty result. So, in other to match statements
    and results correctly we need to keep the last EOL comment statement.

    Args:
        multi_stmt: Query representing multi-statement operations separated by semicolons.

    Returns:
        A list of statements that aren't empty and don't contain leading
        ASCII whitespaces. Also, they aren't EOL comments except
        perhaps for the last one.
    """
    executed_list: Deque[bytes] = deque(RE_SQL_SPLIT_STMTS.split(multi_stmt))
    stmt, num_stms = b"", len(executed_list)
    while num_stms > 0:
        num_stms -= 1
        stmt_next = executed_list.popleft().lstrip()
        if stmt_next:
            stmt = stmt_next
            if not is_eol_comment(stmt):
                executed_list.append(stmt)

    if is_eol_comment(stmt):
        executed_list.append(stmt)

    return executed_list


class _ParamSubstitutor:
    """
    Substitutes parameters into SQL statement.
    """

    def __init__(self, params: Sequence[bytes]) -> None:
        self.params: Sequence[bytes] = params
        self.index: int = 0

    def __call__(self, matchobj: re.Match) -> bytes:
        index = self.index
        self.index += 1
        try:
            return bytes(self.params[index])
        except IndexError:
            raise ProgrammingError(
                "Not enough parameters for the SQL statement"
            ) from None

    @property
    def remaining(self) -> int:
        """Returns number of parameters remaining to be substituted"""
        return len(self.params) - self.index


def _bytestr_format_dict(bytestr: bytes, value_dict: Dict[bytes, bytes]) -> bytes:
    """
    >>> _bytestr_format_dict(b'%(a)s', {b'a': b'foobar'})
    b'foobar
    >>> _bytestr_format_dict(b'%%(a)s', {b'a': b'foobar'})
    b'%%(a)s'
    >>> _bytestr_format_dict(b'%%%(a)s', {b'a': b'foobar'})
    b'%%foobar'
    >>> _bytestr_format_dict(b'%(x)s %(y)s',
    ...                      {b'x': b'x=%(y)s', b'y': b'y=%(x)s'})
    b'x=%(y)s y=%(x)s'
    """

    def replace(matchobj: re.Match) -> bytes:
        """Replace pattern."""
        value: Optional[bytes] = None
        groups = matchobj.groupdict()
        if groups["conversion_type"] == b"%":
            value = b"%"
        if groups["conversion_type"] == b"s":
            key = groups["mapping_key"]
            value = value_dict[key]
        if value is None:
            raise ValueError(
                f"Unsupported conversion_type: {groups['conversion_type']}"
            )
        return value

    stmt = RE_PY_MAPPING_PARAM.sub(replace, bytestr)
    return stmt


class MySQLCursor(MySQLCursorAbstract):
    """Default cursor for interacting with MySQL

    This cursor will execute statements and handle the result. It will
    not automatically fetch all rows.

    MySQLCursor should be inherited whenever other functionallity is
    required. An example would to change the fetch* member functions
    to return dictionaries instead of lists of values.

    Implements the Python Database API Specification v2.0 (PEP-249)
    """

    def __init__(self, connection: Optional[MySQLConnection] = None) -> None:
        """Initialize"""
        super().__init__(connection)
        self._connection: MySQLConnection = cast("MySQLConnection", self._connection)

    def __iter__(self) -> Iterator[RowType]:
        """
        Iteration over the result set which calls self.fetchone()
        and returns the next row.
        """
        return iter(self.fetchone, None)

    def _reset_result(self) -> None:
        """Reset the cursor to default"""
        self._rowcount: int = -1
        self._nextrow = (None, None)
        self._stored_results: List[MySQLCursor] = []
        self._warnings: Optional[List[WarningType]] = None
        self._warning_count: int = 0
        self._description: Optional[List[DescriptionType]] = None
        self._executed: Optional[bytes] = None
        self._executed_list: List[bytes] = []
        self.reset()

    def _have_unread_result(self) -> bool:
        """Check whether there is an unread result"""
        try:
            return self._connection.unread_result
        except AttributeError:
            return False

    def _check_executed(self) -> None:
        """Check if the statement has been executed.

        Raises an error if the statement has not been executed.
        """
        if self._executed is None:
            raise InterfaceError(ERR_NO_RESULT_TO_FETCH)

    def __next__(self) -> RowType:
        """
        Used for iterating over the result set. Calles self.fetchone()
        to get the next row.
        """
        try:
            row = self.fetchone()
        except InterfaceError:
            raise StopIteration from None
        if not row:
            raise StopIteration
        return row

    def close(self) -> bool:
        """Close the cursor

        Returns True when successful, otherwise False.
        """
        if self._connection is None:
            return False

        self._connection.handle_unread_result()
        self._reset_result()
        self._connection = None

        return True

    def _process_params_dict(
        self, params: ParamsDictType
    ) -> Dict[bytes, Union[bytes, Decimal]]:
        """Process query parameters given as dictionary"""
        res: Dict[bytes, Any] = {}
        try:
            sql_mode = self._connection.sql_mode
            to_mysql = self._connection.converter.to_mysql
            escape = self._connection.converter.escape
            quote = self._connection.converter.quote
            for key, value in params.items():
                conv = value
                conv = to_mysql(conv)
                conv = escape(conv, sql_mode)
                if not isinstance(value, Decimal):
                    conv = quote(conv)
                res[key.encode()] = conv
        except Exception as err:
            raise ProgrammingError(
                f"Failed processing pyformat-parameters; {err}"
            ) from err
        return res

    def _process_params(
        self, params: ParamsSequenceType
    ) -> Tuple[Union[bytes, Decimal], ...]:
        """Process query parameters."""
        res = params[:]
        try:
            sql_mode = self._connection.sql_mode
            to_mysql = self._connection.converter.to_mysql
            escape = self._connection.converter.escape
            quote = self._connection.converter.quote
            res = [to_mysql(value) for value in res]
            res = [escape(value, sql_mode) for value in res]
            res = [
                quote(value) if not isinstance(params[i], Decimal) else value
                for i, value in enumerate(res)
            ]
        except Exception as err:
            raise ProgrammingError(
                f"Failed processing format-parameters; {err}"
            ) from err
        return tuple(res)

    def _handle_noresultset(self, res: ResultType) -> None:
        """Handles result of execute() when there is no result set"""
        try:
            self._rowcount = res["affected_rows"]
            self._last_insert_id = res["insert_id"]
            self._warning_count = res["warning_count"]
        except (KeyError, TypeError) as err:
            raise ProgrammingError(f"Failed handling non-resultset; {err}") from None

        self._handle_warnings()

    def _handle_resultset(self) -> None:
        """Handles result set

        This method handles the result set and is called after reading
        and storing column information in _handle_result(). For non-buffering
        cursors, this method is usually doing nothing.
        """

    def _handle_result(self, result: ResultType) -> None:
        """
        Handle the result after a command was send. The result can be either
        an OK-packet or a dictionary containing column/eof information.

        Raises InterfaceError when result is not a dict() or result is
        invalid.
        """
        if not isinstance(result, dict):
            raise InterfaceError("Result was not a dict()")

        if "columns" in result:
            # Weak test, must be column/eof information
            self._description = result["columns"]
            self._connection.unread_result = True
            self._handle_resultset()
        elif "affected_rows" in result:
            # Weak test, must be an OK-packet
            self._connection.unread_result = False
            self._handle_noresultset(result)
        else:
            raise InterfaceError("Invalid result")

    def _execute_iter(
        self, query_iter: Generator[ResultType, None, None]
    ) -> Generator[MySQLCursor, None, None]:
        """Generator returns MySQLCursor objects for multiple statements

        This method is only used when multiple statements are executed
        by the `cursor.execute(multi_stmt_query, multi=True)` method.

        It matches the given `query_iter` (result of `MySQLConnection.cmd_query_iter()`)
        and the list of statements that were executed.

        How does this method work? To properly map each statement (stmt) to a result,
        the following facts must be considered:

        1. Read operations such as `SELECT` produce a non-empty result
            (calling `next(query_iter)` gets a result that includes at least one column).
        2. Write operatios such as `INSERT` produce an empty result
            (calling `next(query_iter)` gets a result with no columns - aka empty).
        3. End-of-line (EOL) comments do not produce a result, unless is the last stmt
            in which case produces an empty result.
        4. Calling procedures such as `CALL my_proc` produce a sequence `(1)*0` which
            means it may produce zero or more non-empty results followed by just one
            empty result. In other words, a callproc stmt always terminates with an
            empty result. E.g., `my_proc` includes an update + select + select + update,
            then the result sequence will be `110` - note how the write ops results get
            annulated, just the read ops results are produced. Other examples:
                * insert + insert -> 0
                * select + select + insert + select -> 1110
                * select -> 10
            Observe how 0 indicates the end of the result sequence. This property is
            vital to know what result corresponds to what callproc stmt.

        In this regard, the implementation is composed of:
        1. Parsing: the multi-statement is broken down into single statements, and then
            for each of these, leading white spaces are removed (including
            jumping line, vertical line, tab, etc.). Also, EOL comments are removed from
            the stream, except when the comment is the last statement of the
            multi-statement string.
        2. Mapping: the facts described above as used as "game rules" to properly match
        statements and results. In case, if we run out of statements before running out
        of results we use a sentinel named "stmt_overflow!" to indicate that the mapping
        went wrong.

        Acronyms
            1: a non-empty result
            2: an empty result
        """
        executed_list = parse_multi_statement_query(multi_stmt=self._executed)
        self._executed = None
        stmt = executed_list.popleft() if executed_list else b"stmt_overflow!"
        for result in query_iter:
            self._reset_result()
            self._handle_result(result)

            if is_eol_comment(stmt):
                continue

            self._executed = stmt.rstrip()
            yield self

            if not stmt.upper().startswith(b"CALL") or "columns" not in result:
                stmt = executed_list.popleft() if executed_list else b"stmt_overflow!"

    def execute(
        self,
        operation: StrOrBytes,
        params: Optional[ParamsSequenceOrDictType] = None,
        multi: bool = False,
    ) -> Optional[Generator[MySQLCursor, None, None]]:
        """Executes the given operation

        Executes the given operation substituting any markers with
        the given parameters.

        For example, getting all rows where id is 5:
          cursor.execute("SELECT * FROM t1 WHERE id = %s", (5,))

        The multi argument should be set to True when executing multiple
        statements in one operation. If not set and multiple results are
        found, an InterfaceError will be raised.

        If warnings where generated, and connection.get_warnings is True, then
        self._warnings will be a list containing these warnings.

        Returns an iterator when multi is True, otherwise None.
        """
        if not operation:
            return None

        try:
            if not self._connection:
                raise ProgrammingError
        except (ProgrammingError, ReferenceError) as err:
            raise ProgrammingError("Cursor is not connected") from err

        self._connection.handle_unread_result()

        self._reset_result()
        stmt: StrOrBytes = ""

        try:
            if not isinstance(operation, (bytes, bytearray)):
                stmt = operation.encode(self._connection.python_charset)
            else:
                stmt = operation
        except (UnicodeDecodeError, UnicodeEncodeError) as err:
            raise ProgrammingError(str(err)) from err

        if params:
            if isinstance(params, dict):
                stmt = _bytestr_format_dict(stmt, self._process_params_dict(params))
            elif isinstance(params, (list, tuple)):
                psub = _ParamSubstitutor(self._process_params(params))
                stmt = RE_PY_PARAM.sub(psub, stmt)
                if psub.remaining != 0:
                    raise ProgrammingError(
                        "Not all parameters were used in the SQL statement"
                    )
            else:
                raise ProgrammingError(
                    f"Could not process parameters: {type(params).__name__}({params}),"
                    " it must be of type list, tuple or dict"
                )

        self._executed = stmt
        if multi:
            self._executed_list = []
            return self._execute_iter(self._connection.cmd_query_iter(stmt))

        try:
            self._handle_result(self._connection.cmd_query(stmt))
        except InterfaceError as err:
            if self._connection.have_next_result:
                raise InterfaceError(
                    "Use multi=True when executing multiple statements"
                ) from err
            raise
        return None

    def _batch_insert(
        self, operation: str, seq_params: Sequence[ParamsSequenceOrDictType]
    ) -> Optional[bytes]:
        """Implements multi row insert"""

        def remove_comments(match: re.Match) -> str:
            """Remove comments from INSERT statements.

            This function is used while removing comments from INSERT
            statements. If the matched string is a comment not enclosed
            by quotes, it returns an empty string, else the string itself.
            """
            if match.group(1):
                return ""
            return match.group(2)

        tmp = re.sub(
            RE_SQL_ON_DUPLICATE,
            "",
            re.sub(RE_SQL_COMMENT, remove_comments, operation),
        )

        matches = re.search(RE_SQL_INSERT_VALUES, tmp)
        if not matches:
            raise InterfaceError(
                "Failed rewriting statement for multi-row INSERT. Check SQL syntax"
            )
        fmt = matches.group(1).encode(self._connection.python_charset)
        values = []

        try:
            stmt = operation.encode(self._connection.python_charset)
            for params in seq_params:
                tmp = fmt
                if isinstance(params, dict):
                    tmp = _bytestr_format_dict(tmp, self._process_params_dict(params))
                else:
                    psub = _ParamSubstitutor(self._process_params(params))
                    tmp = RE_PY_PARAM.sub(psub, tmp)
                    if psub.remaining != 0:
                        raise ProgrammingError(
                            "Not all parameters were used in the SQL statement"
                        )
                values.append(tmp)
            if fmt in stmt:
                stmt = stmt.replace(fmt, b",".join(values), 1)
                self._executed = stmt
                return stmt
            return None
        except (UnicodeDecodeError, UnicodeEncodeError) as err:
            raise ProgrammingError(str(err)) from err
        except Error:
            raise
        except Exception as err:
            raise InterfaceError(f"Failed executing the operation; {err}") from None

    def executemany(
        self, operation: str, seq_params: Sequence[ParamsSequenceOrDictType]
    ) -> Optional[Generator[MySQLCursor, None, None]]:
        """Execute the given operation multiple times

        The executemany() method will execute the operation iterating
        over the list of parameters in seq_params.

        Example: Inserting 3 new employees and their phone number

        data = [
            ('Jane','555-001'),
            ('Joe', '555-001'),
            ('John', '555-003')
            ]
        stmt = "INSERT INTO employees (name, phone) VALUES ('%s','%s)"
        cursor.executemany(stmt, data)

        INSERT statements are optimized by batching the data, that is
        using the MySQL multiple rows syntax.

        Results are discarded. If they are needed, consider looping over
        data using the execute() method.
        """
        if not operation or not seq_params:
            return None
        self._connection.handle_unread_result()

        try:
            _ = iter(seq_params)
        except TypeError as err:
            raise ProgrammingError("Parameters for query must be an Iterable") from err

        # Optimize INSERTs by batching them
        if re.match(RE_SQL_INSERT_STMT, operation):
            if not seq_params:
                self._rowcount = 0
                return None
            stmt = self._batch_insert(operation, seq_params)
            if stmt is not None:
                self._executed = stmt
                return self.execute(stmt)

        rowcnt = 0
        try:
            for params in seq_params:
                self.execute(operation, params)
                if self.with_rows and self._have_unread_result():
                    self.fetchall()
                rowcnt += self._rowcount
        except (ValueError, TypeError) as err:
            raise InterfaceError(f"Failed executing the operation; {err}") from None
        self._rowcount = rowcnt
        return None

    def stored_results(self) -> Iterator[MySQLCursor]:
        """Returns an iterator for stored results

        This method returns an iterator over results which are stored when
        callproc() is called. The iterator will provide MySQLCursorBuffered
        instances.

        Returns a iterator.
        """
        return iter(self._stored_results)

    def callproc(
        self,
        procname: str,
        args: Sequence = (),
    ) -> Optional[Union[Dict[str, RowItemType], RowType]]:
        """Calls a stored procedure with the given arguments

        The arguments will be set during this session, meaning
        they will be called like  _<procname>__arg<nr> where
        <nr> is an enumeration (+1) of the arguments.

        Coding Example:
          1) Defining the Stored Routine in MySQL:
          CREATE PROCEDURE multiply(IN pFac1 INT, IN pFac2 INT, OUT pProd INT)
          BEGIN
            SET pProd := pFac1 * pFac2;
          END

          2) Executing in Python:
          args = (5, 5, 0)  # 0 is to hold pprod
          cursor.callproc('multiply', args)
          print(cursor.fetchone())

        For OUT and INOUT parameters the user should provide the
        type of the parameter as well. The argument should be a
        tuple with first item as the value of the parameter to pass
        and second argument the type of the argument.

        In the above example, one can call callproc method like:
          args = (5, 5, (0, 'INT'))
          cursor.callproc('multiply', args)

        The type of the argument given in the tuple will be used by
        the MySQL CAST function to convert the values in the corresponding
        MySQL type (See CAST in MySQL Reference for more information)

        Does not return a value, but a result set will be
        available when the CALL-statement execute successfully.
        Raises exceptions when something is wrong.
        """
        if not procname or not isinstance(procname, str):
            raise ValueError("procname must be a string")

        if not isinstance(args, (tuple, list)):
            raise ValueError("args must be a sequence")

        argfmt = "@_{name}_arg{index}"
        self._stored_results = []

        results = []
        try:
            argnames = []
            argtypes = []

            # MySQL itself does support calling procedures with their full
            # name <database>.<procedure_name>. It's necessary to split
            # by '.' and grab the procedure name from procname.
            procname_abs = procname.split(".")[-1]
            if args:
                argvalues = []
                for idx, arg in enumerate(args):
                    argname = argfmt.format(name=procname_abs, index=idx + 1)
                    argnames.append(argname)
                    if isinstance(arg, tuple):
                        argtypes.append(f" CAST({argname} AS {arg[1]})")
                        argvalues.append(arg[0])
                    else:
                        argtypes.append(argname)
                        argvalues.append(arg)

                placeholders = ",".join(f"{arg}=%s" for arg in argnames)
                self.execute(f"SET {placeholders}", argvalues)

            call = f"CALL {procname}({','.join(argnames)})"

            # We disable consuming results temporary to make sure we
            # getting all results
            can_consume_results = self._connection.can_consume_results
            for result in self._connection.cmd_query_iter(call):
                self._connection.can_consume_results = False
                if isinstance(self, (MySQLCursorDict, MySQLCursorBufferedDict)):
                    cursor_class = MySQLCursorBufferedDict
                elif isinstance(
                    self,
                    (MySQLCursorNamedTuple, MySQLCursorBufferedNamedTuple),
                ):
                    cursor_class = MySQLCursorBufferedNamedTuple
                elif self._raw:
                    cursor_class = MySQLCursorBufferedRaw
                else:
                    cursor_class = MySQLCursorBuffered
                # pylint: disable=protected-access
                cur = cursor_class(self._connection.get_self())
                cur._executed = f"(a result of {call})"
                cur._handle_result(result)
                # pylint: enable=protected-access
                if cur.warnings is not None:
                    self._warnings = cur.warnings
                if "columns" in result:
                    results.append(cur)
            self._connection.can_consume_results = can_consume_results

            if argnames:
                # Create names aliases to be compatible with namedtuples
                args = [
                    f"{name} AS {alias}"
                    for name, alias in zip(
                        argtypes, [arg.lstrip("@_") for arg in argnames]
                    )
                ]
                select = f"SELECT {','.join(args)}"
                self.execute(select)
                self._stored_results = results
                return self.fetchone()

            self._stored_results = results
            return tuple()

        except Error:
            raise
        except Exception as err:
            raise InterfaceError(f"Failed calling stored routine; {err}") from None

    def getlastrowid(self) -> Optional[int]:
        """Returns the value generated for an AUTO_INCREMENT column

        Returns the value generated for an AUTO_INCREMENT column by
        the previous INSERT or UPDATE statement.

        Returns a long value or None.
        """
        return self._last_insert_id

    def _fetch_warnings(self) -> Optional[List[WarningType]]:
        """
        Fetch warnings doing a SHOW WARNINGS. Can be called after getting
        the result.

        Returns a result set or None when there were no warnings.
        """
        res = []
        try:
            cur = self._connection.cursor(raw=False)
            cur.execute("SHOW WARNINGS")
            res = cur.fetchall()
            cur.close()
        except Exception as err:
            raise InterfaceError(f"Failed getting warnings; {err}") from None

        if res:
            return res  # type: ignore[return-value]

        return None

    def _handle_warnings(self) -> None:
        """Handle possible warnings after all results are consumed.

        Raises:
            Error: Also raises exceptions if raise_on_warnings is set.
        """
        if self._connection.get_warnings and self._warning_count:
            self._warnings = self._fetch_warnings()

        if not self._warnings:
            return

        err = get_mysql_exception(
            self._warnings[0][1],
            self._warnings[0][2],
            warning=not self._connection.raise_on_warnings,
        )

        if self._connection.raise_on_warnings:
            raise err

        warnings.warn(err, stacklevel=4)

    def _handle_eof(self, eof: EofPacketType) -> None:
        """Handle EOF packet"""
        self._connection.unread_result = False
        self._nextrow = (None, None)
        self._warning_count = eof["warning_count"]
        self._handle_warnings()

    def _fetch_row(self, raw: bool = False) -> Optional[RowType]:
        """Returns the next row in the result set

        Returns a tuple or None.
        """
        if not self._have_unread_result():
            return None
        row = None

        if self._nextrow == (None, None):
            (row, eof) = self._connection.get_row(
                binary=self._binary, columns=self.description, raw=raw
            )
        else:
            (row, eof) = self._nextrow

        if row:
            self._nextrow = self._connection.get_row(
                binary=self._binary, columns=self.description, raw=raw
            )
            eof = self._nextrow[1]
            if eof is not None:
                self._handle_eof(eof)
            if self._rowcount == -1:
                self._rowcount = 1
            else:
                self._rowcount += 1
        if eof:
            self._handle_eof(eof)

        return row

    def fetchone(self) -> Optional[RowType]:
        """Return next row of a query result set.

        Returns:
            tuple or None: A row from query result set.
        """
        self._check_executed()
        return self._fetch_row()

    def fetchmany(self, size: Optional[int] = None) -> List[RowType]:
        """Return the next set of rows of a query result set.

        When no more rows are available, it returns an empty list.
        The number of rows returned can be specified using the size argument,
        which defaults to one.

        Returns:
            list: The next set of rows of a query result set.
        """
        self._check_executed()
        res = []
        cnt = size or self.arraysize
        while cnt > 0 and self._have_unread_result():
            cnt -= 1
            row = self.fetchone()
            if row:
                res.append(row)
        return res

    def fetchall(self) -> List[RowType]:
        """Return all rows of a query result set.

        Returns:
            list: A list of tuples with all rows of a query result set.
        """
        self._check_executed()
        if not self._have_unread_result():
            return []

        (rows, eof) = self._connection.get_rows()
        if self._nextrow[0]:
            rows.insert(0, self._nextrow[0])

        self._handle_eof(eof)
        rowcount = len(rows)
        if rowcount >= 0 and self._rowcount == -1:
            self._rowcount = 0
        self._rowcount += rowcount
        return rows

    @property
    def column_names(self) -> Tuple[str, ...]:
        """Returns column names

        This property returns the columns names as a tuple.

        Returns a tuple.
        """
        if not self.description:
            return tuple()
        return tuple(d[0] for d in self.description)

    @property
    def statement(self) -> Optional[str]:
        """Returns the executed statement

        This property returns the executed statement. When multiple
        statements were executed, the current statement in the iterator
        will be returned.
        """
        if self._executed is None:
            return None
        try:
            return self._executed.strip().decode("utf-8")
        except (AttributeError, UnicodeDecodeError):
            return self._executed.strip()  # type: ignore[return-value]

    @property
    def with_rows(self) -> bool:
        """Returns whether the cursor could have rows returned

        This property returns True when column descriptions are available
        and possibly also rows, which will need to be fetched.

        Returns True or False.
        """
        if not self.description:
            return False
        return True

    def __str__(self) -> str:
        fmt = "{class_name}: {stmt}"
        if self._executed:
            try:
                executed = self._executed.decode("utf-8")
            except AttributeError:
                executed = self._executed
            if len(executed) > 40:
                executed = executed[:40] + ".."
        else:
            executed = "(Nothing executed yet)"
        return fmt.format(class_name=self.__class__.__name__, stmt=executed)


class MySQLCursorBuffered(MySQLCursor):
    """Cursor which fetches rows within execute()"""

    def __init__(self, connection: Optional[MySQLConnection] = None) -> None:
        super().__init__(connection)
        self._rows: Optional[List[RowType]] = None
        self._next_row: int = 0

    def _handle_resultset(self) -> None:
        (self._rows, eof) = self._connection.get_rows()
        self._rowcount = len(self._rows)
        self._handle_eof(eof)
        self._next_row = 0
        try:
            self._connection.unread_result = False
        except AttributeError:
            pass

    def reset(self, free: bool = True) -> None:
        self._rows = None

    def _fetch_row(self, raw: bool = False) -> Optional[RowType]:
        row = None
        try:
            row = self._rows[self._next_row]
        except (IndexError, TypeError):
            return None
        self._next_row += 1
        return row

    def fetchone(self) -> Optional[RowType]:
        """Return next row of a query result set.

        Returns:
            tuple or None: A row from query result set.
        """
        self._check_executed()
        return self._fetch_row()

    def fetchall(self) -> List[RowType]:
        """Return all rows of a query result set.

        Returns:
            list: A list of tuples with all rows of a query result set.
        """
        if self._executed is None or self._rows is None:
            raise InterfaceError(ERR_NO_RESULT_TO_FETCH)
        res = []
        res = self._rows[self._next_row :]
        self._next_row = len(self._rows)
        return res

    def fetchmany(self, size: Optional[int] = None) -> List[RowType]:
        """Return the next set of rows of a query result set.

        When no more rows are available, it returns an empty list.
        The number of rows returned can be specified using the size argument,
        which defaults to one.

        Returns:
            list: The next set of rows of a query result set.
        """
        self._check_executed()
        res = []
        cnt = size or self.arraysize
        while cnt > 0:
            cnt -= 1
            row = self.fetchone()
            if row:
                res.append(row)

        return res

    @property
    def with_rows(self) -> bool:
        return self._rows is not None


class MySQLCursorRaw(MySQLCursor):
    """
    Skips conversion from MySQL datatypes to Python types when fetching rows.
    """

    def __init__(self, connection: Optional[MySQLConnection] = None) -> None:
        super().__init__(connection)
        self._raw: bool = True

    def fetchone(self) -> Optional[RowType]:
        """Return next row of a query result set.

        Returns:
            tuple or None: A row from query result set.
        """
        self._check_executed()
        return self._fetch_row(raw=self._raw)

    def fetchall(self) -> List[RowType]:
        """Return all rows of a query result set.

        Returns:
            list: A list of tuples with all rows of a query result set.
        """
        self._check_executed()
        if not self._have_unread_result():
            return []
        (rows, eof) = self._connection.get_rows(raw=self._raw)
        if self._nextrow[0]:
            rows.insert(0, self._nextrow[0])
        self._handle_eof(eof)
        rowcount = len(rows)
        if rowcount >= 0 and self._rowcount == -1:
            self._rowcount = 0
        self._rowcount += rowcount
        return rows


class MySQLCursorBufferedRaw(MySQLCursorBuffered):
    """
    Cursor which skips conversion from MySQL datatypes to Python types when
    fetching rows and fetches rows within execute().
    """

    def __init__(self, connection: Optional[MySQLConnection] = None) -> None:
        super().__init__(connection)
        self._raw: bool = True

    def _handle_resultset(self) -> None:
        (self._rows, eof) = self._connection.get_rows(raw=self._raw)
        self._rowcount = len(self._rows)
        self._handle_eof(eof)
        self._next_row = 0
        try:
            self._connection.unread_result = False
        except AttributeError:
            pass

    def fetchone(self) -> Optional[RowType]:
        """Return next row of a query result set.

        Returns:
            tuple or None: A row from query result set.
        """
        self._check_executed()
        return self._fetch_row()

    def fetchall(self) -> List[RowType]:
        """Return all rows of a query result set.

        Returns:
            list: A list of tuples with all rows of a query result set.
        """
        self._check_executed()
        return list(self._rows[self._next_row :])

    @property
    def with_rows(self) -> bool:
        return self._rows is not None


class MySQLCursorPrepared(MySQLCursor):
    """Cursor using MySQL Prepared Statements"""

    def __init__(self, connection: Optional[MySQLConnection] = None):
        super().__init__(connection)
        self._rows: Optional[List[RowType]] = None
        self._next_row: int = 0
        self._prepared: Optional[Dict[str, Union[int, List[DescriptionType]]]] = None
        self._binary: bool = True
        self._have_result: Optional[bool] = None
        self._last_row_sent: bool = False
        self._cursor_exists: bool = False

    def reset(self, free: bool = True) -> None:
        if self._prepared:
            try:
                self._connection.cmd_stmt_close(self._prepared["statement_id"])
            except Error:
                # We tried to deallocate, but it's OK when we fail.
                pass
            self._prepared = None
        self._last_row_sent = False
        self._cursor_exists = False

    def _handle_noresultset(self, res: ResultType) -> None:
        self._handle_server_status(res.get("status_flag", res.get("server_status", 0)))
        super()._handle_noresultset(res)

    def _handle_server_status(self, flags: int) -> None:
        """Check for SERVER_STATUS_CURSOR_EXISTS and
        SERVER_STATUS_LAST_ROW_SENT flags set by the server.
        """
        self._cursor_exists = flags & ServerFlag.STATUS_CURSOR_EXISTS != 0
        self._last_row_sent = flags & ServerFlag.STATUS_LAST_ROW_SENT != 0

    def _handle_eof(self, eof: EofPacketType) -> None:
        self._handle_server_status(eof.get("status_flag", eof.get("server_status", 0)))
        super()._handle_eof(eof)

    def callproc(self, procname: Any, args: Any = ()) -> NoReturn:
        """Calls a stored procedue

        Not supported with MySQLCursorPrepared.
        """
        raise NotSupportedError()

    def close(self) -> None:
        """Close the cursor

        This method will try to deallocate the prepared statement and close
        the cursor.
        """
        self.reset()
        super().close()

    def _row_to_python(self, rowdata: Any, desc: Any = None) -> Any:
        """Convert row data from MySQL to Python types

        The conversion is done while reading binary data in the
        protocol module.
        """

    def _handle_result(self, result: ResultType) -> None:
        """Handle result after execution"""
        if isinstance(result, dict):
            self._connection.unread_result = False
            self._have_result = False
            self._handle_noresultset(result)
        else:
            self._description = result[1]
            self._connection.unread_result = True
            self._have_result = True

            if "status_flag" in result[2]:  # type: ignore[operator]
                self._handle_server_status(result[2]["status_flag"])
            elif "server_status" in result[2]:  # type: ignore[operator]
                self._handle_server_status(result[2]["server_status"])

    def execute(
        self,
        operation: StrOrBytes,
        params: Optional[ParamsSequenceOrDictType] = None,
        multi: bool = False,
    ) -> None:  # multi is unused
        """Prepare and execute a MySQL Prepared Statement

        This method will prepare the given operation and execute it using
        the optionally given parameters.

        If the cursor instance already had a prepared statement, it is
        first closed.

        Note: argument "multi" is unused.
        """
        charset = self._connection.charset
        if charset == "utf8mb4":
            charset = "utf8"

        if not isinstance(operation, str):
            try:
                operation = operation.decode(charset)
            except UnicodeDecodeError as err:
                raise ProgrammingError(str(err)) from err

        if isinstance(params, dict):
            replacement_keys = re.findall(RE_SQL_PYTHON_CAPTURE_PARAM_NAME, operation)
            try:
                # Replace params dict with params tuple in correct order.
                params = tuple(params[key] for key in replacement_keys)
            except KeyError as err:
                raise ProgrammingError(
                    "Not all placeholders were found in the parameters dict"
                ) from err
            # Convert %(name)s to ? before sending it to MySQL
            operation = re.sub(RE_SQL_PYTHON_REPLACE_PARAM, "?", operation)

        if operation is not self._executed:
            if self._prepared:
                self._connection.cmd_stmt_close(self._prepared["statement_id"])
            self._executed = operation

            try:
                operation = operation.encode(charset)
            except UnicodeEncodeError as err:
                raise ProgrammingError(str(err)) from err

            if b"%s" in operation:
                # Convert %s to ? before sending it to MySQL
                operation = re.sub(RE_SQL_FIND_PARAM, b"?", operation)

            try:
                self._prepared = self._connection.cmd_stmt_prepare(operation)
            except Error:
                self._executed = None
                raise

        self._connection.cmd_stmt_reset(self._prepared["statement_id"])

        if self._prepared["parameters"] and not params:
            return
        if params:
            if not isinstance(params, (tuple, list)):
                raise ProgrammingError(
                    errno=1210,
                    msg=f"Incorrect type of argument: {type(params).__name__}({params})"
                    ", it must be of type tuple or list the argument given to "
                    "the prepared statement",
                )
            if len(self._prepared["parameters"]) != len(params):
                raise ProgrammingError(
                    errno=1210,
                    msg="Incorrect number of arguments executing prepared statement",
                )

        if params is None:
            params = ()
        res = self._connection.cmd_stmt_execute(
            self._prepared["statement_id"],
            data=params,
            parameters=self._prepared["parameters"],
        )
        self._handle_result(res)

    def executemany(
        self,
        operation: str,
        seq_params: Sequence[ParamsSequenceType],
    ) -> None:
        """Prepare and execute a MySQL Prepared Statement many times

        This method will prepare the given operation and execute with each
        tuple found the list seq_params.

        If the cursor instance already had a prepared statement, it is
        first closed.

        executemany() simply calls execute().
        """
        rowcnt = 0
        try:
            for params in seq_params:
                self.execute(operation, params)
                if self.with_rows and self._have_unread_result():
                    self.fetchall()
                rowcnt += self._rowcount
        except (ValueError, TypeError) as err:
            raise InterfaceError(f"Failed executing the operation; {err}") from None
        self._rowcount = rowcnt

    def fetchone(self) -> Optional[RowType]:
        """Return next row of a query result set.

        Returns:
            tuple or None: A row from query result set.
        """
        self._check_executed()
        if self._cursor_exists:
            self._connection.cmd_stmt_fetch(self._prepared["statement_id"])
        return self._fetch_row() or None

    def fetchmany(self, size: Optional[int] = None) -> List[RowType]:
        """Return the next set of rows of a query result set.

        When no more rows are available, it returns an empty list.
        The number of rows returned can be specified using the size argument,
        which defaults to one.

        Returns:
            list: The next set of rows of a query result set.
        """
        self._check_executed()
        res = []
        cnt = size or self.arraysize
        while cnt > 0 and self._have_unread_result():
            cnt -= 1
            row = self._fetch_row()
            if row:
                res.append(row)
        return res

    def fetchall(self) -> List[RowType]:
        """Return all rows of a query result set.

        Returns:
            list: A list of tuples with all rows of a query result set.
        """
        self._check_executed()
        rows = []
        if self._nextrow[0]:
            rows.append(self._nextrow[0])
        while self._have_unread_result():
            if self._cursor_exists:
                self._connection.cmd_stmt_fetch(
                    self._prepared["statement_id"], MAX_RESULTS
                )
            (tmp, eof) = self._connection.get_rows(
                binary=self._binary, columns=self.description
            )
            rows.extend(tmp)
            self._handle_eof(eof)
        self._rowcount = len(rows)
        return rows


class MySQLCursorDict(MySQLCursor):
    """
    Cursor fetching rows as dictionaries.

    The fetch methods of this class will return dictionaries instead of tuples.
    Each row is a dictionary that looks like:
        row = {
            "col1": value1,
            "col2": value2
        }
    """

    def _row_to_python(
        self,
        rowdata: RowType,
        desc: Optional[List[DescriptionType]] = None,  # pylint: disable=unused-argument
    ) -> Optional[Dict[str, RowItemType]]:
        """Convert a MySQL text result row to Python types

        Returns a dictionary.
        """
        return dict(zip(self.column_names, rowdata)) if rowdata else None

    def fetchone(self) -> Optional[Dict[str, RowItemType]]:
        """Return next row of a query result set.

        Returns:
            dict or None: A dict from query result set.
        """
        return self._row_to_python(super().fetchone(), self.description)

    def fetchall(self) -> List[Optional[Dict[str, RowItemType]]]:
        """Return all rows of a query result set.

        Returns:
            list: A list of dictionaries with all rows of a query
                  result set where column names are used as keys.
        """
        return [
            self._row_to_python(row, self.description)
            for row in super().fetchall()
            if row
        ]


class MySQLCursorNamedTuple(MySQLCursor):
    """
    Cursor fetching rows as named tuple.

    The fetch methods of this class will return namedtuples instead of tuples.
    Each row is returned as a namedtuple and the values can be accessed as:
    row.col1, row.col2
    """

    def _row_to_python(
        self,
        rowdata: RowType,
        desc: Optional[List[DescriptionType]] = None,  # pylint: disable=unused-argument
    ) -> Optional[RowType]:
        """Convert a MySQL text result row to Python types

        Returns a named tuple.
        """
        row = rowdata

        if row:
            columns = tuple(self.column_names)
            try:
                named_tuple = NAMED_TUPLE_CACHE[columns]
            except KeyError:
                named_tuple = namedtuple("Row", columns)  # type:ignore[no-redef, misc]
                NAMED_TUPLE_CACHE[columns] = named_tuple
            return named_tuple(*row)
        return None

    def fetchone(self) -> Optional[RowType]:
        """Return next row of a query result set.

        Returns:
            tuple or None: A row from query result set.
        """
        row = super().fetchone()
        if not row:
            return None
        return (
            self._row_to_python(row, self.description)
            if hasattr(self._connection, "converter")
            else row
        )

    def fetchall(self) -> List[Optional[RowType]]:
        """Return all rows of a query result set.

        Returns:
            list: A list of tuples with all rows of a query result set.
        """
        return [
            self._row_to_python(row, self.description)
            for row in super().fetchall()
            if row
        ]


class MySQLCursorBufferedDict(MySQLCursorDict, MySQLCursorBuffered):
    """
    Buffered Cursor fetching rows as dictionaries.
    """

    def fetchone(self) -> Optional[Dict[str, RowItemType]]:
        """Return next row of a query result set.

        Returns:
            tuple or None: A row from query result set.
        """
        self._check_executed()
        row = self._fetch_row()
        if row:
            return self._row_to_python(row, self.description)
        return None

    def fetchall(self) -> List[Optional[Dict[str, RowItemType]]]:
        """Return all rows of a query result set.

        Returns:
            list: A list of tuples with all rows of a query result set.
        """
        if self._executed is None or self._rows is None:
            raise InterfaceError(ERR_NO_RESULT_TO_FETCH)
        res = []
        for row in self._rows[self._next_row :]:
            res.append(self._row_to_python(row, self.description))
        self._next_row = len(self._rows)
        return res


class MySQLCursorBufferedNamedTuple(MySQLCursorNamedTuple, MySQLCursorBuffered):
    """
    Buffered Cursor fetching rows as named tuple.
    """

    def fetchone(self) -> Optional[RowType]:
        """Return next row of a query result set.

        Returns:
            tuple or None: A row from query result set.
        """
        self._check_executed()
        row = self._fetch_row()
        if row:
            return self._row_to_python(row, self.description)
        return None

    def fetchall(self) -> List[Optional[RowType]]:
        """Return all rows of a query result set.

        Returns:
            list: A list of tuples with all rows of a query result set.
        """
        if self._executed is None or self._rows is None:
            raise InterfaceError(ERR_NO_RESULT_TO_FETCH)
        res = []
        for row in self._rows[self._next_row :]:
            res.append(self._row_to_python(row, self.description))
        self._next_row = len(self._rows)
        return res


class MySQLCursorPreparedDict(MySQLCursorDict, MySQLCursorPrepared):  # type: ignore[misc]
    """
    This class is a blend of features from MySQLCursorDict and MySQLCursorPrepared

    Multiple inheritance in python is allowed but care must be taken
    when assuming methods resolution. In the case of multiple
    inheritance, a given attribute is first searched in the current
    class if it's not found then it's searched in the parent classes.
    The parent classes are searched in a left-right fashion and each
    class is searched once.
    Based on python's attribute resolution, in this case, attributes
    are searched as follows:
    1. MySQLCursorPreparedDict (current class)
    2. MySQLCursorDict (left parent class)
    3. MySQLCursorPrepared (right parent class)
    4. MySQLCursor (base class)
    """

    def fetchmany(self, size: Optional[int] = None) -> List[Dict[str, RowItemType]]:
        """Return the next set of rows of a query result set.

        When no more rows are available, it returns an empty list.
        The number of rows returned can be specified using the size argument,
        which defaults to one.

        Returns:
            list: The next set of rows of a query result set represented
                  as a list of dictionaries where column names are used as keys.
        """
        return [
            self._row_to_python(row, self.description)
            for row in super().fetchmany(size=size)
            if row
        ]


class MySQLCursorPreparedNamedTuple(MySQLCursorNamedTuple, MySQLCursorPrepared):
    """
    This class is a blend of features from MySQLCursorNamedTuple and MySQLCursorPrepared
    """

    def fetchmany(self, size: Optional[int] = None) -> List[RowType]:
        """Return the next set of rows of a query result set.

        When no more rows are available, it returns an empty list.
        The number of rows returned can be specified using the size argument,
        which defaults to one.

        Returns:
            list: The next set of rows of a query result set represented
                  as a list of named tuples where column names are used as names.
        """
        return [
            self._row_to_python(row, self.description)
            for row in super().fetchmany(size=size)
            if row
        ]


class MySQLCursorPreparedRaw(MySQLCursorPrepared):
    """
    This class is a blend of features from MySQLCursorRaw and MySQLCursorPrepared
    """

    def __init__(self, connection: Optional[MySQLConnection] = None) -> None:
        super().__init__(connection)
        self._raw: bool = True

    def fetchone(self) -> Optional[RowType]:
        """Return next row of a query result set.

        Returns:
            tuple or None: A row from query result set.
        """
        self._check_executed()
        if self._cursor_exists:
            self._connection.cmd_stmt_fetch(self._prepared["statement_id"])
        return self._fetch_row(raw=self._raw) or None

    def fetchmany(self, size: Optional[int] = None) -> List[RowType]:
        """Return the next set of rows of a query result set.

        When no more rows are available, it returns an empty list.
        The number of rows returned can be specified using the size argument,
        which defaults to one.

        Returns:
            list: The next set of rows of a query result set.
        """
        self._check_executed()
        res = []
        cnt = size or self.arraysize
        while cnt > 0 and self._have_unread_result():
            cnt -= 1
            row = self._fetch_row(raw=self._raw)
            if row:
                res.append(row)
        return res

    def fetchall(self) -> List[RowType]:
        """Return all rows of a query result set.

        Returns:
            list: A list of tuples with all rows of a query result set.
        """
        self._check_executed()
        rows = []
        if self._nextrow[0]:
            rows.append(self._nextrow[0])
        while self._have_unread_result():
            if self._cursor_exists:
                self._connection.cmd_stmt_fetch(
                    self._prepared["statement_id"], MAX_RESULTS
                )
            (tmp, eof) = self._connection.get_rows(
                raw=self._raw, binary=self._binary, columns=self.description
            )
            rows.extend(tmp)
            self._handle_eof(eof)
        self._rowcount = len(rows)
        return rows
