import functools
import logging
import re
import typing
from collections import deque
from itertools import count, islice
from typing import TYPE_CHECKING
from warnings import warn

import redshift_connector
from redshift_connector.config import (
    ClientProtocolVersion,
    DbApiParamstyle,
    _client_encoding,
    table_type_clauses,
)
from redshift_connector.error import (
    MISSING_MODULE_ERROR_MSG,
    InterfaceError,
    ProgrammingError,
)

if TYPE_CHECKING:
    from redshift_connector.core import Connection

    try:
        import numpy  # type: ignore
        import pandas  # type: ignore
    except:
        pass

_logger: logging.Logger = logging.getLogger(__name__)


class Cursor:
    """A cursor object is returned by the :meth:`~Connection.cursor` method of
    a connection. It has the following attributes and methods:

    .. attribute:: arraysize

        This read/write attribute specifies the number of rows to fetch at a
        time with :meth:`fetchmany`.  It defaults to 1.

    .. attribute:: connection

        This read-only attribute contains a reference to the connection object
        (an instance of :class:`Connection`) on which the cursor was
        created.

        This attribute is part of a DBAPI 2.0 extension.  Accessing this
        attribute will generate the following warning: ``DB-API extension
        cursor.connection used``.

    .. attribute:: rowcount

        This read-only attribute contains the number of rows that the last
        ``execute()`` or ``executemany()`` method produced (for query
        statements like ``SELECT``) or affected (for modification statements
        like ``UPDATE``).

        The value is -1 if:

        - No ``execute()`` or ``executemany()`` method has been performed yet
          on the cursor.
        - There was no rowcount associated with the last ``execute()``.
        - At least one of the statements executed as part of an
          ``executemany()`` had no row count associated with it.
        - Using a ``SELECT`` query statement on Amazon Redshift server older than
          version 9.
        - Using a ``COPY`` query statement.

        This attribute is part of the `DBAPI 2.0 specification
        <http://www.python.org/dev/peps/pep-0249/>`_.

    .. attribute:: description

        This read-only attribute is a sequence of 7-item sequences.  Each value
        contains information describing one result column.  The 7 items
        returned for each column are (name, type_code, display_size,
        internal_size, precision, scale, null_ok).  Only the first two values
        are provided by the current implementation.

        This attribute is part of the `DBAPI 2.0 specification
        <http://www.python.org/dev/peps/pep-0249/>`_.
    """

    def __init__(self: "Cursor", connection: "Connection", paramstyle=None) -> None:
        """
        A cursor object is returned by the :meth:`~Connection.cursor` method of a connection.

        Parameters
        ----------
        connection : :class:`Connection`
            The :class:`Connection` object to associate with this :class:`Cursor`
        paramstyle : Optional[str]
            The DB-API paramstyle to use with this :class:`Cursor`
        """
        self._c: typing.Optional["Connection"] = connection
        self.arraysize: int = 1
        self.ps: typing.Optional[typing.Dict[str, typing.Any]] = None
        self._row_count: int = -1
        self._redshift_row_count: int = -1
        self._cached_rows: deque = deque()
        if paramstyle is None:
            self.paramstyle: str = redshift_connector.paramstyle
        else:
            self.paramstyle = paramstyle

        _logger.debug("Cursor.paramstyle=%s", self.paramstyle)

    def __enter__(self: "Cursor") -> "Cursor":
        return self

    def __exit__(self: "Cursor", exc_type, exc_value, traceback) -> None:
        self.close()

    @property
    def connection(self: "Cursor") -> typing.Optional["Connection"]:
        warn("DB-API extension cursor.connection used", stacklevel=3)
        return self._c

    @property
    def rowcount(self: "Cursor") -> int:
        """
        This read-only attribute specifies the number of rows that the last .execute*() produced
        (for DQL statements like SELECT) or affected (for DML statements like UPDATE or INSERT).

        The attribute is -1 in case no .execute*() has been performed on the cursor or the rowcount of the last
        operation is cannot be determined by the interface.
        """
        return self._row_count

    @property
    def redshift_rowcount(self: "Cursor") -> int:
        """
        Native to ``redshift_connector``, this read-only attribute specifies the number of rows that the last .execute*() produced.

        For DQL statements (like SELECT) the number of rows is derived by ``redshift_connector`` rather than
        provided by the server. For DML statements (like UPDATE or INSERT) this value is provided by the server.

        This property's behavior is subject to change inline with modifications made to query execution.
        Use ``rowcount`` as an alternative to this property.
        """
        return self._redshift_row_count

    @typing.no_type_check
    @functools.lru_cache()
    def truncated_row_desc(self: "Cursor"):
        _data: typing.List[
            typing.Optional[typing.Union[typing.Tuple[typing.Callable, int], typing.Tuple[typing.Callable]]]
        ] = []

        for cidx in range(len(self.ps["row_desc"])):
            if self._c._client_protocol_version == ClientProtocolVersion.BINARY and self.ps["row_desc"][cidx][
                "type_oid"
            ] in (1700,):
                scale: int
                if self.ps["row_desc"][cidx]["type_modifier"] != -1:
                    scale = (self.ps["row_desc"][cidx]["type_modifier"] - 4) & 0xFFFF
                else:
                    scale = -4 & 0xFFFF
                _data.append((self.ps["input_funcs"][cidx], scale))
            else:
                _data.append((self.ps["input_funcs"][cidx],))

        return _data

    description = property(lambda self: self._getDescription())

    def _getDescription(self: "Cursor") -> typing.Optional[typing.List[typing.Optional[typing.Tuple]]]:
        if self.ps is None:
            return None
        row_desc: typing.List[typing.Dict[str, typing.Union[bytes, str, int, typing.Callable]]] = self.ps["row_desc"]
        if len(row_desc) == 0:
            return None
        columns: typing.List[typing.Optional[typing.Tuple]] = []
        for col in row_desc:
            try:
                col_name: typing.Union[str, bytes] = typing.cast(bytes, col["label"]).decode(_client_encoding)
            except UnicodeError:
                warn("failed to decode column name: {}, reverting to bytes".format(col["label"]))  # type: ignore
                col_name = typing.cast(bytes, col["label"])
            columns.append((col_name, col["type_oid"], None, None, None, None, None))
        return columns

    ##
    # Executes a database operation.  Parameters may be provided as a sequence
    # or mapping and will be bound to variables in the operation.
    # <p>
    # Stability: Part of the DBAPI 2.0 specification.
    def execute(self: "Cursor", operation, args=None, stream=None, merge_socket_read=False) -> "Cursor":
        """Executes a database operation.  Parameters may be provided as a
        sequence, or as a mapping, depending upon the value of
        :data:`paramstyle`.

        This method is part of the `DBAPI 2.0 specification
        <http://www.python.org/dev/peps/pep-0249/>`_.

        :param operation: str
            The SQL statement to execute.

        :param args: typing.Union[typing.Mapping, typing.Dict, list]
            If :data:`paramstyle` is ``qmark``, ``numeric``, or ``format``,
            this argument should be an array of parameters to bind into the
            statement.  If :data:`paramstyle` is ``named``, the argument should
            be a dict mapping of parameters.  If the :data:`paramstyle` is
            ``pyformat``, the argument value may be either an array or a
            mapping.

        :param stream: This is a extension for use with the Amazon Redshift
            `COPY
            <https://docs.aws.amazon.com/redshift/latest/dg/r_COPY.html>`_
            command. For a COPY FROM the parameter must be a readable file-like
            object, and for COPY TO it must be writable.

            .. versionadded:: 1.9.11

        Returns
        -------
        The Cursor object used for executing the specified database operation: :class:`Cursor`

        """
        if self._c is None:
            raise InterfaceError("Cursor closed")
        if self._c._sock is None:
            raise InterfaceError("connection is closed")

        try:
            self.stream = stream

            # a miniaturized version of the row description is cached to speed up
            # processing data from server. It needs to be cleared with each statement
            # execution.
            self.truncated_row_desc.cache_clear()

            # For Redshift, we need to begin transaction and then to process query
            # In the end we can use commit or rollback to end the transaction
            if not self._c.in_transaction and not self._c.autocommit:
                self._c.execute(self, "begin transaction", None)
            self._c.merge_socket_read = merge_socket_read
            self._c.execute(self, operation, args)
        except Exception as e:
            try:
                _logger.debug("Cursor's connection._usock state: %s", str(self._c._usock.__dict__))
                _logger.debug("Cursor's connection._sock is closed: %s", str(self._c._sock.closed))
            except:
                pass
            raise e
        return self

    def executemany(self: "Cursor", operation, param_sets) -> "Cursor":
        """Prepare a database operation, and then execute it against all
        parameter sequences or mappings provided.

        This method is part of the `DBAPI 2.0 specification
        <http://www.python.org/dev/peps/pep-0249/>`_.

        :param operation: str
            The SQL statement to execute
        :param parameter_sets:
            A sequence of parameters to execute the statement with. The values
            in the sequence should be sequences or mappings of parameters, the
            same as the args argument of the :meth:`execute` method.

        Returns
        -------
        The Cursor object used for executing the specified database operation: :class:`Cursor`
        """
        rowcounts: typing.List[int] = []
        redshift_rowcounts: typing.List[int] = []
        for parameters in param_sets:
            self.execute(operation, parameters)
            rowcounts.append(self._row_count)
            redshift_rowcounts.append(self._redshift_row_count)

        self._row_count = -1 if -1 in rowcounts else sum(rowcounts)
        self._redshift_row_count = -1 if -1 in redshift_rowcounts else sum(rowcounts)
        return self

    def insert_data_bulk(
        self: "Cursor",
        filename: str,
        table_name: str,
        parameter_indices: typing.List[int],
        column_names: typing.List[str],
        delimiter: str,
        batch_size: int = 1,
    ) -> "Cursor":
        """runs a single bulk insert statement into the database.
        This method is native to redshift_connector.
         :param filename: str
             The name of the file to read from.
         :param table_name: str
             The name of the table to insert to.
         :param column_names:list
             The name of the columns in the table to insert to.
         :param parameter_indices:list
             The indexes of the columns in the file to insert to.
         :param delimiter: str
             The delimiter to use when reading the file.
        :param batch_size: int
            The number of rows to insert per insert statement. Minimum allowed value is 1.
         Returns
         -------
         The Cursor object used for executing the specified database operation: :class:`Cursor`
        """
        if batch_size < 1:
            raise InterfaceError("batch_size must be greater than 1")
        if not self.__is_valid_table(table_name):
            raise InterfaceError("Invalid table name passed to insert_data_bulk: {}".format(table_name))
        if not self.__has_valid_columns(table_name, column_names):
            raise InterfaceError("Invalid column names passed to insert_data_bulk: {}".format(table_name))
        orig_paramstyle = self.paramstyle
        import csv

        if len(column_names) != len(parameter_indices):
            raise InterfaceError("Column names and parameter indexes must be the same length")
        base_stmt = f"INSERT INTO  {table_name} ("
        base_stmt += ", ".join(column_names)
        base_stmt += ") VALUES "
        sql_param_list_template = "(" + ", ".join(["%s"] * len(parameter_indices)) + ")"
        try:
            with open(filename) as csv_file:
                reader = csv.reader(csv_file, delimiter=delimiter)
                next(reader)
                values_list: typing.List[str] = []
                row_count = 0
                for row in reader:
                    if row_count == batch_size:
                        sql_param_lists = [sql_param_list_template] * row_count
                        insert_stmt = base_stmt + ", ".join(sql_param_lists) + ";"
                        self.execute(insert_stmt, values_list)
                        row_count = 0
                        values_list.clear()

                    for column_index in parameter_indices:
                        values_list.append(row[column_index])

                    row_count += 1

                if row_count:
                    sql_param_lists = [sql_param_list_template] * row_count
                    insert_stmt = base_stmt + ", ".join(sql_param_lists) + ";"
                    self.execute(insert_stmt, values_list)

        except Exception as e:
            raise e
        finally:
            # reset paramstyle to it's original value
            self.paramstyle = orig_paramstyle

        return self

    def __has_valid_columns(self: "Cursor", table: str, columns: typing.List[str]) -> bool:
        split_table_name: typing.List[str] = table.split(".")
        q: str = "select 1 from pg_catalog.svv_all_columns where table_name = ? and column_name = ?"
        if len(split_table_name) == 2:
            q += " and schema_name = ?"
            param_list = [[split_table_name[1], c, split_table_name[0]] for c in columns]
        else:
            param_list = [[split_table_name[0], c] for c in columns]
        temp = self.paramstyle
        self.paramstyle = DbApiParamstyle.QMARK.value
        try:
            for params in param_list:
                self.execute(q, params)
                res = self.fetchone()
                if res is None:
                    raise InterfaceError(
                        "Invalid column name. No results were returned when performing column name validity check. Query: {} Parameters: {}".format(
                            q, params
                        )
                    )
                if typing.cast(typing.List[int], res)[0] != 1:
                    raise InterfaceError("Invalid column name: {} specified for table: {}".format(params[1], table))
        except:
            raise
        finally:
            # reset paramstyle to it's original value
            self.paramstyle = temp

        return True

    def callproc(self, procname, parameters=None):
        args = [] if parameters is None else parameters
        operation = "CALL " + self.__sanitize_str(procname) + "(" + ", ".join(["%s" for _ in args]) + ")"
        from redshift_connector.core import convert_paramstyle

        try:
            statement, make_args = convert_paramstyle(DbApiParamstyle.FORMAT.value, operation)
            vals = make_args(args)
            self.execute(statement, vals)

        except AttributeError as e:
            if self._c is None:
                raise InterfaceError("Cursor closed")
            elif self._c._sock is None:
                raise InterfaceError("connection is closed")
            else:
                raise e

    def fetchone(self: "Cursor") -> typing.Optional[typing.List]:
        """Fetch the next row of a query result set.

        This method is part of the `DBAPI 2.0 specification
        <http://www.python.org/dev/peps/pep-0249/>`_.

        Returns
        -------
        A row as a sequence of field values, or ``None`` if no more rows are available:typing.Optional[typing.List]
        """
        try:
            return next(self)
        except StopIteration:
            return None
        except TypeError:
            raise ProgrammingError("attempting to use unexecuted cursor")
        except AttributeError:
            raise ProgrammingError("attempting to use unexecuted cursor")

    def fetchmany(self: "Cursor", num: typing.Optional[int] = None) -> typing.Tuple:
        """Fetches the next set of rows of a query result.

        This method is part of the `DBAPI 2.0 specification
        <http://www.python.org/dev/peps/pep-0249/>`_.

        :param num:

            The number of rows to fetch when called.  If not provided, the
            :attr:`arraysize` attribute value is used instead.

        :returns:

            A sequence, each entry of which is a sequence of field values
            making up a row.  If no more rows are available, an empty sequence
            will be returned.:typing.Tuple

        """
        try:
            return tuple(islice(self, self.arraysize if num is None else num))
        except TypeError:
            raise ProgrammingError("attempting to use unexecuted cursor")

    def fetchall(self: "Cursor") -> typing.Tuple:
        """Fetches all remaining rows of a query result.

        This method is part of the `DBAPI 2.0 specification
        <http://www.python.org/dev/peps/pep-0249/>`_.

        :returns:

            A sequence, each entry of which is a sequence of field values
            making up a row.:tuple
        """
        try:
            return tuple(self)
        except TypeError:
            raise ProgrammingError("attempting to use unexecuted cursor")

    def close(self: "Cursor") -> None:
        """Closes the cursor.

        This method is part of the `DBAPI 2.0 specification
        <http://www.python.org/dev/peps/pep-0249/>`_.

        A row as a sequence of field values, or ``None`` if no more rows
            are available.

        Returns
        -------
        None:None
        """
        self._c = None

    def __iter__(self: "Cursor") -> "Cursor":
        """A cursor object is iterable to retrieve the rows from a query.

        This is a DBAPI 2.0 extension.
        """
        return self

    def setinputsizes(self: "Cursor", *sizes):
        """This method is part of the `DBAPI 2.0 specification
        <http://www.python.org/dev/peps/pep-0249/>`_, however, it is not
        implemented.
        """
        pass

    def setoutputsize(self: "Cursor", size, column=None):
        """This method is part of the `DBAPI 2.0 specification
        <http://www.python.org/dev/peps/pep-0249/>`_, however, it is not
        implemented.
        """
        pass

    def __next__(self: "Cursor") -> typing.List:
        try:
            return self._cached_rows.popleft()
        except IndexError:
            if self.ps is None:
                raise ProgrammingError("A query hasn't been issued.")
            elif len(self.ps["row_desc"]) == 0:
                raise ProgrammingError("no result set")
            else:
                raise StopIteration()

    def fetch_dataframe(self: "Cursor", num: typing.Optional[int] = None) -> "pandas.DataFrame":
        """
        Fetches a user defined number of rows of a query result as a :class:`pandas.DataFrame`.

        Parameters
        ----------
        num : Optional[int] The number of rows to retrieve. If unspecified, all rows will be retrieved

        Returns
        -------
        A `pandas.DataFrame` containing field values making up a row. A column label, derived from the row description of the table, is provided. : "pandas.Dataframe"
        """
        try:
            import pandas
        except ModuleNotFoundError:
            raise ModuleNotFoundError(MISSING_MODULE_ERROR_MSG.format(module="pandas"))

        columns: typing.Optional[typing.List[typing.Union[str, bytes]]] = None
        try:
            columns = [column[0].lower() for column in self.description]
        except:
            warn("No row description was found. pandas dataframe will be missing column labels.", stacklevel=2)

        if num:
            fetcheddata: tuple = self.fetchmany(num)
        else:
            fetcheddata = self.fetchall()

        result: typing.List = [tuple(column for column in rows) for rows in fetcheddata]

        return pandas.DataFrame(result, columns=columns)

    def __is_valid_table(self: "Cursor", table: str) -> bool:
        split_table_name: typing.List[str] = table.split(".")

        if len(split_table_name) > 2:
            return False

        q: str = "select 1 from pg_catalog.svv_all_tables where table_name = ?"

        temp = self.paramstyle
        self.paramstyle = DbApiParamstyle.QMARK.value

        try:
            if len(split_table_name) == 2:
                q += " and schema_name = ?"
                self.execute(q, (split_table_name[1], split_table_name[0]))
            else:
                self.execute(q, (split_table_name[0],))
        except:
            raise
        finally:
            # reset paramstyle to it's original value
            self.paramstyle = temp

        result = self.fetchone()

        return result[0] == 1 if result is not None else False

    def write_dataframe(self: "Cursor", df: "pandas.DataFrame", table: str) -> None:
        """
        Inserts a :class:`pandas.DataFrame` into a table within the current database.

        Parameters
        ----------
        df : :class:`pandas.DataFrame` Contains row values to insert into `table`
        table : str Name of an existing table in the current Amazon Redshift database to insert the values in `df`

        Returns
        -------
        None: None
        """
        try:
            import pandas
        except ModuleNotFoundError:
            raise ModuleNotFoundError(MISSING_MODULE_ERROR_MSG.format(module="pandas"))

        if not self.__is_valid_table(table):
            raise InterfaceError("Invalid table name passed to write_dataframe: {}".format(table))
        sanitized_table_name: str = self.__sanitize_str(table)
        arrays: list = df.values.tolist()
        placeholder: str = ", ".join(["%s"] * len(arrays[0]))
        sql: str = "insert into {table} values ({placeholder})".format(
            table=sanitized_table_name, placeholder=placeholder
        )
        cursor_paramstyle: str = self.paramstyle
        try:
            # force using FORMAT i.e. %s paramstyle for the current statement, then revert the
            # cursor to use the cursor's original paramstyle
            self.paramstyle = DbApiParamstyle.FORMAT.value
            if len(arrays) == 1:
                self.execute(sql, arrays[0])
            elif len(arrays) > 1:
                self.executemany(sql, arrays)
        except:
            raise InterfaceError(
                "An error occurred when attempting to insert the pandas.DataFrame into ${}".format(table)
            )
        finally:
            self.paramstyle = cursor_paramstyle

    def fetch_numpy_array(self: "Cursor", num: typing.Optional[int] = None) -> "numpy.ndarray":
        """
        Fetches a user defined number of rows of a query result as a :class:`numpy.ndarray`.

        Parameters
        ----------
        num : int The number of rows to retrieve from the result set.

        Returns
        -------
        A `numpy.ndarray` containing the results of a query executed::class:`numpy.ndarray`
        """
        try:
            import numpy
        except ModuleNotFoundError:
            raise ModuleNotFoundError(MISSING_MODULE_ERROR_MSG.format(module="numpy"))

        if num:
            fetched: typing.Tuple = self.fetchmany(num)
        else:
            fetched = self.fetchall()

        return numpy.array(fetched)

    def get_procedures(
        self: "Cursor",
        catalog: typing.Optional[str] = None,
        schema_pattern: typing.Optional[str] = None,
        procedure_name_pattern: typing.Optional[str] = None,
    ) -> tuple:
        sql: str = (
            "SELECT current_database() AS PROCEDURE_CAT, n.nspname AS PROCEDURE_SCHEM, p.proname AS PROCEDURE_NAME, "
            "NULL, NULL, NULL, d.description AS REMARKS, "
            " CASE p.prokind "
            " WHEN 'f' THEN 2 "
            " WHEN 'p' THEN 1 "
            " ELSE 0 "
            " END AS PROCEDURE_TYPE, "
            " p.proname || '_' || p.prooid AS SPECIFIC_NAME "
            " FROM pg_catalog.pg_namespace n, pg_catalog.pg_proc_info p "
            " LEFT JOIN pg_catalog.pg_description d ON (p.prooid=d.objoid) "
            " LEFT JOIN pg_catalog.pg_class c ON (d.classoid=c.oid AND c.relname='pg_proc') "
            " LEFT JOIN pg_catalog.pg_namespace pn ON (c.relnamespace=pn.oid AND pn.nspname='pg_catalog') "
            " WHERE p.pronamespace=n.oid "
        )
        query_args: typing.List[str] = []
        if schema_pattern is not None and schema_pattern != "":
            sql += " AND n.nspname LIKE ?"
            query_args.append(self.__sanitize_str(schema_pattern))
        else:
            sql += "and pg_function_is_visible(p.prooid)"

        if procedure_name_pattern is not None and procedure_name_pattern != "":
            sql += " AND p.proname LIKE ?"
            query_args.append(self.__sanitize_str(procedure_name_pattern))
        sql += " ORDER BY PROCEDURE_SCHEM, PROCEDURE_NAME, p.prooid::text "

        if len(query_args) > 0:
            # temporarily use qmark paramstyle
            temp = self.paramstyle
            self.paramstyle = DbApiParamstyle.QMARK.value

            try:
                self.execute(sql, tuple(query_args))
            except:
                raise
            finally:
                # reset the original value of paramstyle
                self.paramstyle = temp
        else:
            self.execute(sql)

        procedures: tuple = self.fetchall()
        return procedures

    def _get_catalog_filter_conditions(
        self: "Cursor",
        catalog: typing.Optional[str],
        api_supported_only_for_connected_database: bool,
        database_col_name: typing.Optional[str],
    ) -> str:
        if self._c is None:
            raise InterfaceError("connection is closed")

        catalog_filter: str = ""
        if catalog is not None and catalog != "":
            if self._c.is_single_database_metadata is True or api_supported_only_for_connected_database is True:
                catalog_filter += " AND current_database() = {catalog}".format(catalog=self.__escape_quotes(catalog))
            else:
                if database_col_name is None or database_col_name == "":
                    database_col_name = "database_name"
                catalog_filter += " AND {col_name} = {catalog}".format(
                    col_name=self.__sanitize_str(database_col_name), catalog=self.__escape_quotes(catalog)
                )
        return catalog_filter

    def get_schemas(
        self: "Cursor", catalog: typing.Optional[str] = None, schema_pattern: typing.Optional[str] = None
    ) -> tuple:
        if self._c is None:
            raise InterfaceError("connection is closed")

        query_args: typing.List[str] = []
        sql: str = ""

        if self._c.is_single_database_metadata is True:
            sql = (
                "SELECT nspname AS TABLE_SCHEM, NULL AS TABLE_CATALOG FROM pg_catalog.pg_namespace "
                " WHERE nspname <> 'pg_toast' AND (nspname !~ '^pg_temp_' "
                " OR nspname = (pg_catalog.current_schemas(true))[1]) AND (nspname !~ '^pg_toast_temp_' "
                " OR nspname = replace((pg_catalog.current_schemas(true))[1], 'pg_temp_', 'pg_toast_temp_')) "
            )
            sql += self._get_catalog_filter_conditions(catalog, True, None)

            if schema_pattern is not None and schema_pattern != "":
                sql += " AND nspname LIKE ?"
                query_args.append(self.__sanitize_str(schema_pattern))

            # if self._c.get_hide_unprivileged_objects():  # TODO: not implemented
            #     sql += " AND has_schema_privilege(nspname, 'USAGE, CREATE')"
            sql += " ORDER BY TABLE_SCHEM"
        else:
            sql = (
                "SELECT CAST(schema_name AS varchar(124)) AS TABLE_SCHEM, "
                " CAST(database_name AS varchar(124)) AS TABLE_CATALOG "
                " FROM PG_CATALOG.SVV_ALL_SCHEMAS "
                " WHERE TRUE "
            )
            sql += self._get_catalog_filter_conditions(catalog, False, None)

            if schema_pattern is not None and schema_pattern != "":
                sql += " AND schema_name LIKE ?"
                query_args.append(self.__sanitize_str(schema_pattern))
            sql += " ORDER BY TABLE_CATALOG, TABLE_SCHEM"

        if len(query_args) == 1:
            # temporarily use qmark paramstyle
            temp = self.paramstyle
            self.paramstyle = DbApiParamstyle.QMARK.value
            try:
                self.execute(sql, tuple(query_args))
            except:
                raise
            finally:
                self.paramstyle = temp
        else:
            self.execute(sql)

        schemas: tuple = self.fetchall()
        return schemas

    def get_primary_keys(
        self: "Cursor",
        catalog: typing.Optional[str] = None,
        schema: typing.Optional[str] = None,
        table: typing.Optional[str] = None,
    ) -> tuple:
        sql: str = (
            "SELECT "
            "current_database() AS TABLE_CAT, "
            "n.nspname AS TABLE_SCHEM,  "
            "ct.relname AS TABLE_NAME,   "
            "a.attname AS COLUMN_NAME,   "
            "a.attnum AS KEY_SEQ,   "
            "ci.relname AS PK_NAME   "
            "FROM  "
            "pg_catalog.pg_namespace n,  "
            "pg_catalog.pg_class ct,  "
            "pg_catalog.pg_class ci, "
            "pg_catalog.pg_attribute a, "
            "pg_catalog.pg_index i "
            "WHERE "
            "ct.oid=i.indrelid AND "
            "ci.oid=i.indexrelid  AND "
            "a.attrelid=ci.oid AND "
            "i.indisprimary  AND "
            "ct.relnamespace = n.oid "
        )
        query_args: typing.List[str] = []
        if schema is not None and schema != "":
            sql += " AND n.nspname = ?"
            query_args.append(self.__sanitize_str(schema))
        if table is not None and table != "":
            sql += " AND ct.relname = ?"
            query_args.append(self.__sanitize_str(table))

        sql += " ORDER BY table_name, pk_name, key_seq"

        if len(query_args) > 0:
            # temporarily use qmark paramstyle
            temp = self.paramstyle
            self.paramstyle = DbApiParamstyle.QMARK.value
            try:
                self.execute(sql, tuple(query_args))
            except:
                raise
            finally:
                self.paramstyle = temp
        else:
            self.execute(sql)
        keys: tuple = self.fetchall()
        return keys

    def get_catalogs(self: "Cursor") -> typing.Tuple:
        """
        Redshift does not support multiple catalogs from a single connection, so to reduce confusion we only return the
        current catalog.

        Returns
        -------
        A tuple containing the name of the current catalog: tuple
        """
        if self._c is None:
            raise InterfaceError("connection is closed")

        sql: str = ""
        if self._c.is_single_database_metadata is True:
            sql = "select current_database as TABLE_CAT FROM current_database()"
        else:
            # Datasharing/federation support enable, so get databases using the new view.
            sql = "SELECT CAST(database_name AS varchar(124)) AS TABLE_CAT FROM PG_CATALOG.SVV_REDSHIFT_DATABASES "
        sql += " ORDER BY TABLE_CAT"

        self.execute(sql)
        catalogs: typing.Tuple = self.fetchall()
        return catalogs

    def get_tables(
        self: "Cursor",
        catalog: typing.Optional[str] = None,
        schema_pattern: typing.Optional[str] = None,
        table_name_pattern: typing.Optional[str] = None,
        types: list = [],
    ) -> tuple:
        """
        Returns the unique public tables which are user-defined within the system.

        Parameters
        ----------
        catalog : Optional[str] The name of the catalog
        schema_pattern : Optional[str] A valid pattern for desired schemas
        table_name_pattern : Optional[str] A valid pattern for desired table names
        types : Optional[list[str]] A list of `str` containing table types. By default table types is not used as a filter.

        Returns
        -------
        A tuple containing unique public tables which are user-defined within the system: tuple
        """
        if self._c is None:
            raise InterfaceError("connection is closed")

        sql: str = ""
        sql_args: typing.Tuple[str, ...] = tuple()
        schema_pattern_type: str = self.__schema_pattern_match(schema_pattern)
        if schema_pattern_type == "LOCAL_SCHEMA_QUERY":
            sql, sql_args = self.__build_local_schema_tables_query(catalog, schema_pattern, table_name_pattern, types)
        elif schema_pattern_type == "NO_SCHEMA_UNIVERSAL_QUERY":
            if self._c.is_single_database_metadata is True:
                sql, sql_args = self.__build_universal_schema_tables_query(
                    catalog, schema_pattern, table_name_pattern, types
                )
            else:
                sql, sql_args = self.__build_universal_all_schema_tables_query(
                    catalog, schema_pattern, table_name_pattern, types
                )
        elif schema_pattern_type == "EXTERNAL_SCHEMA_QUERY":
            sql, sql_args = self.__build_external_schema_tables_query(
                catalog, schema_pattern, table_name_pattern, types
            )

        if len(sql_args) > 0:
            temp = self.paramstyle
            self.paramstyle = DbApiParamstyle.QMARK.value
            try:
                self.execute(sql, sql_args)
            except:
                raise
            finally:
                self.paramstyle = temp
        else:
            self.execute(sql)
        tables: tuple = self.fetchall()
        return tables

    def __build_local_schema_tables_query(
        self: "Cursor",
        catalog: typing.Optional[str],
        schema_pattern: typing.Optional[str],
        table_name_pattern: typing.Optional[str],
        types: list,
    ) -> typing.Tuple[str, typing.Tuple[str, ...]]:
        sql: str = (
            "SELECT CAST(current_database() AS VARCHAR(124)) AS TABLE_CAT, n.nspname AS TABLE_SCHEM, c.relname AS TABLE_NAME, "
            " CASE n.nspname ~ '^pg_' OR n.nspname = 'information_schema' "
            " WHEN true THEN CASE "
            " WHEN n.nspname = 'pg_catalog' OR n.nspname = 'information_schema' THEN CASE c.relkind "
            "  WHEN 'r' THEN 'SYSTEM TABLE' "
            "  WHEN 'v' THEN 'SYSTEM VIEW' "
            "  WHEN 'i' THEN 'SYSTEM INDEX' "
            "  ELSE NULL "
            "  END "
            " WHEN n.nspname = 'pg_toast' THEN CASE c.relkind "
            "  WHEN 'r' THEN 'SYSTEM TOAST TABLE' "
            "  WHEN 'i' THEN 'SYSTEM TOAST INDEX' "
            "  ELSE NULL "
            "  END "
            " ELSE CASE c.relkind "
            "  WHEN 'r' THEN 'TEMPORARY TABLE' "
            "  WHEN 'p' THEN 'TEMPORARY TABLE' "
            "  WHEN 'i' THEN 'TEMPORARY INDEX' "
            "  WHEN 'S' THEN 'TEMPORARY SEQUENCE' "
            "  WHEN 'v' THEN 'TEMPORARY VIEW' "
            "  ELSE NULL "
            "  END "
            " END "
            " WHEN false THEN CASE c.relkind "
            " WHEN 'r' THEN 'TABLE' "
            " WHEN 'p' THEN 'PARTITIONED TABLE' "
            " WHEN 'i' THEN 'INDEX' "
            " WHEN 'S' THEN 'SEQUENCE' "
            " WHEN 'v' THEN 'VIEW' "
            " WHEN 'c' THEN 'TYPE' "
            " WHEN 'f' THEN 'FOREIGN TABLE' "
            " WHEN 'm' THEN 'MATERIALIZED VIEW' "
            " ELSE NULL "
            " END "
            " ELSE NULL "
            " END "
            " AS TABLE_TYPE, d.description AS REMARKS, "
            " '' as TYPE_CAT, '' as TYPE_SCHEM, '' as TYPE_NAME, "
            "'' AS SELF_REFERENCING_COL_NAME, '' AS REF_GENERATION "
            " FROM pg_catalog.pg_namespace n, pg_catalog.pg_class c "
            " LEFT JOIN pg_catalog.pg_description d ON (c.oid = d.objoid AND d.objsubid = 0) "
            " LEFT JOIN pg_catalog.pg_class dc ON (d.classoid=dc.oid AND dc.relname='pg_class') "
            " LEFT JOIN pg_catalog.pg_namespace dn ON (dn.oid=dc.relnamespace AND dn.nspname='pg_catalog') "
            " WHERE c.relnamespace = n.oid "
        )
        filter_clause, filter_args = self.__get_table_filter_clause(
            catalog, schema_pattern, table_name_pattern, types, "LOCAL_SCHEMA_QUERY", True, None
        )
        orderby: str = " ORDER BY TABLE_TYPE,TABLE_SCHEM,TABLE_NAME "

        return sql + filter_clause + orderby, filter_args

    def __get_table_filter_clause(
        self: "Cursor",
        catalog: typing.Optional[str],
        schema_pattern: typing.Optional[str],
        table_name_pattern: typing.Optional[str],
        types: typing.List[str],
        schema_pattern_type: str,
        api_supported_only_for_connected_database: bool,
        database_col_name: typing.Optional[str],
    ) -> typing.Tuple[str, typing.Tuple[str, ...]]:
        filter_clause: str = ""
        use_schemas: str = "SCHEMAS"

        filter_clause += self._get_catalog_filter_conditions(
            catalog, api_supported_only_for_connected_database, database_col_name
        )
        query_args: typing.List[str] = []

        if schema_pattern is not None and schema_pattern != "":
            filter_clause += " AND TABLE_SCHEM LIKE ?"
            query_args.append(self.__sanitize_str(schema_pattern))
        if table_name_pattern is not None and table_name_pattern != "":
            filter_clause += " AND TABLE_NAME LIKE ?"
            query_args.append(self.__sanitize_str(table_name_pattern))
        if len(types) > 0:
            if schema_pattern_type == "LOCAL_SCHEMA_QUERY":
                filter_clause += " AND (false "
                orclause: str = ""
                for type in types:
                    if type not in table_type_clauses.keys():
                        raise InterfaceError(
                            "Invalid type: {} provided. types may only contain: {}".format(
                                type, table_type_clauses.keys()
                            )
                        )
                    clauses: typing.Optional[typing.Dict[str, str]] = table_type_clauses[type]
                    if clauses is not None:
                        cluase: str = clauses[use_schemas]
                        orclause += " OR ( {cluase} ) ".format(cluase=cluase)
                filter_clause += orclause + ") "

            elif schema_pattern_type == "NO_SCHEMA_UNIVERSAL_QUERY" or schema_pattern_type == "EXTERNAL_SCHEMA_QUERY":
                filter_clause += " AND TABLE_TYPE IN ( "
                length = len(types)
                for type in types:
                    if type not in table_type_clauses.keys():
                        raise InterfaceError(
                            "Invalid type: {} provided. types may only contain: {}".format(
                                type, table_type_clauses.keys()
                            )
                        )
                    filter_clause += "?"
                    query_args.append(type)
                    length -= 1
                    if length > 0:
                        filter_clause += ", "
                filter_clause += ") "

        return filter_clause, tuple(query_args)

    def __build_universal_schema_tables_query(
        self: "Cursor",
        catalog: typing.Optional[str],
        schema_pattern: typing.Optional[str],
        table_name_pattern: typing.Optional[str],
        types: list,
    ) -> typing.Tuple[str, typing.Tuple[str, ...]]:
        sql: str = (
            "SELECT * FROM (SELECT CAST(current_database() AS VARCHAR(124)) AS TABLE_CAT,"
            " table_schema AS TABLE_SCHEM,"
            " table_name AS TABLE_NAME,"
            " CAST("
            " CASE table_type"
            " WHEN 'BASE TABLE' THEN CASE"
            " WHEN table_schema = 'pg_catalog' OR table_schema = 'information_schema' THEN 'SYSTEM TABLE'"
            " WHEN table_schema = 'pg_toast' THEN 'SYSTEM TOAST TABLE'"
            " WHEN table_schema ~ '^pg_' AND table_schema != 'pg_toast' THEN 'TEMPORARY TABLE'"
            " ELSE 'TABLE'"
            " END"
            " WHEN 'VIEW' THEN CASE"
            " WHEN table_schema = 'pg_catalog' OR table_schema = 'information_schema' THEN 'SYSTEM VIEW'"
            " WHEN table_schema = 'pg_toast' THEN NULL"
            " WHEN table_schema ~ '^pg_' AND table_schema != 'pg_toast' THEN 'TEMPORARY VIEW'"
            " ELSE 'VIEW'"
            " END"
            " WHEN 'EXTERNAL TABLE' THEN 'EXTERNAL TABLE'"
            " END"
            " AS VARCHAR(124)) AS TABLE_TYPE,"
            " REMARKS,"
            " '' as TYPE_CAT,"
            " '' as TYPE_SCHEM,"
            " '' as TYPE_NAME, "
            " '' AS SELF_REFERENCING_COL_NAME,"
            " '' AS REF_GENERATION "
            " FROM svv_tables)"
            " WHERE true "
        )
        filter_clause, filter_args = self.__get_table_filter_clause(
            catalog, schema_pattern, table_name_pattern, types, "NO_SCHEMA_UNIVERSAL_QUERY", True, None
        )
        orderby: str = " ORDER BY TABLE_TYPE,TABLE_SCHEM,TABLE_NAME "
        sql += filter_clause + orderby
        return sql, filter_args

    def __build_universal_all_schema_tables_query(
        self: "Cursor",
        catalog: typing.Optional[str],
        schema_pattern: typing.Optional[str],
        table_name_pattern: typing.Optional[str],
        types: list,
    ) -> typing.Tuple[str, typing.Tuple[str, ...]]:
        sql: str = (
            "SELECT * FROM (SELECT CAST(DATABASE_NAME AS VARCHAR(124)) AS TABLE_CAT,"
            " SCHEMA_NAME AS TABLE_SCHEM,"
            " TABLE_NAME  AS TABLE_NAME,"
            " CAST("
            " CASE "
            " WHEN SCHEMA_NAME='information_schema' "
            "    AND TABLE_TYPE='TABLE' THEN 'SYSTEM TABLE' "
            " WHEN SCHEMA_NAME='information_schema' "
            "    AND TABLE_TYPE='VIEW' THEN 'SYSTEM VIEW' "
            " ELSE TABLE_TYPE "
            " END "
            " AS VARCHAR(124)) AS TABLE_TYPE,"
            " REMARKS,"
            " '' as TYPE_CAT,"
            " '' as TYPE_SCHEM,"
            " '' as TYPE_NAME, "
            " '' AS SELF_REFERENCING_COL_NAME,"
            " '' AS REF_GENERATION "
            " FROM PG_CATALOG.SVV_ALL_TABLES)"
            " WHERE true "
        )
        filter_clause, filter_args = self.__get_table_filter_clause(
            catalog, schema_pattern, table_name_pattern, types, "NO_SCHEMA_UNIVERSAL_QUERY", False, "TABLE_CAT"
        )
        orderby: str = " ORDER BY TABLE_TYPE,TABLE_SCHEM,TABLE_NAME "

        sql += filter_clause
        sql += orderby

        return sql, filter_args

    def __build_external_schema_tables_query(
        self: "Cursor",
        catalog: typing.Optional[str],
        schema_pattern: typing.Optional[str],
        table_name_pattern: typing.Optional[str],
        types: list,
    ) -> typing.Tuple[str, typing.Tuple[str, ...]]:
        sql: str = (
            "SELECT * FROM (SELECT CAST(current_database() AS VARCHAR(124)) AS TABLE_CAT,"
            " schemaname AS table_schem,"
            " tablename AS TABLE_NAME,"
            " 'EXTERNAL TABLE' AS TABLE_TYPE,"
            " NULL AS REMARKS,"
            " '' as TYPE_CAT,"
            " '' as TYPE_SCHEM,"
            " '' as TYPE_NAME, "
            " '' AS SELF_REFERENCING_COL_NAME,"
            " '' AS REF_GENERATION "
            " FROM svv_external_tables)"
            " WHERE true "
        )
        filter_clause, filter_args = self.__get_table_filter_clause(
            catalog, schema_pattern, table_name_pattern, types, "EXTERNAL_SCHEMA_QUERY", True, None
        )
        orderby: str = " ORDER BY TABLE_TYPE,TABLE_SCHEM,TABLE_NAME "
        sql += filter_clause + orderby
        return sql, filter_args

    def get_columns(
        self: "Cursor",
        catalog: typing.Optional[str] = None,
        schema_pattern: typing.Optional[str] = None,
        tablename_pattern: typing.Optional[str] = None,
        columnname_pattern: typing.Optional[str] = None,
    ) -> tuple:
        """
        Returns a list of all columns in a specific table in Amazon Redshift database.

        Parameters
        ----------
        catalog : Optional[str] The name of the catalog
        schema_pattern : Optional[str] A valid pattern for desired schemas
        table_name_pattern : Optional[str] A valid pattern for desired table names
        column_name_pattern : Optional[str] A valid pattern for desired column names

        Returns
        -------
        A tuple containing all columns in a specific table in Amazon Redshift database: tuple
        """
        if self._c is None:
            raise InterfaceError("connection is closed")

        sql: str = ""
        schema_pattern_type: str = self.__schema_pattern_match(schema_pattern)
        if schema_pattern_type == "LOCAL_SCHEMA_QUERY":
            sql = self.__build_local_schema_columns_query(
                catalog, schema_pattern, tablename_pattern, columnname_pattern
            )
        elif schema_pattern_type == "NO_SCHEMA_UNIVERSAL_QUERY":
            if self._c.is_single_database_metadata is True:
                sql = self.__build_universal_schema_columns_query(
                    catalog, schema_pattern, tablename_pattern, columnname_pattern
                )
            else:
                sql = self.__build_universal_all_schema_columns_query(
                    catalog, schema_pattern, tablename_pattern, columnname_pattern
                )
        elif schema_pattern_type == "EXTERNAL_SCHEMA_QUERY":
            sql = self.__build_external_schema_columns_query(
                catalog, schema_pattern, tablename_pattern, columnname_pattern
            )

        self.execute(sql)
        columns: tuple = self.fetchall()
        return columns

    def __build_local_schema_columns_query(
        self: "Cursor",
        catalog: typing.Optional[str],
        schema_pattern: typing.Optional[str],
        tablename_pattern: typing.Optional[str],
        columnname_pattern: typing.Optional[str],
    ) -> str:
        sql: str = (
            "SELECT * FROM ( "
            "SELECT current_database() AS TABLE_CAT, "
            "n.nspname AS TABLE_SCHEM, "
            "c.relname as TABLE_NAME , "
            "a.attname as COLUMN_NAME, "
            "CAST(case typname "
            "when 'text' THEN 12 "
            "when 'bit' THEN -7 "
            "when 'bool' THEN -7 "
            "when 'boolean' THEN -7 "
            "when 'varchar' THEN 12 "
            "when 'character varying' THEN 12 "
            "when 'char' THEN 1 "
            "when '\"char\"' THEN 1 "
            "when 'character' THEN 1 "
            "when 'nchar' THEN 12 "
            "when 'bpchar' THEN 1 "
            "when 'nvarchar' THEN 12 "
            "when 'date' THEN 91 "
            "when 'timestamp' THEN 93 "
            "when 'timestamp without time zone' THEN 93 "
            "when 'smallint' THEN 5 "
            "when 'int2' THEN 5 "
            "when 'integer' THEN 4 "
            "when 'int' THEN 4 "
            "when 'int4' THEN 4 "
            "when 'bigint' THEN -5 "
            "when 'int8' THEN -5 "
            "when 'decimal' THEN 3 "
            "when 'real' THEN 7 "
            "when 'float4' THEN 7 "
            "when 'double precision' THEN 8 "
            "when 'float8' THEN 8 "
            "when 'float' THEN 6 "
            "when 'numeric' THEN 2 "
            "when '_float4' THEN 2003 "
            "when 'timestamptz' THEN 2014 "
            "when 'timestamp with time zone' THEN 2014 "
            "when '_aclitem' THEN 2003 "
            "when '_text' THEN 2003 "
            "when 'bytea' THEN -2 "
            "when 'oid' THEN -5 "
            "when 'name' THEN 12 "
            "when '_int4' THEN 2003 "
            "when '_int2' THEN 2003 "
            "when 'ARRAY' THEN 2003 "
            "when 'geometry' THEN -4 "
            "when 'super' THEN -16 "
            "when 'varbyte' THEN -4 "
            "when 'geography' THEN -4 "
            "when 'intervaly2m' THEN 1111 "
            "when 'intervald2s' THEN 1111 "
            "else 1111 END as SMALLINT) AS DATA_TYPE, "
            "t.typname as TYPE_NAME, "
            "case typname "
            "when 'int4' THEN 10 "
            "when 'bit' THEN 1 "
            "when 'bool' THEN 1 "
            "when 'varchar' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'character varying' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'char' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'character' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'nchar' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'bpchar' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'nvarchar' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'date' THEN 13 "
            "when 'timestamp' THEN 29 "
            "when 'smallint' THEN 5 "
            "when 'int2' THEN 5 "
            "when 'integer' THEN 10 "
            "when 'int' THEN 10 "
            "when 'int4' THEN 10 "
            "when 'bigint' THEN 19 "
            "when 'int8' THEN 19 "
            "when 'decimal' then (atttypmod - 4) >> 16 "
            "when 'real' THEN 8 "
            "when 'float4' THEN 8 "
            "when 'double precision' THEN 17 "
            "when 'float8' THEN 17 "
            "when 'float' THEN 17 "
            "when 'numeric' THEN (atttypmod - 4) >> 16 "
            "when '_float4' THEN 8 "
            "when 'timestamptz' THEN 35 "
            "when 'oid' THEN 10 "
            "when '_int4' THEN 10 "
            "when '_int2' THEN 5 "
            "when 'geometry' THEN NULL "
            "when 'super' THEN NULL "
            "when 'varbyte' THEN NULL "
            "when 'geography' THEN NULL "
            "when 'intervaly2m' THEN 32 "
            "when 'intervald2s' THEN 64 "
            "else 2147483647 end as COLUMN_SIZE , "
            "null as BUFFER_LENGTH , "
            "case typname "
            "when 'float4' then 8 "
            "when 'float8' then 17 "
            "when 'numeric' then (atttypmod - 4) & 65535 "
            "when 'timestamp' then 6 "
            "when 'geometry' then NULL "
            "when 'super' then NULL "
            "when 'varbyte' then NULL "
            "when 'geography' then NULL "
            "when 'intervaly2m' then 32 "
            "when 'intervald2s' then 64 "
            "else 0 end as DECIMAL_DIGITS, "
            "10 AS NUM_PREC_RADIX , "
            "case a.attnotnull OR (t.typtype = 'd' AND t.typnotnull) "
            "when 'false' then 1 "
            "when NULL then 2 "
            "else 0 end AS NULLABLE , "
            "dsc.description as REMARKS , "
            "pg_catalog.pg_get_expr(def.adbin, def.adrelid) AS COLUMN_DEF, "
            "CAST(case typname "
            "when 'text' THEN 12 "
            "when 'bit' THEN -7 "
            "when 'bool' THEN -7 "
            "when 'boolean' THEN -7 "
            "when 'varchar' THEN 12 "
            "when 'character varying' THEN 12 "
            "when '\"char\"' THEN 1 "
            "when 'char' THEN 1 "
            "when 'character' THEN 1 "
            "when 'nchar' THEN 1 "
            "when 'bpchar' THEN 1 "
            "when 'nvarchar' THEN 12 "
            "when 'date' THEN 91 "
            "when 'timestamp' THEN 93 "
            "when 'timestamp without time zone' THEN 93 "
            "when 'smallint' THEN 5 "
            "when 'int2' THEN 5 "
            "when 'integer' THEN 4 "
            "when 'int' THEN 4 "
            "when 'int4' THEN 4 "
            "when 'bigint' THEN -5 "
            "when 'int8' THEN -5 "
            "when 'decimal' THEN 3 "
            "when 'real' THEN 7 "
            "when 'float4' THEN 7 "
            "when 'double precision' THEN 8 "
            "when 'float8' THEN 8 "
            "when 'float' THEN 6 "
            "when 'numeric' THEN 2 "
            "when '_float4' THEN 2003 "
            "when 'timestamptz' THEN 2014 "
            "when 'timestamp with time zone' THEN 2014 "
            "when '_aclitem' THEN 2003 "
            "when '_text' THEN 2003 "
            "when 'bytea' THEN -2 "
            "when 'oid' THEN -5 "
            "when 'name' THEN 12 "
            "when '_int4' THEN 2003 "
            "when '_int2' THEN 2003 "
            "when 'ARRAY' THEN 2003 "
            "when 'geometry' THEN -4 "
            "when 'super' THEN -16 "
            "when 'varbyte' THEN -4 "
            "when 'geography' THEN -4 "
            "when 'intervaly2m' THEN 1111 "
            "when 'intervald2s' THEN 1111 "
            "else 1111 END as SMALLINT) AS SQL_DATA_TYPE, "
            "CAST(NULL AS SMALLINT) as SQL_DATETIME_SUB , "
            "case typname "
            "when 'int4' THEN 10 "
            "when 'bit' THEN 1 "
            "when 'bool' THEN 1 "
            "when 'varchar' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'character varying' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'char' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'character' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'nchar' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'bpchar' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'nvarchar' THEN CASE atttypmod WHEN -1 THEN 0 ELSE atttypmod -4 END "
            "when 'date' THEN 13 "
            "when 'timestamp' THEN 29 "
            "when 'smallint' THEN 5 "
            "when 'int2' THEN 5 "
            "when 'integer' THEN 10 "
            "when 'int' THEN 10 "
            "when 'int4' THEN 10 "
            "when 'bigint' THEN 19 "
            "when 'int8' THEN 19 "
            "when 'decimal' then ((atttypmod - 4) >> 16) & 65535 "
            "when 'real' THEN 8 "
            "when 'float4' THEN 8 "
            "when 'double precision' THEN 17 "
            "when 'float8' THEN 17 "
            "when 'float' THEN 17 "
            "when 'numeric' THEN ((atttypmod - 4) >> 16) & 65535 "
            "when '_float4' THEN 8 "
            "when 'timestamptz' THEN 35 "
            "when 'oid' THEN 10 "
            "when '_int4' THEN 10 "
            "when '_int2' THEN 5 "
            "when 'geometry' THEN NULL "
            "when 'super' THEN NULL "
            "when 'varbyte' THEN NULL "
            "when 'geography' THEN NULL "
            "when 'intervaly2m' THEN 32 "
            "when 'intervald2s' THEN 64 "
            "else 2147483647 end as CHAR_OCTET_LENGTH , "
            "a.attnum AS ORDINAL_POSITION, "
            "case a.attnotnull OR (t.typtype = 'd' AND t.typnotnull) "
            "when 'false' then 'YES' "
            "when NULL then '' "
            "else 'NO' end AS IS_NULLABLE, "
            "null as SCOPE_CATALOG , "
            "null as SCOPE_SCHEMA , "
            "null as SCOPE_TABLE, "
            "t.typbasetype AS SOURCE_DATA_TYPE , "
            "CASE WHEN left(pg_catalog.pg_get_expr(def.adbin, def.adrelid), 16) = 'default_identity' THEN 'YES' "
            "ELSE 'NO' END AS IS_AUTOINCREMENT, "
            "IS_AUTOINCREMENT AS IS_GENERATEDCOLUMN "
            "FROM pg_catalog.pg_namespace n  JOIN pg_catalog.pg_class c ON (c.relnamespace = n.oid) "
            "JOIN pg_catalog.pg_attribute a ON (a.attrelid=c.oid) "
            "JOIN pg_catalog.pg_type t ON (a.atttypid = t.oid) "
            "LEFT JOIN pg_catalog.pg_attrdef def ON (a.attrelid=def.adrelid AND a.attnum = def.adnum) "
            "LEFT JOIN pg_catalog.pg_description dsc ON (c.oid=dsc.objoid AND a.attnum = dsc.objsubid) "
            "LEFT JOIN pg_catalog.pg_class dc ON (dc.oid=dsc.classoid AND dc.relname='pg_class') "
            "LEFT JOIN pg_catalog.pg_namespace dn ON (dc.relnamespace=dn.oid AND dn.nspname='pg_catalog') "
            "WHERE a.attnum > 0 AND NOT a.attisdropped    "
        )

        sql += self._get_catalog_filter_conditions(catalog, True, None)

        if schema_pattern is not None and schema_pattern != "":
            sql += " AND n.nspname LIKE {schema}".format(schema=self.__escape_quotes(schema_pattern))
        if tablename_pattern is not None and tablename_pattern != "":
            sql += " AND c.relname LIKE {table}".format(table=self.__escape_quotes(tablename_pattern))
        if columnname_pattern is not None and columnname_pattern != "":
            sql += " AND attname LIKE {column}".format(column=self.__escape_quotes(columnname_pattern))

        sql += " ORDER BY TABLE_SCHEM,c.relname,attnum ) "

        # This part uses redshift method PG_GET_LATE_BINDING_VIEW_COLS() to
        # get the column list for late binding view.
        sql += (
            " UNION ALL "
            "SELECT current_database()::VARCHAR(128) AS TABLE_CAT, "
            "schemaname::varchar(128) AS table_schem, "
            "tablename::varchar(128) AS table_name, "
            "columnname::varchar(128) AS column_name, "
            "CAST(CASE columntype_rep "
            "WHEN 'text' THEN 12 "
            "WHEN 'bit' THEN -7 "
            "WHEN 'bool' THEN -7 "
            "WHEN 'boolean' THEN -7 "
            "WHEN 'varchar' THEN 12 "
            "WHEN 'character varying' THEN 12 "
            "WHEN 'char' THEN 1 "
            "WHEN 'character' THEN 1 "
            "WHEN 'nchar' THEN 1 "
            "WHEN 'bpchar' THEN 1 "
            "WHEN 'nvarchar' THEN 12 "
            "WHEN '\"char\"' THEN 1 "
            "WHEN 'date' THEN 91 "
            "WHEN 'timestamp' THEN 93 "
            "WHEN 'timestamp without time zone' THEN 93 "
            "WHEN 'timestamp with time zone' THEN 2014 "
            "WHEN 'smallint' THEN 5 "
            "WHEN 'int2' THEN 5 "
            "WHEN 'integer' THEN 4 "
            "WHEN 'int' THEN 4 "
            "WHEN 'int4' THEN 4 "
            "WHEN 'bigint' THEN -5 "
            "WHEN 'int8' THEN -5 "
            "WHEN 'decimal' THEN 3 "
            "WHEN 'real' THEN 7 "
            "WHEN 'float4' THEN 7 "
            "WHEN 'double precision' THEN 8 "
            "WHEN 'float8' THEN 8 "
            "WHEN 'float' THEN 6 "
            "WHEN 'numeric' THEN 2 "
            "WHEN 'timestamptz' THEN 2014 "
            "WHEN 'bytea' THEN -2 "
            "WHEN 'oid' THEN -5 "
            "WHEN 'name' THEN 12 "
            "WHEN 'ARRAY' THEN 2003 "
            "WHEN 'geometry' THEN -4 "
            "WHEN 'super' THEN -16 "
            "WHEN 'varbyte' THEN -4 "
            "WHEN 'geography' THEN -4"
            "WHEN 'intervaly2m' THEN 1111 "
            "WHEN 'intervald2s' THEN 1111 "
            "ELSE 1111 END AS SMALLINT) AS DATA_TYPE, "
            "COALESCE(NULL,CASE columntype WHEN 'boolean' THEN 'bool' "
            "WHEN 'character varying' THEN 'varchar' "
            "WHEN '\"char\"' THEN 'char' "
            "WHEN 'smallint' THEN 'int2' "
            "WHEN 'integer' THEN 'int4'"
            "WHEN 'bigint' THEN 'int8' "
            "WHEN 'real' THEN 'float4' "
            "WHEN 'double precision' THEN 'float8' "
            "WHEN 'timestamp without time zone' THEN 'timestamp' "
            "WHEN 'timestamp with time zone' THEN 'timestamptz' "
            "ELSE columntype END) AS TYPE_NAME,  "
            "CASE columntype_rep "
            "WHEN 'int4' THEN 10  "
            "WHEN 'bit' THEN 1    "
            "WHEN 'bool' THEN 1"
            "WHEN 'boolean' THEN 1"
            "WHEN 'varchar' THEN regexp_substr (columntype,'[0-9]+',7)::INTEGER "
            "WHEN 'character varying' THEN regexp_substr (columntype,'[0-9]+',7)::INTEGER "
            "WHEN 'char' THEN regexp_substr (columntype,'[0-9]+',4)::INTEGER "
            "WHEN 'character' THEN regexp_substr (columntype,'[0-9]+',4)::INTEGER "
            "WHEN 'nchar' THEN regexp_substr (columntype,'[0-9]+',7)::INTEGER "
            "WHEN 'bpchar' THEN regexp_substr (columntype,'[0-9]+',7)::INTEGER "
            "WHEN 'nvarchar' THEN regexp_substr (columntype,'[0-9]+',7)::INTEGER "
            "WHEN 'date' THEN 13 "
            "WHEN 'timestamp' THEN 29 "
            "WHEN 'timestamp without time zone' THEN 29 "
            "WHEN 'smallint' THEN 5 "
            "WHEN 'int2' THEN 5 "
            "WHEN 'integer' THEN 10 "
            "WHEN 'int' THEN 10 "
            "WHEN 'int4' THEN 10 "
            "WHEN 'bigint' THEN 19 "
            "WHEN 'int8' THEN 19 "
            "WHEN 'decimal' THEN regexp_substr (columntype,'[0-9]+',7)::INTEGER "
            "WHEN 'real' THEN 8 "
            "WHEN 'float4' THEN 8 "
            "WHEN 'double precision' THEN 17 "
            "WHEN 'float8' THEN 17 "
            "WHEN 'float' THEN 17"
            "WHEN 'numeric' THEN regexp_substr (columntype,'[0-9]+',7)::INTEGER "
            "WHEN '_float4' THEN 8 "
            "WHEN 'timestamptz' THEN 35 "
            "WHEN 'timestamp with time zone' THEN 35 "
            "WHEN 'oid' THEN 10 "
            "WHEN '_int4' THEN 10 "
            "WHEN '_int2' THEN 5 "
            "WHEN 'geometry' THEN NULL "
            "WHEN 'super' THEN NULL "
            "WHEN 'varbyte' THEN NULL "
            "WHEN 'geography' THEN NULL "
            "WHEN 'intervaly2m' THEN 32 "
            "WHEN 'intervald2s' THEN 64 "
            "ELSE 2147483647 END AS COLUMN_SIZE, "
            "NULL AS BUFFER_LENGTH, "
            "CASE REGEXP_REPLACE(columntype,'[()0-9,]') "
            "WHEN 'real' THEN 8 "
            "WHEN 'float4' THEN 8 "
            "WHEN 'double precision' THEN 17 "
            "WHEN 'float8' THEN 17 "
            "WHEN 'timestamp' THEN 6 "
            "WHEN 'timestamp without time zone' THEN 6 "
            "WHEN 'geometry' THEN NULL "
            "WHEN 'super' THEN NULL "
            "WHEN 'numeric' THEN regexp_substr (columntype,'[0-9]+',charindex (',',columntype))::INTEGER "
            "WHEN 'varbyte' THEN NULL "
            "WHEN 'geography' THEN NULL "
            "WHEN 'intervaly2m' THEN 32 "
            "WHEN 'intervald2s' THEN 64 "
            "ELSE 0 END AS DECIMAL_DIGITS, 10 AS NUM_PREC_RADIX, "
            "NULL AS NULLABLE,  NULL AS REMARKS,   NULL AS COLUMN_DEF, "
            "CAST(CASE columntype_rep "
            "WHEN 'text' THEN 12 "
            "WHEN 'bit' THEN -7 "
            "WHEN 'bool' THEN -7 "
            "WHEN 'boolean' THEN -7 "
            "WHEN 'varchar' THEN 12 "
            "WHEN 'character varying' THEN 12 "
            "WHEN 'char' THEN 1 "
            "WHEN 'character' THEN 1 "
            "WHEN 'nchar' THEN 12 "
            "WHEN 'bpchar' THEN 1 "
            "WHEN 'nvarchar' THEN 12 "
            "WHEN '\"char\"' THEN 1 "
            "WHEN 'date' THEN 91 "
            "WHEN 'timestamp' THEN 93 "
            "WHEN 'timestamp without time zone' THEN 93 "
            "WHEN 'timestamp with time zone' THEN 2014 "
            "WHEN 'smallint' THEN 5 "
            "WHEN 'int2' THEN 5 "
            "WHEN 'integer' THEN 4 "
            "WHEN 'int' THEN 4 "
            "WHEN 'int4' THEN 4 "
            "WHEN 'bigint' THEN -5 "
            "WHEN 'int8' THEN -5 "
            "WHEN 'decimal' THEN 3 "
            "WHEN 'real' THEN 7 "
            "WHEN 'float4' THEN 7 "
            "WHEN 'double precision' THEN 8 "
            "WHEN 'float8' THEN 8 "
            "WHEN 'float' THEN 6 "
            "WHEN 'numeric' THEN 2 "
            "WHEN 'timestamptz' THEN 2014 "
            "WHEN 'bytea' THEN -2 "
            "WHEN 'oid' THEN -5 "
            "WHEN 'name' THEN 12 "
            "WHEN 'ARRAY' THEN 2003 "
            "WHEN 'geometry' THEN -4 "
            "WHEN 'super' THEN -16 "
            "WHEN 'varbyte' THEN -4 "
            "WHEN 'geography' THEN -4 "
            "WHEN 'intervaly2m' THEN 1111 "
            "WHEN 'intervald2s' THEN 1111 "
            "ELSE 1111 END AS SMALLINT) AS SQL_DATA_TYPE, "
            "CAST(NULL AS SMALLINT) AS SQL_DATETIME_SUB, CASE "
            "WHEN LEFT (columntype,7) = 'varchar' THEN regexp_substr (columntype,'[0-9]+',7)::INTEGER "
            "WHEN LEFT (columntype,4) = 'char' THEN regexp_substr (columntype,'[0-9]+',4)::INTEGER "
            "WHEN columntype = 'string' THEN 16383  ELSE NULL "
            "END AS CHAR_OCTET_LENGTH, columnnum AS ORDINAL_POSITION, "
            "NULL AS IS_NULLABLE,  NULL AS SCOPE_CATALOG,  NULL AS SCOPE_SCHEMA, "
            "NULL AS SCOPE_TABLE, NULL AS SOURCE_DATA_TYPE, 'NO' AS IS_AUTOINCREMENT, "
            "'NO' as IS_GENERATEDCOLUMN "
            "FROM (select lbv_cols.schemaname, "
            "lbv_cols.tablename, lbv_cols.columnname,"
            "REGEXP_REPLACE(REGEXP_REPLACE(lbv_cols.columntype,'\\\\(.*\\\\)'),'^_.+','ARRAY') as columntype_rep,"
            "columntype, "
            "lbv_cols.columnnum "
            "from pg_get_late_binding_view_cols() lbv_cols( "
            "schemaname name, tablename name, columnname name, "
            "columntype text, columnnum int)) lbv_columns  "
            " WHERE true "
        )
        if schema_pattern is not None and schema_pattern != "":
            sql += " AND schemaname LIKE {schema}".format(schema=self.__escape_quotes(schema_pattern))
        if tablename_pattern is not None and tablename_pattern != "":
            sql += " AND tablename LIKE {table}".format(table=self.__escape_quotes(tablename_pattern))
        if columnname_pattern is not None and columnname_pattern != "":
            sql += " AND columnname LIKE {column}".format(column=self.__escape_quotes(columnname_pattern))

        return sql

    def __build_universal_schema_columns_query_filters(
        self: "Cursor",
        schema_pattern: typing.Optional[str],
        tablename_pattern: typing.Optional[str],
        columnname_pattern: typing.Optional[str],
    ) -> str:
        filter_clause: str = ""

        if schema_pattern is not None and schema_pattern != "":
            filter_clause += " AND schema_name LIKE {schema}".format(schema=self.__escape_quotes(schema_pattern))
        if tablename_pattern is not None and tablename_pattern != "":
            filter_clause += " AND table_name LIKE {table}".format(table=self.__escape_quotes(tablename_pattern))
        if columnname_pattern is not None and columnname_pattern != "":
            filter_clause += " AND COLUMN_NAME LIKE {column}".format(column=self.__escape_quotes(columnname_pattern))

        return filter_clause

    def __build_universal_schema_columns_query(
        self: "Cursor",
        catalog: typing.Optional[str],
        schema_pattern: typing.Optional[str],
        tablename_pattern: typing.Optional[str],
        columnname_pattern: typing.Optional[str],
    ) -> str:
        unknown_column_size: str = "2147483647"
        sql: str = (
            "SELECT current_database()::varchar(128) AS TABLE_CAT,"
            " table_schema AS TABLE_SCHEM,"
            " table_name,"
            " COLUMN_NAME,"
            " CAST(CASE regexp_replace(data_type, '^_.+', 'ARRAY')"
            " WHEN 'text' THEN 12"
            " WHEN 'bit' THEN -7"
            " WHEN 'bool' THEN -7"
            " WHEN 'boolean' THEN -7"
            " WHEN 'varchar' THEN 12"
            " WHEN 'character varying' THEN 12"
            " WHEN 'char' THEN 1"
            " WHEN 'character' THEN 1"
            " WHEN 'nchar' THEN 1"
            " WHEN 'bpchar' THEN 1"
            " WHEN 'nvarchar' THEN 12"
            " WHEN '\"char\"' THEN 1"
            " WHEN 'date' THEN 91"
            " WHEN 'timestamp' THEN 93"
            " WHEN 'timestamp without time zone' THEN 93"
            " WHEN 'timestamp with time zone' THEN 2014"
            " WHEN 'smallint' THEN 5"
            " WHEN 'int2' THEN 5"
            " WHEN 'integer' THEN 4"
            " WHEN 'int' THEN 4"
            " WHEN 'int4' THEN 4"
            " WHEN 'bigint' THEN -5"
            " WHEN 'int8' THEN -5"
            " WHEN 'decimal' THEN 3"
            " WHEN 'real' THEN 7"
            " WHEN 'float4' THEN 7"
            " WHEN 'double precision' THEN 8"
            " WHEN 'float8' THEN 8"
            " WHEN 'float' THEN 6"
            " WHEN 'numeric' THEN 2"
            " WHEN 'timestamptz' THEN 2014"
            " WHEN 'bytea' THEN -2"
            " WHEN 'oid' THEN -5"
            " WHEN 'name' THEN 12"
            " WHEN 'ARRAY' THEN 2003"
            " WHEN 'geometry' THEN -4 "
            " WHEN 'super' THEN -16 "
            " WHEN 'varbyte' THEN -4 "
            " WHEN 'geography' THEN -4 "
            " WHEN 'intervaly2m' THEN 1111 "
            " WHEN 'intervald2s' THEN 1111 "
            " ELSE 1111 END AS SMALLINT) AS DATA_TYPE,"
            " COALESCE("
            " domain_name,"
            " CASE data_type"
            " WHEN 'boolean' THEN 'bool'"
            " WHEN 'character varying' THEN 'varchar'"
            " WHEN '\"char\"' THEN 'char'"
            " WHEN 'smallint' THEN 'int2'"
            " WHEN 'integer' THEN 'int4'"
            " WHEN 'bigint' THEN 'int8'"
            " WHEN 'real' THEN 'float4'"
            " WHEN 'double precision' THEN 'float8'"
            " WHEN 'timestamp without time zone' THEN 'timestamp'"
            " WHEN 'timestamp with time zone' THEN 'timestamptz'"
            " ELSE data_type"
            " END) AS TYPE_NAME,"
            " CASE data_type"
            " WHEN 'int4' THEN 10"
            " WHEN 'bit' THEN 1"
            " WHEN 'bool' THEN 1"
            " WHEN 'boolean' THEN 1"
            " WHEN 'varchar' THEN character_maximum_length"
            " WHEN 'character varying' THEN character_maximum_length"
            " WHEN 'char' THEN character_maximum_length"
            " WHEN 'character' THEN character_maximum_length"
            " WHEN 'nchar' THEN character_maximum_length"
            " WHEN 'bpchar' THEN character_maximum_length"
            " WHEN 'nvarchar' THEN character_maximum_length"
            " WHEN 'date' THEN 13"
            " WHEN 'timestamp' THEN 29"
            " WHEN 'timestamp without time zone' THEN 29"
            " WHEN 'smallint' THEN 5"
            " WHEN 'int2' THEN 5"
            " WHEN 'integer' THEN 10"
            " WHEN 'int' THEN 10"
            " WHEN 'int4' THEN 10"
            " WHEN 'bigint' THEN 19"
            " WHEN 'int8' THEN 19"
            " WHEN 'decimal' THEN numeric_precision"
            " WHEN 'real' THEN 8"
            " WHEN 'float4' THEN 8"
            " WHEN 'double precision' THEN 17"
            " WHEN 'float8' THEN 17"
            " WHEN 'float' THEN 17"
            " WHEN 'numeric' THEN numeric_precision"
            " WHEN '_float4' THEN 8"
            " WHEN 'timestamptz' THEN 35"
            " WHEN 'timestamp with time zone' THEN 35"
            " WHEN 'oid' THEN 10"
            " WHEN '_int4' THEN 10"
            " WHEN '_int2' THEN 5"
            " WHEN 'geometry' THEN NULL"
            " WHEN 'super' THEN NULL"
            " WHEN 'varbyte' THEN NULL"
            " WHEN 'geography' THEN NULL "
            " WHEN 'intervaly2m' THEN 32 "
            " WHEN 'intervald2s' THEN 64 "
            " ELSE {unknown_column_size}"
            " END AS COLUMN_SIZE,"
            " NULL AS BUFFER_LENGTH,"
            " CASE data_type"
            " WHEN 'real' THEN 8"
            " WHEN 'float4' THEN 8"
            " WHEN 'double precision' THEN 17"
            " WHEN 'float8' THEN 17"
            " WHEN 'numeric' THEN numeric_scale"
            " WHEN 'timestamp' THEN 6"
            " WHEN 'timestamp without time zone' THEN 6"
            " WHEN 'geometry' THEN NULL"
            " WHEN 'super' THEN NULL"
            " WHEN 'varbyte' THEN NULL"
            " WHEN 'geography' THEN NULL "
            " WHEN 'intervaly2m' THEN 32 "
            " WHEN 'intervald2s' THEN 64 "
            " ELSE 0"
            " END AS DECIMAL_DIGITS,"
            " 10 AS NUM_PREC_RADIX,"
            " CASE is_nullable WHEN 'YES' THEN 1"
            " WHEN 'NO' THEN 0"
            " ELSE 2 end AS NULLABLE,"
            " REMARKS,"
            " column_default AS COLUMN_DEF,"
            " CAST(CASE regexp_replace(data_type, '^_.+', 'ARRAY')"
            " WHEN 'text' THEN 12"
            " WHEN 'bit' THEN -7"
            " WHEN 'bool' THEN -7"
            " WHEN 'boolean' THEN -7"
            " WHEN 'varchar' THEN 12"
            " WHEN 'character varying' THEN 12"
            " WHEN 'char' THEN 1"
            " WHEN 'character' THEN 1"
            " WHEN 'nchar' THEN 1"
            " WHEN 'bpchar' THEN 1"
            " WHEN 'nvarchar' THEN 12"
            " WHEN '\"char\"' THEN 1"
            " WHEN 'date' THEN 91"
            " WHEN 'timestamp' THEN 93"
            " WHEN 'timestamp without time zone' THEN 93"
            " WHEN 'timestamp with time zone' THEN 2014"
            " WHEN 'smallint' THEN 5"
            " WHEN 'int2' THEN 5"
            " WHEN 'integer' THEN 4"
            " WHEN 'int' THEN 4"
            " WHEN 'int4' THEN 4"
            " WHEN 'bigint' THEN -5"
            " WHEN 'int8' THEN -5"
            " WHEN 'decimal' THEN 3"
            " WHEN 'real' THEN 7"
            " WHEN 'float4' THEN 7"
            " WHEN 'double precision' THEN 8"
            " WHEN 'float8' THEN 8"
            " WHEN 'float' THEN 6"
            " WHEN 'numeric' THEN 2"
            " WHEN 'timestamptz' THEN 2014"
            " WHEN 'bytea' THEN -2"
            " WHEN 'oid' THEN -5"
            " WHEN 'name' THEN 12"
            " WHEN 'ARRAY' THEN 2003"
            " WHEN 'geometry' THEN -4"
            " WHEN 'super' THEN -16"
            " WHEN 'varbyte' THEN -4"
            " WHEN 'geography' THEN -4 "
            " WHEN 'intervaly2m' THEN 1111 "
            " WHEN 'intervald2s' THEN 1111 "
            " ELSE 1111 END AS SMALLINT) AS SQL_DATA_TYPE,"
            " CAST(NULL AS SMALLINT) AS SQL_DATETIME_SUB,"
            " CASE data_type"
            " WHEN 'int4' THEN 10"
            " WHEN 'bit' THEN 1"
            " WHEN 'bool' THEN 1"
            " WHEN 'boolean' THEN 1"
            " WHEN 'varchar' THEN character_maximum_length"
            " WHEN 'character varying' THEN character_maximum_length"
            " WHEN 'char' THEN character_maximum_length"
            " WHEN 'character' THEN character_maximum_length"
            " WHEN 'nchar' THEN character_maximum_length"
            " WHEN 'bpchar' THEN character_maximum_length"
            " WHEN 'nvarchar' THEN character_maximum_length"
            " WHEN 'date' THEN 13"
            " WHEN 'timestamp' THEN 29"
            " WHEN 'timestamp without time zone' THEN 29"
            " WHEN 'smallint' THEN 5"
            " WHEN 'int2' THEN 5"
            " WHEN 'integer' THEN 10"
            " WHEN 'int' THEN 10"
            " WHEN 'int4' THEN 10"
            " WHEN 'bigint' THEN 19"
            " WHEN 'int8' THEN 19"
            " WHEN 'decimal' THEN numeric_precision"
            " WHEN 'real' THEN 8"
            " WHEN 'float4' THEN 8"
            " WHEN 'double precision' THEN 17"
            " WHEN 'float8' THEN 17"
            " WHEN 'float' THEN 17"
            " WHEN 'numeric' THEN numeric_precision"
            " WHEN '_float4' THEN 8"
            " WHEN 'timestamptz' THEN 35"
            " WHEN 'timestamp with time zone' THEN 35"
            " WHEN 'oid' THEN 10"
            " WHEN '_int4' THEN 10"
            " WHEN '_int2' THEN 5"
            " WHEN 'geometry' THEN NULL"
            " WHEN 'super' THEN NULL"
            " WHEN 'varbyte' THEN NULL"
            " WHEN 'geography' THEN NULL "
            " WHEN 'intervaly2m' THEN 32 "
            " WHEN 'intervald2s' THEN 64 "
            " ELSE {unknown_column_size}"
            " END AS CHAR_OCTET_LENGTH,"
            " ordinal_position AS ORDINAL_POSITION,"
            " is_nullable AS IS_NULLABLE,"
            " NULL AS SCOPE_CATALOG,"
            " NULL AS SCOPE_SCHEMA,"
            " NULL AS SCOPE_TABLE,"
            " CASE"
            " WHEN domain_name is not null THEN data_type"
            " END AS SOURCE_DATA_TYPE,"
            " CASE WHEN left(column_default, 10) = '\"identity\"' THEN 'YES'"
            " WHEN left(column_default, 16) = 'default_identity' THEN 'YES' "
            " ELSE 'NO' END AS IS_AUTOINCREMENT,"
            " IS_AUTOINCREMENT AS IS_GENERATEDCOLUMN"
            " FROM svv_columns"
            " WHERE true "
        ).format(unknown_column_size=unknown_column_size)

        sql += self._get_catalog_filter_conditions(catalog, True, None)
        sql += self.__build_universal_schema_columns_query_filters(
            schema_pattern, tablename_pattern, columnname_pattern
        )

        sql += " ORDER BY table_schem,table_name,ORDINAL_POSITION "
        return sql

    def __build_universal_all_schema_columns_query(
        self: "Cursor",
        catalog: typing.Optional[str],
        schema_pattern: typing.Optional[str],
        tablename_pattern: typing.Optional[str],
        columnname_pattern: typing.Optional[str],
    ) -> str:
        unknown_column_size: str = "2147483647"
        sql: str = (
            "SELECT database_name AS TABLE_CAT, "
            " schema_name AS TABLE_SCHEM, "
            " table_name, "
            " COLUMN_NAME, "
            " CAST(CASE regexp_replace(data_type, '^_.', 'ARRAY') "
            " WHEN 'text' THEN 12 "
            " WHEN 'bit' THEN -7 "
            " WHEN 'bool' THEN -7 "
            " WHEN 'boolean' THEN -7 "
            " WHEN 'varchar' THEN 12 "
            " WHEN 'character varying' THEN 12 "
            " WHEN 'char' THEN 1 "
            " WHEN 'character' THEN 1 "
            " WHEN 'nchar' THEN 1 "
            " WHEN 'bpchar' THEN 1 "
            " WHEN 'nvarchar' THEN 12 "
            " WHEN '\"char\"' THEN 1 "
            " WHEN 'date' THEN 91 "
            " WHEN 'timestamp' THEN 93 "
            " WHEN 'timestamp without time zone' THEN 93 "
            " WHEN 'timestamp with time zone' THEN 2014 "
            " WHEN 'smallint' THEN 5 "
            " WHEN 'int2' THEN 5 "
            " WHEN 'integer' THEN 4 "
            " WHEN 'int' THEN 4 "
            " WHEN 'int4' THEN 4 "
            " WHEN 'bigint' THEN -5 "
            " WHEN 'int8' THEN -5 "
            " WHEN 'decimal' THEN 3 "
            " WHEN 'real' THEN 7 "
            " WHEN 'float4' THEN 7 "
            " WHEN 'double precision' THEN 8 "
            " WHEN 'float8' THEN 8 "
            " WHEN 'float' THEN 6 "
            " WHEN 'numeric' THEN 2 "
            " WHEN 'timestamptz' THEN 2014 "
            " WHEN 'bytea' THEN -2 "
            " WHEN 'oid' THEN -5 "
            " WHEN 'name' THEN 12 "
            " WHEN 'ARRAY' THEN 2003 "
            " WHEN 'geometry' THEN -4 "
            " WHEN 'super' THEN -16 "
            " WHEN 'varbyte' THEN -4 "
            " WHEN 'geography' THEN -4 "
            " WHEN 'intervaly2m' THEN 1111 "
            " WHEN 'intervald2s' THEN 1111 "
            " ELSE 1111 END AS SMALLINT) AS DATA_TYPE, "
            " CASE data_type "
            " WHEN 'boolean' THEN 'bool' "
            " WHEN 'character varying' THEN 'varchar' "
            " WHEN '\"char\"' THEN 'char' "
            " WHEN 'smallint' THEN 'int2' "
            " WHEN 'integer' THEN 'int4' "
            " WHEN 'bigint' THEN 'int8' "
            " WHEN 'real' THEN 'float4' "
            " WHEN 'double precision' THEN 'float8' "
            " WHEN 'timestamp without time zone' THEN 'timestamp' "
            " WHEN 'timestamp with time zone' THEN 'timestamptz' "
            " ELSE data_type "
            " END AS TYPE_NAME, "
            " CASE data_type "
            " WHEN 'int4' THEN 10 "
            " WHEN 'bit' THEN 1 "
            " WHEN 'bool' THEN 1 "
            " WHEN 'boolean' THEN 1 "
            " WHEN 'varchar' THEN character_maximum_length "
            " WHEN 'character varying' THEN character_maximum_length "
            " WHEN 'char' THEN character_maximum_length "
            " WHEN 'character' THEN character_maximum_length "
            " WHEN 'nchar' THEN character_maximum_length "
            " WHEN 'bpchar' THEN character_maximum_length "
            " WHEN 'nvarchar' THEN character_maximum_length "
            " WHEN 'date' THEN 13 "
            " WHEN 'timestamp' THEN 29 "
            " WHEN 'timestamp without time zone' THEN 29 "
            " WHEN 'smallint' THEN 5 "
            " WHEN 'int2' THEN 5 "
            " WHEN 'integer' THEN 10 "
            " WHEN 'int' THEN 10 "
            " WHEN 'int4' THEN 10 "
            " WHEN 'bigint' THEN 19 "
            " WHEN 'int8' THEN 19 "
            " WHEN 'decimal' THEN numeric_precision "
            " WHEN 'real' THEN 8 "
            " WHEN 'float4' THEN 8 "
            " WHEN 'double precision' THEN 17 "
            " WHEN 'float8' THEN 17 "
            " WHEN 'float' THEN 17 "
            " WHEN 'numeric' THEN numeric_precision "
            " WHEN '_float4' THEN 8 "
            " WHEN 'timestamptz' THEN 35 "
            " WHEN 'timestamp with time zone' THEN 35 "
            " WHEN 'oid' THEN 10 "
            " WHEN '_int4' THEN 10 "
            " WHEN '_int2' THEN 5 "
            " WHEN 'geometry' THEN NULL "
            " WHEN 'super' THEN NULL "
            " WHEN 'varbyte' THEN NULL "
            " WHEN 'geography' THEN NULL "
            " WHEN 'intervaly2m' THEN 32 "
            " WHEN 'intervald2s' THEN 64 "
            " ELSE   2147483647 "
            " END AS COLUMN_SIZE, "
            " NULL AS BUFFER_LENGTH, "
            " CASE data_type "
            " WHEN 'real' THEN 8 "
            " WHEN 'float4' THEN 8 "
            " WHEN 'double precision' THEN 17 "
            " WHEN 'float8' THEN 17 "
            " WHEN 'numeric' THEN numeric_scale "
            " WHEN 'timestamp' THEN 6 "
            " WHEN 'timestamp without time zone' THEN 6 "
            " WHEN 'geometry' THEN NULL "
            " WHEN 'super' THEN NULL "
            " WHEN 'varbyte' THEN NULL "
            " WHEN 'geography' THEN NULL "
            " WHEN 'intervaly2m' THEN 32 "
            " WHEN 'intervald2s' THEN 64 "
            " ELSE 0 "
            " END AS DECIMAL_DIGITS, "
            " 10 AS NUM_PREC_RADIX, "
            " CASE is_nullable WHEN 'YES' THEN 1 "
            " WHEN 'NO' THEN 0 "
            " ELSE 2 end AS NULLABLE, "
            " REMARKS, "
            " column_default AS COLUMN_DEF, "
            " CAST(CASE regexp_replace(data_type, '^_.', 'ARRAY') "
            " WHEN 'text' THEN 12 "
            " WHEN 'bit' THEN -7 "
            " WHEN 'bool' THEN -7 "
            " WHEN 'boolean' THEN -7 "
            " WHEN 'varchar' THEN 12 "
            " WHEN 'character varying' THEN 12 "
            " WHEN 'char' THEN 1 "
            " WHEN 'character' THEN 1 "
            " WHEN 'nchar' THEN 1 "
            " WHEN 'bpchar' THEN 1 "
            " WHEN 'nvarchar' THEN 12 "
            " WHEN '\"char\"' THEN 1 "
            " WHEN 'date' THEN 91 "
            " WHEN 'timestamp' THEN 93 "
            " WHEN 'timestamp without time zone' THEN 93 "
            " WHEN 'timestamp with time zone' THEN 2014 "
            " WHEN 'smallint' THEN 5 "
            " WHEN 'int2' THEN 5 "
            " WHEN 'integer' THEN 4 "
            " WHEN 'int' THEN 4 "
            " WHEN 'int4' THEN 4 "
            " WHEN 'bigint' THEN -5 "
            " WHEN 'int8' THEN -5 "
            " WHEN 'decimal' THEN 3 "
            " WHEN 'real' THEN 7 "
            " WHEN 'float4' THEN 7 "
            " WHEN 'double precision' THEN 8 "
            " WHEN 'float8' THEN 8 "
            " WHEN 'float' THEN 6 "
            " WHEN 'numeric' THEN 2 "
            " WHEN 'timestamptz' THEN 2014 "
            " WHEN 'bytea' THEN -2 "
            " WHEN 'oid' THEN -5 "
            " WHEN 'name' THEN 12 "
            " WHEN 'ARRAY' THEN 2003 "
            " WHEN 'geometry' THEN -4 "
            " WHEN 'super' THEN -16 "
            " WHEN 'varbyte' THEN -4 "
            " WHEN 'geography' THEN -4 "
            " WHEN 'intervaly2m' THEN 1111 "
            " WHEN 'intervald2s' THEN 1111 "
            " ELSE 1111 END AS SMALLINT) AS SQL_DATA_TYPE, "
            " CAST(NULL AS SMALLINT) AS SQL_DATETIME_SUB, "
            " CASE data_type "
            " WHEN 'int4' THEN 10 "
            " WHEN 'bit' THEN 1 "
            " WHEN 'bool' THEN 1 "
            " WHEN 'boolean' THEN 1 "
            " WHEN 'varchar' THEN character_maximum_length "
            " WHEN 'character varying' THEN character_maximum_length "
            " WHEN 'char' THEN character_maximum_length "
            " WHEN 'character' THEN character_maximum_length "
            " WHEN 'nchar' THEN character_maximum_length "
            " WHEN 'bpchar' THEN character_maximum_length "
            " WHEN 'nvarchar' THEN character_maximum_length "
            " WHEN 'date' THEN 13 "
            " WHEN 'timestamp' THEN 29 "
            " WHEN 'timestamp without time zone' THEN 29 "
            " WHEN 'smallint' THEN 5 "
            " WHEN 'int2' THEN 5 "
            " WHEN 'integer' THEN 10 "
            " WHEN 'int' THEN 10 "
            " WHEN 'int4' THEN 10 "
            " WHEN 'bigint' THEN 19 "
            " WHEN 'int8' THEN 19 "
            " WHEN 'decimal' THEN numeric_precision "
            " WHEN 'real' THEN 8 "
            " WHEN 'float4' THEN 8 "
            " WHEN 'double precision' THEN 17 "
            " WHEN 'float8' THEN 17 "
            " WHEN 'float' THEN 17 "
            " WHEN 'numeric' THEN numeric_precision "
            " WHEN '_float4' THEN 8 "
            " WHEN 'timestamptz' THEN 35 "
            " WHEN 'timestamp with time zone' THEN 35 "
            " WHEN 'oid' THEN 10 "
            " WHEN '_int4' THEN 10 "
            " WHEN '_int2' THEN 5 "
            " WHEN 'geometry' THEN NULL "
            " WHEN 'super' THEN NULL "
            " WHEN 'varbyte' THEN NULL "
            " WHEN 'geography' THEN NULL "
            " WHEN 'intervaly2m' THEN 32 "
            " WHEN 'intervald2s' THEN 64 "
            " ELSE   2147483647 "
            " END AS CHAR_OCTET_LENGTH, "
            " ordinal_position AS ORDINAL_POSITION, "
            " is_nullable AS IS_NULLABLE, "
            " NULL AS SCOPE_CATALOG, "
            " NULL AS SCOPE_SCHEMA, "
            " NULL AS SCOPE_TABLE, "
            " data_type as SOURCE_DATA_TYPE, "
            " CASE WHEN left(column_default, 10) = '\"identity\"' THEN 'YES' "
            " WHEN left(column_default, 16) = 'default_identity' THEN 'YES' "
            " ELSE 'NO' END AS IS_AUTOINCREMENT, "
            " IS_AUTOINCREMENT AS IS_GENERATEDCOLUMN "
            " FROM PG_CATALOG.svv_all_columns "
            " WHERE true "
        )

        sql += self._get_catalog_filter_conditions(catalog, False, None)
        sql += self.__build_universal_schema_columns_query_filters(
            schema_pattern, tablename_pattern, columnname_pattern
        )

        sql += " ORDER BY TABLE_CAT, TABLE_SCHEM, TABLE_NAME, ORDINAL_POSITION "
        return sql

    def __build_external_schema_columns_query(
        self: "Cursor",
        catalog: typing.Optional[str],
        schema_pattern: typing.Optional[str],
        tablename_pattern: typing.Optional[str],
        columnname_pattern: typing.Optional[str],
    ) -> str:
        sql: str = (
            "SELECT current_database()::varchar(128) AS TABLE_CAT,"
            " schemaname AS TABLE_SCHEM,"
            " tablename AS TABLE_NAME,"
            " columnname AS COLUMN_NAME,"
            " CAST(CASE WHEN external_type = 'text' THEN 12"
            " WHEN external_type = 'bit' THEN -7"
            " WHEN external_type = 'bool' THEN -7"
            " WHEN external_type = 'boolean' THEN -7"
            " WHEN left(external_type, 7) = 'varchar' THEN 12"
            " WHEN left(external_type, 17) = 'character varying' THEN 12"
            " WHEN left(external_type, 4) = 'char' THEN 1"
            " WHEN left(external_type, 9) = 'character' THEN 1"
            " WHEN left(external_type, 5) = 'nchar' THEN 1"
            " WHEN left(external_type, 6) = 'bpchar' THEN 1"
            " WHEN left(external_type, 8) = 'nvarchar' THEN 12"
            " WHEN external_type = '\"char\"' THEN 1"
            " WHEN external_type = 'date' THEN 91"
            " WHEN external_type = 'timestamp' THEN 93"
            " WHEN external_type = 'timestamp without time zone' THEN 93"
            " WHEN external_type = 'timestamp with time zone' THEN 2014"
            " WHEN external_type = 'smallint' THEN 5"
            " WHEN external_type = 'int2' THEN 5"
            " WHEN external_type = '_int2' THEN 5"
            " WHEN external_type = 'integer' THEN 4"
            " WHEN external_type = 'int' THEN 4"
            " WHEN external_type = 'int4' THEN 4"
            " WHEN external_type = '_int4' THEN 4"
            " WHEN external_type = 'bigint' THEN -5"
            " WHEN external_type = 'int8' THEN -5"
            " WHEN left(external_type, 7) = 'decimal' THEN 2"
            " WHEN external_type = 'real' THEN 7"
            " WHEN external_type = 'float4' THEN 7"
            " WHEN external_type = '_float4' THEN 7"
            " WHEN external_type = 'double' THEN 8"
            " WHEN external_type = 'double precision' THEN 8"
            " WHEN external_type = 'float8' THEN 8"
            " WHEN external_type = '_float8' THEN 8"
            " WHEN external_type = 'float' THEN 6"
            " WHEN left(external_type, 7) = 'numeric' THEN 2"
            " WHEN external_type = 'timestamptz' THEN 2014"
            " WHEN external_type = 'bytea' THEN -2"
            " WHEN external_type = 'oid' THEN -5"
            " WHEN external_type = 'name' THEN 12"
            " WHEN external_type = 'ARRAY' THEN 2003"
            " WHEN external_type = 'geometry' THEN -4"
            " WHEN external_type = 'super' THEN -16"
            " WHEN external_type = 'varbyte' THEN -4"
            " WHEN external_type = 'intervaly2m' THEN 1111 "
            " WHEN external_type = 'intervald2s' THEN 1111 "
            " ELSE 1111 END AS SMALLINT) AS DATA_TYPE,"
            " CASE WHEN left(external_type, 17) = 'character varying' THEN 'varchar'"
            " WHEN left(external_type, 7) = 'varchar' THEN 'varchar'"
            " WHEN left(external_type, 4) = 'char' THEN 'char'"
            " WHEN left(external_type, 7) = 'decimal' THEN 'numeric'"
            " WHEN left(external_type, 7) = 'numeric' THEN 'numeric'"
            " WHEN external_type = 'double' THEN 'double precision'"
            " WHEN external_type = 'timestamp without time zone' THEN 'timestamp'"
            " WHEN external_type = 'timestamp with time zone' THEN 'timestamptz'"
            " ELSE external_type END AS TYPE_NAME,"
            " CASE WHEN external_type = 'int4' THEN 10"
            " WHEN external_type = 'bit' THEN 1"
            " WHEN external_type = 'bool' THEN 1"
            " WHEN external_type = 'boolean' THEN 1"
            " WHEN left(external_type, 7) = 'varchar' THEN regexp_substr(external_type, '[0-9]+', 7)::integer"
            " WHEN left(external_type, 17) = 'character varying' THEN regexp_substr(external_type, '[0-9]+', 17)::integer"
            " WHEN left(external_type, 4) = 'char' THEN regexp_substr(external_type, '[0-9]+', 4)::integer"
            " WHEN left(external_type, 9) = 'character' THEN regexp_substr(external_type, '[0-9]+', 9)::integer"
            " WHEN left(external_type, 5) = 'nchar' THEN regexp_substr(external_type, '[0-9]+', 5)::integer"
            " WHEN left(external_type, 6) = 'bpchar' THEN regexp_substr(external_type, '[0-9]+', 6)::integer"
            " WHEN left(external_type, 8) = 'nvarchar' THEN regexp_substr(external_type, '[0-9]+', 8)::integer"
            " WHEN external_type = 'date' THEN 13 WHEN external_type = 'timestamp' THEN 29"
            " WHEN external_type = 'timestamp without time zone' THEN 29"
            " WHEN external_type = 'smallint' THEN 5"
            " WHEN external_type = 'int2' THEN 5"
            " WHEN external_type = 'integer' THEN 10"
            " WHEN external_type = 'int' THEN 10"
            " WHEN external_type = 'int4' THEN 10"
            " WHEN external_type = 'bigint' THEN 19"
            " WHEN external_type = 'int8' THEN 19"
            " WHEN left(external_type, 7) = 'decimal' THEN regexp_substr(external_type, '[0-9]+', 7)::integer"
            " WHEN external_type = 'real' THEN 8"
            " WHEN external_type = 'float4' THEN 8"
            " WHEN external_type = '_float4' THEN 8"
            " WHEN external_type = 'double' THEN 17"
            " WHEN external_type = 'double precision' THEN 17"
            " WHEN external_type = 'float8' THEN 17"
            " WHEN external_type = '_float8' THEN 17"
            " WHEN external_type = 'float' THEN 17"
            " WHEN left(external_type, 7) = 'numeric' THEN regexp_substr(external_type, '[0-9]+', 7)::integer"
            " WHEN external_type = '_float4' THEN 8"
            " WHEN external_type = 'timestamptz' THEN 35"
            " WHEN external_type = 'timestamp with time zone' THEN 35"
            " WHEN external_type = 'oid' THEN 10"
            " WHEN external_type = '_int4' THEN 10"
            " WHEN external_type = '_int2' THEN 5"
            " WHEN external_type = 'geometry' THEN NULL"
            " WHEN external_type = 'super' THEN NULL"
            " WHEN external_type = 'varbyte' THEN NULL"
            " WHEN external_type = 'intervaly2m' THEN 32 "
            " WHEN external_type = 'intervald2s' THEN 64 "
            " ELSE 2147483647 END AS COLUMN_SIZE,"
            " NULL AS BUFFER_LENGTH,"
            " CASE WHEN external_type = 'real'THEN 8"
            " WHEN external_type = 'float4' THEN 8"
            " WHEN external_type = 'double' THEN 17"
            " WHEN external_type = 'double precision' THEN 17"
            " WHEN external_type = 'float8' THEN 17"
            " WHEN left(external_type, 7) = 'numeric' THEN regexp_substr(external_type, '[0-9]+', 10)::integer"
            " WHEN left(external_type, 7) = 'decimal' THEN regexp_substr(external_type, '[0-9]+', 10)::integer"
            " WHEN external_type = 'timestamp' THEN 6"
            " WHEN external_type = 'timestamp without time zone' THEN 6"
            " WHEN external_type = 'geometry' THEN NULL"
            " WHEN external_type = 'super' THEN NULL"
            " WHEN external_type = 'varbyte' THEN NULL"
            " WHEN external_type = 'intervaly2m' THEN 32 "
            " WHEN external_type = 'intervald2s' THEN 64 "
            " ELSE 0 END AS DECIMAL_DIGITS,"
            " 10 AS NUM_PREC_RADIX,"
            " NULL AS NULLABLE,"
            " NULL AS REMARKS,"
            " NULL AS COLUMN_DEF,"
            " CAST(CASE WHEN external_type = 'text' THEN 12"
            " WHEN external_type = 'bit' THEN -7"
            " WHEN external_type = 'bool' THEN -7"
            " WHEN external_type = 'boolean' THEN -7"
            " WHEN left(external_type, 7) = 'varchar' THEN 12"
            " WHEN left(external_type, 17) = 'character varying' THEN 12"
            " WHEN left(external_type, 4) = 'char' THEN 1"
            " WHEN left(external_type, 9) = 'character' THEN 1"
            " WHEN left(external_type, 5) = 'nchar' THEN 1"
            " WHEN left(external_type, 6) = 'bpchar' THEN 1"
            " WHEN left(external_type, 8) = 'nvarchar' THEN 12"
            " WHEN external_type = '\"char\"' THEN 1"
            " WHEN external_type = 'date' THEN 91"
            " WHEN external_type = 'timestamp' THEN 93"
            " WHEN external_type = 'timestamp without time zone' THEN 93"
            " WHEN external_type = 'timestamp with time zone' THEN 2014"
            " WHEN external_type = 'smallint' THEN 5"
            " WHEN external_type = 'int2' THEN 5"
            " WHEN external_type = '_int2' THEN 5"
            " WHEN external_type = 'integer' THEN 4"
            " WHEN external_type = 'int' THEN 4"
            " WHEN external_type = 'int4' THEN 4"
            " WHEN external_type = '_int4' THEN 4"
            " WHEN external_type = 'bigint' THEN -5"
            " WHEN external_type = 'int8' THEN -5"
            " WHEN left(external_type, 7) = 'decimal' THEN 3"
            " WHEN external_type = 'real' THEN 7"
            " WHEN external_type = 'float4' THEN 7"
            " WHEN external_type = '_float4' THEN 7"
            " WHEN external_type = 'double' THEN 8"
            " WHEN external_type = 'double precision' THEN 8"
            " WHEN external_type = 'float8' THEN 8"
            " WHEN external_type = '_float8' THEN 8"
            " WHEN external_type = 'float' THEN 6"
            " WHEN left(external_type, 7) = 'numeric' THEN 2"
            " WHEN external_type = 'timestamptz' THEN 2014"
            " WHEN external_type = 'bytea' THEN -2"
            " WHEN external_type = 'oid' THEN -5"
            " WHEN external_type = 'name' THEN 12"
            " WHEN external_type = 'ARRAY' THEN 2003"
            " WHEN external_type = 'geometry' THEN -4"
            " WHEN external_type = 'super' THEN -16"
            " WHEN external_type = 'varbyte' THEN -4"
            " WHEN external_type = 'intervaly2m' THEN 1111 "
            " WHEN external_type = 'intervald2s' THEN 1111 "
            " ELSE 1111 END AS SMALLINT) AS SQL_DATA_TYPE,"
            " CAST(NULL AS SMALLINT) AS SQL_DATETIME_SUB,"
            " CASE WHEN left(external_type, 7) = 'varchar' THEN regexp_substr(external_type, '[0-9]+', 7)::integer"
            " WHEN left(external_type, 17) = 'character varying' THEN regexp_substr(external_type, '[0-9]+', 17)::integer"
            " WHEN left(external_type, 4) = 'char' THEN regexp_substr(external_type, '[0-9]+', 4)::integer"
            " WHEN left(external_type, 9) = 'character' THEN regexp_substr(external_type, '[0-9]+', 9)::integer"
            " WHEN left(external_type, 5) = 'nchar' THEN regexp_substr(external_type, '[0-9]+', 5)::integer"
            " WHEN left(external_type, 6) = 'bpchar' THEN regexp_substr(external_type, '[0-9]+', 6)::integer"
            " WHEN left(external_type, 8) = 'nvarchar' THEN regexp_substr(external_type, '[0-9]+', 8)::integer"
            " WHEN external_type = 'string' THEN 16383"
            " ELSE NULL END AS CHAR_OCTET_LENGTH,"
            " columnnum AS ORDINAL_POSITION,"
            " NULL AS IS_NULLABLE,"
            " NULL AS SCOPE_CATALOG,"
            " NULL AS SCOPE_SCHEMA,"
            " NULL AS SCOPE_TABLE,"
            " NULL AS SOURCE_DATA_TYPE,"
            " 'NO' AS IS_AUTOINCREMENT,"
            " 'NO' AS IS_GENERATEDCOLUMN"
            " FROM svv_external_columns"
            " WHERE true "
        )
        sql += self._get_catalog_filter_conditions(catalog, True, None)

        if schema_pattern is not None and schema_pattern != "":
            sql += " AND schemaname LIKE {schema}".format(schema=self.__escape_quotes(schema_pattern))
        if tablename_pattern is not None and tablename_pattern != "":
            sql += " AND tablename LIKE {table}".format(table=self.__escape_quotes(tablename_pattern))
        if columnname_pattern is not None and columnname_pattern != "":
            sql += " AND columnname LIKE {column}".format(column=self.__escape_quotes(columnname_pattern))

        sql += " ORDER BY table_schem,table_name,ORDINAL_POSITION "

        return sql

    def __schema_pattern_match(self: "Cursor", schema_pattern: typing.Optional[str]) -> str:
        if self._c is None:
            raise InterfaceError("connection is closed")
        if schema_pattern is not None and schema_pattern != "":
            if self._c.is_single_database_metadata is True:
                sql: str = "select 1 from svv_external_schemas where schemaname like {schema}".format(
                    schema=self.__escape_quotes(schema_pattern)
                )
                self.execute(sql)
                schemas: tuple = self.fetchall()
                if schemas is not None and len(schemas) > 0:
                    return "EXTERNAL_SCHEMA_QUERY"
                else:
                    return "LOCAL_SCHEMA_QUERY"
            else:
                return "NO_SCHEMA_UNIVERSAL_QUERY"
        else:
            return "NO_SCHEMA_UNIVERSAL_QUERY"

    def __sanitize_str(self: "Cursor", s: str) -> str:
        return re.sub(r"[-;/'\"\n\r ]", "", s)

    def __escape_quotes(self: "Cursor", s: str) -> str:
        return "'{s}'".format(s=self.__sanitize_str(s))
