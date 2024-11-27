import re
import warnings
from functools import lru_cache
from typing import (
    TYPE_CHECKING,
    Any,
    Collection,
    Dict,
    Iterable,
    List,
    Optional,
    Set,
    Tuple,
    Type,
)

import duckdb
import sqlalchemy
from sqlalchemy import pool, select, sql, text, util
from sqlalchemy import types as sqltypes
from sqlalchemy.dialects.postgresql import UUID
from sqlalchemy.dialects.postgresql.base import (
    PGDialect,
    PGIdentifierPreparer,
    PGInspector,
    PGTypeCompiler,
)
from sqlalchemy.dialects.postgresql.psycopg2 import PGDialect_psycopg2
from sqlalchemy.engine.default import DefaultDialect
from sqlalchemy.engine.interfaces import Dialect as RootDialect
from sqlalchemy.engine.reflection import cache
from sqlalchemy.engine.url import URL
from sqlalchemy.exc import NoSuchTableError
from sqlalchemy.ext.compiler import compiles
from sqlalchemy.sql import bindparam
from sqlalchemy.sql.selectable import Select

from ._supports import has_comment_support
from .config import apply_config, get_core_config
from .datatypes import ISCHEMA_NAMES, register_extension_types

__version__ = "0.13.5"
sqlalchemy_version = sqlalchemy.__version__
duckdb_version: str = duckdb.__version__
supports_attach: bool = duckdb_version >= "0.7.0"
supports_user_agent: bool = duckdb_version >= "0.9.2"

if TYPE_CHECKING:
    from sqlalchemy.base import Connection
    from sqlalchemy.engine.interfaces import _IndexDict
    from sqlalchemy.sql.type_api import _ResultProcessor

register_extension_types()


class DBAPI:
    paramstyle = "numeric_dollar" if sqlalchemy_version >= "2.0.0" else "qmark"
    apilevel = duckdb.apilevel
    threadsafety = duckdb.threadsafety

    # this is being fixed upstream to add a proper exception hierarchy
    Error = getattr(duckdb, "Error", RuntimeError)
    TransactionException = getattr(duckdb, "TransactionException", Error)
    ParserException = getattr(duckdb, "ParserException", Error)

    @staticmethod
    def Binary(x: Any) -> Any:
        return x


class DuckDBInspector(PGInspector):
    def get_check_constraints(
        self, table_name: str, schema: Optional[str] = None, **kw: Any
    ) -> List[Dict[str, Any]]:
        try:
            return super().get_check_constraints(table_name, schema, **kw)
        except Exception as e:
            raise NotImplementedError() from e


class ConnectionWrapper:
    __c: duckdb.DuckDBPyConnection
    notices: List[str]
    autocommit = None  # duckdb doesn't support setting autocommit
    closed = False

    def __init__(self, c: duckdb.DuckDBPyConnection) -> None:
        self.__c = c
        self.notices = list()

    def cursor(self) -> "CursorWrapper":
        return CursorWrapper(self.__c, self)

    def __getattr__(self, name: str) -> Any:
        return getattr(self.__c, name)

    def close(self) -> None:
        self.__c.close()
        self.closed = True


class CursorWrapper:
    __c: duckdb.DuckDBPyConnection
    __connection_wrapper: "ConnectionWrapper"

    def __init__(
        self, c: duckdb.DuckDBPyConnection, connection_wrapper: "ConnectionWrapper"
    ) -> None:
        self.__c = c
        self.__connection_wrapper = connection_wrapper

    def executemany(
        self,
        statement: str,
        parameters: Optional[List[Dict]] = None,
        context: Optional[Any] = None,
    ) -> None:
        self.__c.executemany(statement, list(parameters) if parameters else [])

    def execute(
        self,
        statement: str,
        parameters: Optional[Tuple] = None,
        context: Optional[Any] = None,
    ) -> None:
        try:
            if statement.lower() == "commit":  # this is largely for ipython-sql
                self.__c.commit()
            elif statement.lower() in (
                "register",
                "register(?, ?)",
                "register($1, $2)",
            ):
                assert parameters and len(parameters) == 2, parameters
                view_name, df = parameters
                self.__c.register(view_name, df)
            elif parameters is None:
                self.__c.execute(statement)
            else:
                self.__c.execute(statement, parameters)
        except RuntimeError as e:
            if e.args[0].startswith("Not implemented Error"):
                raise NotImplementedError(*e.args) from e
            elif (
                e.args[0]
                == "TransactionContext Error: cannot commit - no transaction is active"
            ):
                return
            else:
                raise e

    @property
    def connection(self) -> "Connection":
        return self.__connection_wrapper

    def close(self) -> None:
        pass  # closing cursors is not supported in duckdb

    def __getattr__(self, name: str) -> Any:
        return getattr(self.__c, name)

    def fetchmany(self, size: Optional[int] = None) -> List:
        if size is None:
            return self.__c.fetchmany()
        else:
            return self.__c.fetchmany(size)


class DuckDBEngineWarning(Warning):
    pass


def index_warning() -> None:
    warnings.warn(
        "duckdb-engine doesn't yet support reflection on indices",
        DuckDBEngineWarning,
    )


class DuckDBIdentifierPreparer(PGIdentifierPreparer):
    def _separate(self, name: Optional[str]) -> Tuple[Optional[Any], Optional[str]]:
        """
        Get database name and schema name from schema if it contains a database name
            Format:
              <db_name>.<schema_name>
              db_name and schema_name are double quoted if contains spaces or double quotes
        """
        database_name, schema_name = None, name
        if name is not None and "." in name:
            database_name, schema_name = (
                max(s) for s in re.findall(r'"([^.]+)"|([^.]+)', name)
            )
        return database_name, schema_name

    def format_schema(self, name: str) -> str:
        """Prepare a quoted schema name."""
        database_name, schema_name = self._separate(name)
        if database_name is None:
            return self.quote(name)
        return ".".join(self.quote(_n) for _n in [database_name, schema_name])

    def quote_schema(self, schema: str, force: Any = None) -> str:
        """
        Conditionally quote a schema name.

        :param schema: string schema name
        :param force: unused
        """
        return self.format_schema(schema)


class DuckDBNullType(sqltypes.NullType):
    def result_processor(
        self, dialect: RootDialect, coltype: sqltypes.TypeEngine
    ) -> Optional["_ResultProcessor"]:
        if coltype == "JSON":
            return sqltypes.JSON().result_processor(dialect, coltype)
        else:
            return super().result_processor(dialect, coltype)


class Dialect(PGDialect_psycopg2):
    name = "duckdb"
    driver = "duckdb_engine"
    _has_events = False
    supports_statement_cache = False
    supports_comments = has_comment_support()
    supports_sane_rowcount = False
    supports_server_side_cursors = False
    inspector = DuckDBInspector
    # colspecs TODO: remap types to duckdb types
    colspecs = util.update_copy(
        PGDialect.colspecs,
        {
            # the psycopg2 driver registers a _PGNumeric with custom logic for
            # postgres type_codes (such as 701 for float) that duckdb doesn't have
            sqltypes.Numeric: sqltypes.Numeric,
            sqltypes.JSON: sqltypes.JSON,
            UUID: UUID,
        },
    )
    ischema_names = util.update_copy(
        PGDialect.ischema_names,
        ISCHEMA_NAMES,
    )
    preparer = DuckDBIdentifierPreparer
    identifier_preparer: DuckDBIdentifierPreparer

    def __init__(self, *args: Any, **kwargs: Any) -> None:
        kwargs["use_native_hstore"] = False
        super().__init__(*args, **kwargs)

    def type_descriptor(self, typeobj: Type[sqltypes.TypeEngine]) -> Any:  # type: ignore[override]
        res = super().type_descriptor(typeobj)

        if isinstance(res, sqltypes.NullType):
            return DuckDBNullType()

        return res

    def connect(self, *cargs: Any, **cparams: Any) -> "Connection":
        core_keys = get_core_config()
        preload_extensions = cparams.pop("preload_extensions", [])
        config = cparams.setdefault("config", {})
        config.update(cparams.pop("url_config", {}))

        ext = {k: config.pop(k) for k in list(config) if k not in core_keys}
        if supports_user_agent:
            user_agent = f"duckdb_engine/{__version__}(sqlalchemy/{sqlalchemy_version})"
            if "custom_user_agent" in config:
                user_agent = f"{user_agent} {config['custom_user_agent']}"
            config["custom_user_agent"] = user_agent

        conn = duckdb.connect(*cargs, **cparams)

        for extension in preload_extensions:
            conn.execute(f"LOAD {extension}")

        apply_config(self, conn, ext)

        return ConnectionWrapper(conn)

    def on_connect(self) -> None:
        pass

    @classmethod
    def get_pool_class(cls, url: URL) -> Type[pool.Pool]:
        if url.database == ":memory:":
            return pool.SingletonThreadPool
        else:
            return pool.QueuePool

    @staticmethod
    def dbapi(**kwargs: Any) -> Type[DBAPI]:
        return DBAPI

    def _get_server_version_info(self, connection: "Connection") -> Tuple[int, int]:
        return (8, 0)

    def get_default_isolation_level(self, connection: "Connection") -> None:
        raise NotImplementedError()

    def do_rollback(self, connection: "Connection") -> None:
        try:
            super().do_rollback(connection)
        except DBAPI.TransactionException as e:
            if (
                e.args[0]
                != "TransactionContext Error: cannot rollback - no transaction is active"
            ):
                raise e

    def do_begin(self, connection: "Connection") -> None:
        connection.begin()

    def get_view_names(
        self,
        connection: Any,
        schema: Optional[Any] = None,
        include: Optional[Any] = None,
        **kw: Any,
    ) -> Any:
        s = """
            SELECT table_name
            FROM information_schema.tables
            WHERE
                table_type='VIEW'
                AND table_schema = :schema_name
            """
        params = {}
        database_name = None

        if schema is not None:
            database_name, schema = self.identifier_preparer._separate(schema)
        else:
            schema = "main"

        params.update({"schema_name": schema})

        if database_name is not None:
            s += "AND table_catalog = :database_name\n"
            params.update({"database_name": database_name})

        rs = connection.execute(text(s), params)
        return [view for (view,) in rs]

    @cache  # type: ignore[call-arg]
    def get_schema_names(self, connection: "Connection", **kw: "Any"):  # type: ignore[no-untyped-def]
        """
        Return unquoted database_name.schema_name unless either contains spaces or double quotes.
        In that case, escape double quotes and then wrap in double quotes.
        SQLAlchemy definition of a schema includes database name for databases like SQL Server (Ex: databasename.dbo)
        (see https://docs.sqlalchemy.org/en/20/dialects/mssql.html#multipart-schema-names)
        """

        if not supports_attach:
            return super().get_schema_names(connection, **kw)

        s = """
            SELECT database_name, schema_name AS nspname
            FROM duckdb_schemas()
            WHERE schema_name NOT LIKE 'pg\\_%' ESCAPE '\\'
            ORDER BY database_name, nspname
            """
        rs = connection.execute(text(s))

        qs = self.identifier_preparer.quote_schema
        return [qs(".".join(nspname)) for nspname in rs]

    def _build_query_where(
        self,
        table_name: Optional[str] = None,
        schema_name: Optional[str] = None,
        database_name: Optional[str] = None,
    ) -> Tuple[str, Dict[str, str]]:
        sql = ""
        params = {}

        # If no database name is provided, try to get it from the schema name
        # specified as "<db name>.<schema name>"
        # If only a schema name is found, database_name will return None
        if database_name is None and schema_name is not None:
            database_name, schema_name = self.identifier_preparer._separate(schema_name)

        if table_name is not None:
            sql += "AND table_name = :table_name\n"
            params.update({"table_name": table_name})

        if schema_name is not None:
            sql += "AND schema_name = :schema_name\n"
            params.update({"schema_name": schema_name})

        if database_name is not None:
            sql += "AND database_name = :database_name\n"
            params.update({"database_name": database_name})

        return sql, params

    @cache  # type: ignore[call-arg]
    def get_table_names(self, connection: "Connection", schema=None, **kw: "Any"):  # type: ignore[no-untyped-def]
        """
        Return unquoted database_name.schema_name unless either contains spaces or double quotes.
        In that case, escape double quotes and then wrap in double quotes.
        SQLAlchemy definition of a schema includes database name for databases like SQL Server (Ex: databasename.dbo)
        (see https://docs.sqlalchemy.org/en/20/dialects/mssql.html#multipart-schema-names)
        """

        if not supports_attach:
            return super().get_table_names(connection, schema, **kw)

        s = """
            SELECT database_name, schema_name, table_name
            FROM duckdb_tables()
            WHERE schema_name NOT LIKE 'pg\\_%' ESCAPE '\\'
            """
        sql, params = self._build_query_where(schema_name=schema)
        s += sql
        rs = connection.execute(text(s), params)

        return [
            table
            for (
                db,
                sc,
                table,
            ) in rs
        ]

    @cache  # type: ignore[call-arg]
    def get_table_oid(  # type: ignore[no-untyped-def]
        self,
        connection: "Connection",
        table_name: str,
        schema: "Optional[str]" = None,
        **kw: "Any",
    ):
        """Fetch the oid for (database.)schema.table_name.
        The schema name can be formatted either as database.schema or just the schema name.
        In the latter scenario the schema associated with the default database is used.
        """
        s = """
            SELECT oid, table_name
            FROM (
                SELECT table_oid AS oid, table_name,              database_name, schema_name FROM duckdb_tables()
                UNION ALL BY NAME
                SELECT view_oid AS oid , view_name AS table_name, database_name, schema_name FROM duckdb_views()
            )
            WHERE schema_name NOT LIKE 'pg\\_%' ESCAPE '\\'
            """
        sql, params = self._build_query_where(table_name=table_name, schema_name=schema)
        s += sql

        rs = connection.execute(text(s), params)
        table_oid = rs.scalar()
        if table_oid is None:
            raise NoSuchTableError(table_name)
        return table_oid

    def has_table(
        self,
        connection: "Connection",
        table_name: str,
        schema: Optional[str] = None,
        **kw: Any,
    ) -> bool:
        try:
            return self.get_table_oid(connection, table_name, schema) is not None
        except NoSuchTableError:
            return False

    def get_indexes(
        self,
        connection: "Connection",
        table_name: str,
        schema: Optional[str] = None,
        **kw: Any,
    ) -> List["_IndexDict"]:
        index_warning()
        return []

    # the following methods are for SQLA2 compatibility
    def get_multi_indexes(
        self,
        connection: "Connection",
        schema: Optional[str] = None,
        filter_names: Optional[Collection[str]] = None,
        **kw: Any,
    ) -> Iterable[Tuple]:
        index_warning()
        return []

    def initialize(self, connection: "Connection") -> None:
        DefaultDialect.initialize(self, connection)

    def create_connect_args(self, url: URL) -> Tuple[tuple, dict]:
        opts = url.translate_connect_args(database="database")
        opts["url_config"] = dict(url.query)
        user = opts["url_config"].pop("user", None)
        if user is not None:
            opts["database"] += f"?user={user}"
        return (), opts

    @classmethod
    def import_dbapi(cls: Type["Dialect"]) -> Type[DBAPI]:
        return cls.dbapi()

    def do_executemany(
        self, cursor: Any, statement: Any, parameters: Any, context: Optional[Any] = ...
    ) -> None:
        return DefaultDialect.do_executemany(
            self, cursor, statement, parameters, context
        )

    def _pg_class_filter_scope_schema(
        self,
        query: Select,
        schema: str,
        scope: Any,
        pg_class_table: Any = None,
    ) -> Any:
        # Don't scope by schema for now
        if hasattr(super(), "_pg_class_filter_scope_schema"):
            query = getattr(super(), "_pg_class_filter_scope_schema")(
                query, schema=None, scope=scope, pg_class_table=pg_class_table
            )
            if schema is not None:
                # Now let's scope by schema, but make sure we're not adding in the database name prefix
                # This will not work if a schema or table name is not unique!
                _, schema_name = self.identifier_preparer._separate(schema)
                query = query.where(
                    text("pg_namespace.nspname = :schema_name").bindparams(
                        schema_name=schema_name
                    )
                )
            return query

    # FIXME: this method is a hack around the fact that we use a single cursor for all queries inside a connection,
    #   and this is required to fix get_multi_columns
    def get_multi_columns(
        self,
        connection: "Connection",
        schema: Optional[str] = None,
        filter_names: Optional[Set[str]] = None,
        scope: Optional[str] = None,
        kind: Optional[Tuple[str, ...]] = None,
        **kw: Any,
    ) -> List:
        """
        Copyright 2005-2023 SQLAlchemy authors and contributors <see AUTHORS file>.

        Permission is hereby granted, free of charge, to any person obtaining a copy of
        this software and associated documentation files (the "Software"), to deal in
        the Software without restriction, including without limitation the rights to
        use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
        of the Software, and to permit persons to whom the Software is furnished to do
        so, subject to the following conditions:

        The above copyright notice and this permission notice shall be included in all
        copies or substantial portions of the Software.

        THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
        IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
        FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
        AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
        LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
        OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
        SOFTWARE.
        """

        has_filter_names, params = self._prepare_filter_names(filter_names)  # type: ignore[attr-defined]
        query = self._columns_query(schema, has_filter_names, scope, kind)  # type: ignore[attr-defined]
        rows = list(connection.execute(query, params).mappings())

        # dictionary with (name, ) if default search path or (schema, name)
        # as keys
        domains: Dict[tuple, dict] = {}
        """
        TODO: fix these pg_collation errors in SQLA2
        domains = {
            ((d["schema"], d["name"]) if not d["visible"] else (d["name"],)): d
            for d in self._load_domains(  # type: ignore[attr-defined]
                connection, schema="*", info_cache=kw.get("info_cache")
            )
        }
        """

        # dictionary with (name, ) if default search path or (schema, name)
        # as keys
        enums = dict(
            (
                ((rec["name"],), rec)
                if rec["visible"]
                else ((rec["schema"], rec["name"]), rec)
            )
            for rec in self._load_enums(  # type: ignore[attr-defined]
                connection, schema="*", info_cache=kw.get("info_cache")
            )
        )

        columns = self._get_columns_info(rows, domains, enums, schema)  # type: ignore[attr-defined]

        return columns.items()

    # fix for https://github.com/Mause/duckdb_engine/issues/1128
    # (Overrides sqlalchemy method)
    @lru_cache()
    def _comment_query(  # type: ignore[no-untyped-def]
        self, schema: str, has_filter_names: bool, scope: Any, kind: Any
    ):
        if sqlalchemy.__version__ >= "2.0.36":
            from sqlalchemy.dialects.postgresql import (  # type: ignore[attr-defined]
                pg_catalog,
            )

            if (
                hasattr(super(), "_kind_to_relkinds")
                and hasattr(super(), "_pg_class_filter_scope_schema")
                and hasattr(super(), "_pg_class_relkind_condition")
            ):
                relkinds = getattr(super(), "_kind_to_relkinds")(kind)
                query = (
                    select(
                        pg_catalog.pg_class.c.relname,
                        pg_catalog.pg_description.c.description,
                    )
                    .select_from(pg_catalog.pg_class)
                    .outerjoin(
                        pg_catalog.pg_description,
                        sql.and_(
                            pg_catalog.pg_class.c.oid
                            == pg_catalog.pg_description.c.objoid,
                            pg_catalog.pg_description.c.objsubid == 0,
                        ),
                    )
                    .where(getattr(super(), "_pg_class_relkind_condition")(relkinds))
                )
                query = self._pg_class_filter_scope_schema(query, schema, scope)
                if has_filter_names:
                    query = query.where(
                        pg_catalog.pg_class.c.relname.in_(bindparam("filter_names"))
                    )
                return query
        else:
            if hasattr(super(), "_comment_query"):
                return getattr(super(), "_comment_query")(
                    schema, has_filter_names, scope, kind
                )


if sqlalchemy.__version__ >= "2.0.14":
    from sqlalchemy import TryCast  # type: ignore[attr-defined]

    @compiles(TryCast, "duckdb")  # type: ignore[misc]
    def visit_try_cast(
        instance: TryCast,
        compiler: PGTypeCompiler,
        **kw: Any,
    ) -> str:
        return "TRY_CAST({} AS {})".format(
            compiler.process(instance.clause, **kw),
            compiler.process(instance.typeclause, **kw),
        )
