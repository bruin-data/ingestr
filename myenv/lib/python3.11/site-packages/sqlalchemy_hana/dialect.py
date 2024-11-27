"""Dialect for SAP HANA."""

from __future__ import annotations

import contextlib
from contextlib import closing
from types import ModuleType
from typing import TYPE_CHECKING, Any, Callable, cast

import hdbcli.dbapi
import sqlalchemy
from sqlalchemy import (
    Computed,
    Identity,
    Integer,
    PrimaryKeyConstraint,
    Sequence,
    exc,
    sql,
    types,
    util,
)
from sqlalchemy.engine import Connection, default, reflection
from sqlalchemy.schema import CreateColumn
from sqlalchemy.sql import Select, compiler, sqltypes
from sqlalchemy.sql.ddl import _DropView as BaseDropView
from sqlalchemy.sql.elements import (
    BinaryExpression,
    BindParameter,
    UnaryExpression,
    quoted_name,
)

from sqlalchemy_hana import types as hana_types
from sqlalchemy_hana.elements import CreateView, DropView, Upsert

if TYPE_CHECKING:
    from typing import ParamSpec, TypeVar

    from sqlalchemy import PoolProxiedConnection
    from sqlalchemy.engine import ConnectArgsType
    from sqlalchemy.engine.interfaces import (
        DBAPIConnection,
        DBAPICursor,
        ReflectedCheckConstraint,
        ReflectedColumn,
        ReflectedForeignKeyConstraint,
        ReflectedIndex,
        ReflectedPrimaryKeyConstraint,
        ReflectedTableComment,
        ReflectedUniqueConstraint,
    )
    from sqlalchemy.engine.url import URL
    from sqlalchemy.schema import (
        ColumnCollectionConstraint,
        CreateTable,
        DropConstraint,
    )
    from sqlalchemy.sql.elements import ExpressionClauseList
    from sqlalchemy.sql.selectable import ForUpdateArg

    RET = TypeVar("RET")
    PARAM = ParamSpec("PARAM")

with contextlib.suppress(ImportError):
    # pylint: disable=unused-import
    import alembic  # noqa: F401

    import sqlalchemy_hana.alembic  # noqa: F401

RESERVED_WORDS = {
    "all",
    "alter",
    "as",
    "before",
    "begin",
    "both",
    "case",
    "char",
    "condition",
    "connect",
    "cross",
    "cube",
    "current_connection",
    "current_date",
    "current_schema",
    "current_time",
    "current_timestamp",
    "current_transaction_isolation_level",
    "current_user",
    "current_utcdate",
    "current_utctime",
    "current_utctimestamp",
    "currval",
    "cursor",
    "declare",
    "distinct",
    "else",
    "elseif",
    "end",
    "except",
    "exception",
    "exec",
    "false",
    "for",
    "from",
    "full",
    "group",
    "having",
    "if",
    "in",
    "inner",
    "inout",
    "intersect",
    "into",
    "is",
    "join",
    "leading",
    "left",
    "limit",
    "loop",
    "minus",
    "natural",
    "nchar",
    "nextval",
    "null",
    "on",
    "order",
    "out",
    "prior",
    "return",
    "returns",
    "reverse",
    "right",
    "rollup",
    "rowid",
    "select",
    "session_user",
    "set",
    "sql",
    "start",
    "sysuuid",
    "table",
    "tablesample",
    "top",
    "trailing",
    "true",
    "union",
    "unknown",
    "using",
    "utctimestamp",
    "values",
    "when",
    "where",
    "while",
    "with",
}

if sqlalchemy.__version__ < "2":  # pragma: no cover
    # sqlalchemy 1.4 does not like annotations and caching
    def cache(func: Callable[PARAM, RET]) -> Callable[PARAM, RET]:
        return func

else:
    cache = reflection.cache  # type:ignore[assignment]


class HANAIdentifierPreparer(compiler.IdentifierPreparer):
    reserved_words = RESERVED_WORDS


class HANAStatementCompiler(compiler.SQLCompiler):
    def visit_bindparam(  # type:ignore[override] # pylint: disable=arguments-differ
        self, bindparam: BindParameter[Any], **kw: Any
    ) -> Any:
        # SAP HANA supports bindparameters within the columns clause of SELECT statements
        # but it will always treat such columns as NVARCHAR(5000).
        # With the effect that "select([literal(1)])" will return the string '1' instead of
        # an integer.
        # The following logic detects such requests and rewriting the bindparam into a literal
        if kw.get("within_columns_clause") and kw.get("within_label_clause"):
            if (
                bindparam.value
                and bindparam.callable is None
                and not getattr(bindparam, "expanding", False)
            ):
                kw["literal_execute"] = True

        return super().visit_bindparam(bindparam, **kw)

    def visit_sequence(self, sequence: Sequence, **kw: Any) -> str:
        return self.preparer.format_sequence(sequence) + ".NEXTVAL"

    def visit_empty_set_expr(self, element_types: Any, **kw: Any) -> str:
        columns = ", ".join(["1" for _ in element_types])
        return f"SELECT {columns} FROM DUMMY WHERE 1 != 1"

    def default_from(self) -> str:
        return " FROM DUMMY"

    def limit_clause(self, select: Select[Any], **kw: Any) -> str:
        text = ""
        if select._limit_clause is not None:
            text += "\n LIMIT " + self.process(select._limit_clause, **kw)
        if select._offset_clause is not None:
            if select._limit_clause is None:
                # 2147384648 is the max. no. of records per result set
                # we can hardcode the value, because using a limit would lead to another cache key
                text += "\n LIMIT 2147384648"
            text += " OFFSET " + self.process(select._offset_clause, **kw)
        return text

    def for_update_clause(self, select: Select[Any], **kw: Any) -> str:
        for_update = cast("ForUpdateArg", select._for_update_arg)
        tmp = " FOR SHARE LOCK" if for_update.read else " FOR UPDATE"

        if for_update.of:
            tmp += " OF " + ", ".join(
                self.process(elem, **kw) for elem in for_update.of
            )
        if for_update.nowait:
            tmp += " NOWAIT"
        if for_update.skip_locked:
            tmp += " IGNORE LOCKED"

        return tmp

    # SAP HANA doesn't support the "IS DISTINCT FROM" operator but it is
    # possible to rewrite the expression.
    # https://answers.sap.com/questions/642124/hana-and-'is-distinct-from'-operator.html
    def visit_is_distinct_from_binary(
        self, binary: BinaryExpression[Any], operator: Any, **kw: Any
    ) -> str:
        left = self.process(binary.left)
        right = self.process(binary.right)
        return (
            f"(({left} <> {right} OR {left} IS NULL OR {right} IS NULL) "
            f"AND NOT ({left} IS NULL AND {right} IS NULL))"
        )

    def visit_is_not_distinct_from_binary(
        self, binary: BinaryExpression[Any], operator: Any, **kw: Any
    ) -> str:
        left = self.process(binary.left)
        right = self.process(binary.right)
        return (
            f"(NOT ({left} <> {right} OR {left} IS NULL OR {right} IS NULL) OR "
            f"({left} IS NULL AND {right} IS NULL))"
        )

    # SAP HANA supports native boolean types but it doesn't support a reduced
    # where clause like:
    #   SELECT 1 FROM DUMMY WHERE TRUE
    #   SELECT 1 FROM DUMMY WHERE FALSE

    def visit_is_true_unary_operator(
        self, element: UnaryExpression[Any], operator: Any, **kw: Any
    ) -> str:
        return f"{self.process(element.element, **kw)} = TRUE"

    def visit_is_false_unary_operator(
        self, element: UnaryExpression[Any], operator: Any, **kw: Any
    ) -> str:
        return f"{self.process(element.element, **kw)} = FALSE"

    def _regexp_match(
        self, op: str, binary: BinaryExpression[Any], operator: Any, **kw: Any
    ) -> str:
        flags = binary.modifiers["flags"]  # type:ignore[index]
        left = self.process(binary.left)
        right = self.process(binary.right)

        statement = f"{left} {op} {right}"
        if flags:
            statement += (
                f" FLAG {self.render_literal_value(flags, sqltypes.STRINGTYPE)}"
            )
        return statement

    def visit_regexp_match_op_binary(
        self, binary: BinaryExpression[Any], operator: Any, **kw: Any
    ) -> str:
        return self._regexp_match("LIKE_REGEXPR", binary, operator, **kw)

    def visit_not_regexp_match_op_binary(
        self, binary: BinaryExpression[Any], operator: Any, **kw: Any
    ) -> str:
        return self._regexp_match("NOT LIKE_REGEXPR", binary, operator, **kw)

    def visit_regexp_replace_op_binary(
        self, binary: BinaryExpression[Any], operator: Any, **kw: Any
    ) -> str:
        flags = binary.modifiers["flags"]  # type:ignore[index]
        clauses = cast("ExpressionClauseList[Any]", binary.right).clauses

        within = self.process(binary.left)
        pattern = self.process(clauses[0])
        replacement = self.process(clauses[1])

        statement = f"REPLACE_REGEXPR({pattern}"
        if flags:
            statement += (
                f" FLAG {self.render_literal_value(flags, sqltypes.STRINGTYPE)}"
            )
        statement += f" IN {within} WITH {replacement})"
        return statement

    def visit_upsert(self, upsert: Upsert, **kw: Any) -> str:
        statement: str = super().visit_insert(upsert, **kw)
        assert statement.startswith("INSERT INTO")
        statement = statement.replace("INSERT INTO", "UPSERT")

        if upsert._where_criteria:
            where = self._generate_delimited_and_list(upsert._where_criteria, **kw)
            if where:
                statement += f" WHERE {where}"

        return statement


class HANATypeCompiler(compiler.GenericTypeCompiler):
    def visit_NUMERIC(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # SAP HANA has no NUMERIC type, therefore delegate to DECIMAL
        return self.visit_DECIMAL(type_)

    def visit_TINYINT(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # SAP HANA special type
        return "TINYINT"

    def visit_SMALLDECIMAL(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # SAP HANA special type
        return "SMALLDECIMAL"

    def visit_SECONDDATE(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # SAP HANA special type
        return "SECONDDATE"

    def visit_ALPHANUM(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # SAP HANA special type
        return self._render_string_type(type_, "ALPHANUM")

    def visit_string(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # Normally string renders as VARCHAR, but we want NVARCHAR
        return self.visit_NVARCHAR(type_, **kw)

    def visit_unicode(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # Normally unicode renders as VARCHAR, but we want NVARCHAR
        return self.visit_NVARCHAR(type_, **kw)

    def visit_TEXT(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # Normally unicode renders as TEXT, but we want NCLOB
        return self.visit_NCLOB(type_, **kw)

    def visit_boolean(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # Check if we want native or non-native booleans
        if self.dialect.supports_native_boolean:
            return self.visit_BOOLEAN(type_)
        return self.visit_TINYINT(type_)

    def visit_BINARY(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # SAP HANA has no BINARY type, therefore delegate to VARBINARY
        return super().visit_VARBINARY(type_, **kw)

    def visit_DOUBLE_PRECISION(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # SAP HANA has no DOUBLE_PRECISION type, therefore delegate to DOUBLE
        return super().visit_DOUBLE(type_, **kw)

    def visit_uuid(self, type_: types.TypeEngine[Any], **kw: Any) -> str:
        # SAP HANA has no UUID type, therefore delegate to NVARCHAR(32)
        return self._render_string_type(type_, "NVARCHAR", length_override=32)


class HANADDLCompiler(compiler.DDLCompiler):
    def visit_unique_constraint(
        self, constraint: ColumnCollectionConstraint, **kw: Any
    ) -> str:
        if len(constraint) == 0:
            return ""

        text = ""
        if constraint.name is not None:
            formatted_name = self.preparer.format_constraint(constraint)
            if formatted_name is not None:
                text += f"CONSTRAINT {formatted_name} "

        constraints_columns = ", ".join(self.preparer.quote(c.name) for c in constraint)
        text += f"UNIQUE ({constraints_columns})"
        text += self.define_constraint_deferrability(constraint)
        return text

    def visit_create_table(self, create: CreateTable, **kw: Any) -> str:
        table = create.element

        # The table._prefixes list outlives the current compilation, meaning changing the list
        # will change it globally. To prevent adding the same prefix multiple times, it is
        # removed again after the super-class'es visit_create_table call, which consumes the
        # table prefixes.

        table_type = table.kwargs.get("hana_table_type")
        appended_index = None
        if table_type:
            # https://github.com/SAP/sqlalchemy-hana/issues/84
            if table._prefixes is None:
                table._prefixes = []
            appended_index = len(table._prefixes)
            table._prefixes.append(table_type.upper())

        result = super().visit_create_table(create)

        if appended_index is not None:
            table._prefixes.pop(appended_index)

        return result

    def visit_drop_constraint(self, drop: DropConstraint, **kw: Any) -> str:
        if isinstance(drop.element, PrimaryKeyConstraint):
            table = self.preparer.format_table(drop.element.table)
            return f"ALTER TABLE {table} DROP PRIMARY KEY"
        return super().visit_drop_constraint(drop, **kw)

    def visit_computed_column(self, generated: Computed, **kw: Any) -> str:
        clause = (
            "GENERATED ALWAYS AS"
            if generated.persisted is None or generated.persisted is True
            else "AS"
        )
        expression = self.sql_compiler.process(
            generated.sqltext, include_table=False, literal_binds=True
        )
        return f"{clause} ({expression})"

    def visit_create_view(self, create: CreateView, **kw: Any) -> str:
        selectable = self.sql_compiler.process(create.selectable, literal_binds=True)
        return f"CREATE VIEW {create.name} AS {selectable}"

    def visit_drop_view(self, drop: DropView | BaseDropView, **kw: Any) -> str:
        if isinstance(drop, BaseDropView):
            return f"DROP VIEW {self.preparer.format_table(drop.element)}"
        return f"DROP VIEW {drop.name}"

    def visit_create_column(
        self, create: CreateColumn, first_pk: bool = False, **kw: Any
    ) -> str:
        if create.element.autoincrement is True and not create.element.identity:
            create.element.identity = Identity()
        return super().visit_create_column(create, first_pk, **kw)


class HANAExecutionContext(default.DefaultExecutionContext):
    def fire_sequence(self, seq: Sequence, type_: Integer) -> int:
        seq = self.identifier_preparer.format_sequence(seq)
        return self._execute_scalar(f"SELECT {seq}.NEXTVAL FROM DUMMY", type_)

    def get_lastrowid(self) -> int:
        self.cursor.execute("SELECT CURRENT_IDENTITY_VALUE() FROM DUMMY")
        res = self.cursor.fetchone()
        assert res, "No lastrowid available"
        return res[0]


class HANAInspector(reflection.Inspector):
    dialect: HANAHDBCLIDialect

    def get_table_oid(self, table_name: str, schema: str | None = None) -> int:
        return self.dialect.get_table_oid(
            self.bind,  # type:ignore[arg-type]
            table_name,
            schema,
            info_cache=self.info_cache,
        )


class HANAHDBCLIDialect(default.DefaultDialect):
    name = "hana"
    driver = "hdbcli"
    default_paramstyle = "qmark"
    max_identifier_length = 127

    statement_compiler = HANAStatementCompiler
    type_compiler = HANATypeCompiler
    ddl_compiler = HANADDLCompiler
    preparer = HANAIdentifierPreparer
    execution_ctx_cls = HANAExecutionContext
    inspector = HANAInspector

    # The Python clients for SAP HANA are responsible and optimized
    # for encoding and decoding Python unicode objects. SQLAlchemy
    # will rely on their capabilities.
    convert_unicode = False

    div_is_floordiv = False
    implicit_returning = False
    postfetch_lastrowid = True
    requires_name_normalize = True
    returns_native_bytes = False
    supports_comments = True
    supports_default_values = False
    supports_empty_insert = False
    supports_for_update_of = True
    supports_identity_columns = True
    supports_is_distinct_from = True
    supports_native_boolean = True
    supports_native_decimal = True
    supports_sane_multi_rowcount = False
    supports_sane_rowcount = False
    supports_schemas = True
    supports_sequences = True
    supports_statement_cache = True
    supports_unicode_binds = True
    supports_unicode_statements = True
    support_views = True

    colspecs = {
        types.Date: hana_types.DATE,
        types.Time: hana_types.TIME,
        types.DateTime: hana_types.TIMESTAMP,
        # these classes extend a mapped class (left side of this map); without listing them here,
        # the wrong class will be used
        hana_types.SECONDDATE: hana_types.SECONDDATE,
    }
    types_with_length = [
        hana_types.VARCHAR,
        hana_types.NVARCHAR,
        hana_types.VARBINARY,
        hana_types.CHAR,
        hana_types.NCHAR,
        hana_types.ALPHANUM,
    ]

    isolation_level = None
    default_schema_name: str  # this is always set for us

    def __init__(
        self,
        isolation_level: str | None = None,
        use_native_boolean: bool = True,
        **kw: Any,
    ) -> None:
        super().__init__(**kw)
        self.isolation_level = isolation_level
        self.supports_native_boolean = use_native_boolean

    @classmethod
    def import_dbapi(cls) -> ModuleType:
        hdbcli.dbapi.paramstyle = cls.default_paramstyle  # type:ignore[assignment]
        return hdbcli.dbapi

    if sqlalchemy.__version__ < "2":  # pragma: no cover
        dbapi = import_dbapi  # type:ignore[assignment]

    def create_connect_args(self, url: URL) -> ConnectArgsType:
        if url.host and url.host.lower().startswith("userkey="):
            kwargs = url.translate_connect_args(host="userkey")
            userkey = url.host[len("userkey=") : len(url.host)]
            kwargs["userkey"] = userkey
        else:
            kwargs = url.translate_connect_args(
                host="address", username="user", database="databaseName"
            )
            kwargs.update(url.query)
            port = 30015
            if kwargs.get("databaseName"):
                port = 30013
            kwargs.setdefault("port", port)

        return (), kwargs

    def connect(self, *args: Any, **kw: Any) -> DBAPIConnection:
        connection = super().connect(*args, **kw)
        connection.setautocommit(False)
        return connection

    def on_connect(self) -> Callable[[DBAPIConnection], None] | None:
        if self.isolation_level is not None:

            def connect(conn: DBAPIConnection) -> None:
                assert self.isolation_level
                self.set_isolation_level(conn, self.isolation_level)

            return connect
        return None

    def is_disconnect(
        self,
        e: Exception,
        connection: PoolProxiedConnection | DBAPIConnection | None,
        cursor: DBAPICursor | None,
    ) -> bool:
        if connection:
            dbapi_connection = cast(hdbcli.dbapi.Connection, connection)
            return not dbapi_connection.isconnected()
        if isinstance(e, hdbcli.dbapi.Error):
            if e.errorcode == -10709:
                return True
        return super().is_disconnect(e, connection, cursor)

    _isolation_lookup = {
        "SERIALIZABLE",
        "READ UNCOMMITTED",
        "READ COMMITTED",
        "REPEATABLE READ",
    }

    def set_isolation_level(
        self, dbapi_connection: DBAPIConnection, level: str
    ) -> None:
        hana_connection = cast(hdbcli.dbapi.Connection, dbapi_connection)
        if level == "AUTOCOMMIT":
            hana_connection.setautocommit(True)
        else:
            hana_connection.setautocommit(False)

            if level not in self._isolation_lookup:
                lookups = ", ".join(self._isolation_lookup)
                raise exc.ArgumentError(
                    f"Invalid value '{level}' for isolation_level. "
                    f"Valid isolation levels for {self.name} are {lookups}"
                )
            with hana_connection.cursor() as cursor:
                cursor.execute(f"SET TRANSACTION ISOLATION LEVEL {level}")

    def get_isolation_level(  # type:ignore[override]
        self, dbapi_connection: hdbcli.dbapi.Connection
    ) -> str:
        with closing(dbapi_connection.cursor()) as cursor:
            cursor.execute("SELECT CURRENT_TRANSACTION_ISOLATION_LEVEL FROM DUMMY")
            result = cursor.fetchone()

        assert result, "no current transaction isolation level found"
        return cast(str, result[0])

    def _get_server_version_info(self, connection: Connection) -> tuple[int, ...]:
        result: str = connection.execute(  # type:ignore[assignment]
            sql.text("SELECT VERSION FROM SYS.M_DATABASE")
        ).scalar()
        return tuple(int(i) for i in result.split("."))

    def _get_default_schema_name(self, connection: Connection) -> str:
        # In this case, the SQLAlchemy Connection object is not yet "ready".
        # Therefore we need to use the raw DBAPI connection object
        with closing(connection.connection.cursor()) as cursor:
            cursor.execute("SELECT CURRENT_USER FROM DUMMY")
            result = cursor.fetchone()

        assert result, "No current user found"
        return self.normalize_name(cast(str, result[0]))

    def _check_unicode_returns(self, connection: Connection) -> bool:
        return True

    def _check_unicode_description(self, connection: Connection) -> bool:
        return True

    def normalize_name(self, name: str) -> str:
        if name is None:
            return None

        if name.upper() == name and not self.identifier_preparer._requires_quotes(
            name.lower()
        ):
            name = name.lower()
        elif name.lower() == name:
            return quoted_name(name, quote=True)

        return name

    def denormalize_name(self, name: str) -> str:
        if name is None:
            return None

        if name.lower() == name and not self.identifier_preparer._requires_quotes(
            name.lower()
        ):
            name = name.upper()
        return name

    @cache
    def has_table(
        self,
        connection: Connection,
        table_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> bool:
        schema_name = schema or self.default_schema_name

        result = connection.execute(
            sql.text(
                "SELECT 1 FROM SYS.TABLES "
                "WHERE SCHEMA_NAME=:schema AND TABLE_NAME=:table "
                "UNION ALL "
                "SELECT 1 FROM SYS.VIEWS "
                "WHERE SCHEMA_NAME=:schema AND VIEW_NAME=:table ",
            ).bindparams(
                schema=self.denormalize_name(schema_name),
                table=self.denormalize_name(table_name),
            )
        )
        return bool(result.first())

    @cache
    def has_schema(self, connection: Connection, schema_name: str, **kw: Any) -> bool:
        result = connection.execute(
            sql.text(
                "SELECT 1 FROM SYS.SCHEMAS WHERE SCHEMA_NAME=:schema",
            ).bindparams(schema=self.denormalize_name(schema_name))
        )
        return bool(result.first())

    @cache
    def has_index(
        self,
        connection: Connection,
        table_name: str,
        index_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> bool:
        schema_name = schema or self.default_schema_name

        result = connection.execute(
            sql.text(
                "SELECT 1 FROM SYS.INDEXES "
                "WHERE SCHEMA_NAME=:schema AND TABLE_NAME=:table AND INDEX_NAME=:index"
            ).bindparams(
                schema=self.denormalize_name(schema_name),
                table=self.denormalize_name(table_name),
                index=self.denormalize_name(index_name),
            )
        )
        return bool(result.first())

    @cache
    def has_sequence(
        self,
        connection: Connection,
        sequence_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> bool:
        schema_name = schema or self.default_schema_name
        result = connection.execute(
            sql.text(
                "SELECT 1 FROM SYS.SEQUENCES "
                "WHERE SCHEMA_NAME=:schema AND SEQUENCE_NAME=:sequence",
            ).bindparams(
                schema=self.denormalize_name(schema_name),
                sequence=self.denormalize_name(sequence_name),
            )
        )
        return bool(result.first())

    @cache
    def get_schema_names(self, connection: Connection, **kw: Any) -> list[str]:
        result = connection.execute(sql.text("SELECT SCHEMA_NAME FROM SYS.SCHEMAS"))

        return list(self.normalize_name(name) for name, in result.fetchall())

    @cache
    def get_table_names(
        self, connection: Connection, schema: str | None = None, **kw: Any
    ) -> list[str]:
        schema_name = schema or self.default_schema_name

        result = connection.execute(
            sql.text(
                "SELECT TABLE_NAME FROM SYS.TABLES WHERE SCHEMA_NAME=:schema AND "
                "IS_USER_DEFINED_TYPE='FALSE' AND IS_TEMPORARY='FALSE' ",
            ).bindparams(
                schema=self.denormalize_name(schema_name),
            )
        )

        tables = list(self.normalize_name(row[0]) for row in result.fetchall())
        return tables

    def get_temp_table_names(
        self, connection: Connection, schema: str | None = None, **kw: Any
    ) -> list[str]:
        schema_name = schema or self.default_schema_name

        result = connection.execute(
            sql.text(
                "SELECT TABLE_NAME FROM SYS.TABLES WHERE SCHEMA_NAME=:schema AND "
                "IS_TEMPORARY='TRUE' ORDER BY TABLE_NAME",
            ).bindparams(
                schema=self.denormalize_name(schema_name),
            )
        )

        temp_table_names = list(
            self.normalize_name(row[0]) for row in result.fetchall()
        )
        return temp_table_names

    def get_view_names(
        self, connection: Connection, schema: str | None = None, **kw: Any
    ) -> list[str]:
        schema_name = schema or self.default_schema_name

        result = connection.execute(
            sql.text(
                "SELECT VIEW_NAME FROM SYS.VIEWS WHERE SCHEMA_NAME=:schema",
            ).bindparams(
                schema=self.denormalize_name(schema_name),
            )
        )

        views = list(self.normalize_name(row[0]) for row in result.fetchall())
        return views

    def get_view_definition(
        self,
        connection: Connection,
        view_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> str:
        schema_name = schema or self.default_schema_name
        result = connection.execute(
            sql.text(
                "SELECT DEFINITION FROM SYS.VIEWS "
                "WHERE VIEW_NAME=:view_name AND SCHEMA_NAME=:schema LIMIT 1",
            ).bindparams(
                view_name=self.denormalize_name(view_name),
                schema=self.denormalize_name(schema_name),
            )
        ).scalar()

        if result is None:
            raise exc.NoSuchTableError()
        return result

    def get_columns(
        self,
        connection: Connection,
        table_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> list[ReflectedColumn]:
        schema_name = schema or self.default_schema_name
        if not self.has_table(connection, table_name, schema_name, **kw):
            raise exc.NoSuchTableError()

        result = connection.execute(
            sql.text(
                """SELECT COLUMN_NAME, DATA_TYPE_NAME, DEFAULT_VALUE, IS_NULLABLE, LENGTH, SCALE,
                    COMMENTS, GENERATED_ALWAYS_AS, GENERATION_TYPE FROM (
                        SELECT SCHEMA_NAME, TABLE_NAME, COLUMN_NAME, POSITION, DATA_TYPE_NAME,
                        DEFAULT_VALUE, IS_NULLABLE, LENGTH, SCALE, COMMENTS,
                        GENERATED_ALWAYS_AS, GENERATION_TYPE
                        FROM SYS.TABLE_COLUMNS UNION ALL
                        SELECT SCHEMA_NAME, VIEW_NAME AS TABLE_NAME, COLUMN_NAME, POSITION,
                        DATA_TYPE_NAME, DEFAULT_VALUE, IS_NULLABLE, LENGTH, SCALE,
                        COMMENTS, GENERATED_ALWAYS_AS, GENERATION_TYPE
                        FROM SYS.VIEW_COLUMNS )
                    AS COLUMS WHERE SCHEMA_NAME=:schema AND TABLE_NAME=:table ORDER BY POSITION
                """
            ).bindparams(
                schema=self.denormalize_name(schema_name),
                table=self.denormalize_name(table_name),
            )
        )

        columns: list[ReflectedColumn] = []
        for row in result.fetchall():
            column = {
                "name": self.normalize_name(row[0]),
                "default": row[2],
                "nullable": row[3] == "TRUE",
                "comment": row[6],
            }

            if row[8] == "ALWAYS CALCULATED AS":  # COL AS EXPR
                column["computed"] = {"sqltext": row[7], "persisted": False}
            elif row[8] == "ALWAYS AS":  # COL GENERATED ALWAYS AS EXPR
                column["computed"] = {"sqltext": row[7], "persisted": True}

            if hasattr(hana_types, row[1]):
                column["type"] = getattr(hana_types, row[1])
            elif hasattr(types, row[1]):
                column["type"] = getattr(types, row[1])
            else:
                util.warn(
                    f"Did not recognize type '{row[1]}' of column '{column['name']}'"
                )
                column["type"] = types.NULLTYPE

            if column["type"] == hana_types.DECIMAL:
                column["type"] = hana_types.DECIMAL(row[4], row[5])
            elif column["type"] == hana_types.FLOAT:
                column["type"] = hana_types.FLOAT(row[4])
            elif column["type"] in self.types_with_length:
                column["type"] = column["type"](row[4])

            columns.append(cast("ReflectedColumn", column))

        return columns

    @cache
    def get_sequence_names(
        self, connection: Connection, schema: str | None = None, **kw: Any
    ) -> list[str]:
        schema_name = schema or self.default_schema_name

        result = connection.execute(
            sql.text(
                "SELECT SEQUENCE_NAME FROM SYS.SEQUENCES "
                "WHERE SCHEMA_NAME=:schema ORDER BY SEQUENCE_NAME"
            ).bindparams(schema=self.denormalize_name(schema_name))
        )
        return [self.normalize_name(row[0]) for row in result]

    def get_foreign_keys(
        self,
        connection: Connection,
        table_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> list[ReflectedForeignKeyConstraint]:
        schema_name = schema or self.default_schema_name
        if not self.has_table(connection, table_name, schema_name, **kw):
            raise exc.NoSuchTableError()

        result = connection.execute(
            sql.text(
                "SELECT CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_SCHEMA_NAME, "
                "REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME, UPDATE_RULE, DELETE_RULE "
                "FROM SYS.REFERENTIAL_CONSTRAINTS "
                "WHERE SCHEMA_NAME=:schema AND TABLE_NAME=:table "
                "ORDER BY CONSTRAINT_NAME, POSITION"
            ).bindparams(
                schema=self.denormalize_name(schema_name),
                table=self.denormalize_name(table_name),
            )
        )
        foreign_keys: dict[str, ReflectedForeignKeyConstraint] = {}
        foreign_keys_list: list[ReflectedForeignKeyConstraint] = []

        for row in result:
            foreign_key_name = self.normalize_name(row[0])

            if foreign_key_name in foreign_keys:
                foreign_key = foreign_keys[foreign_key_name]
                foreign_key["constrained_columns"].append(self.normalize_name(row[1]))
                foreign_key["referred_columns"].append(self.normalize_name(row[4]))
            else:
                foreign_key = {
                    "name": foreign_key_name,
                    "constrained_columns": [self.normalize_name(row[1])],
                    "referred_schema": None,
                    "referred_table": self.normalize_name(row[3]),
                    "referred_columns": [self.normalize_name(row[4])],
                    "options": {"onupdate": row[5], "ondelete": row[6]},
                }

                if row[2] != self.denormalize_name(self.default_schema_name):
                    foreign_key["referred_schema"] = self.normalize_name(row[2])

                foreign_keys[foreign_key_name] = foreign_key
                foreign_keys_list.append(foreign_key)

        return sorted(
            foreign_keys_list,
            key=lambda foreign_key: (
                foreign_key["name"] is not None,
                foreign_key["name"],
            ),
        )

    def get_indexes(
        self,
        connection: Connection,
        table_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> list[ReflectedIndex]:
        schema_name = schema or self.default_schema_name
        if not self.has_table(connection, table_name, schema_name, **kw):
            raise exc.NoSuchTableError()

        result = connection.execute(
            sql.text(
                'SELECT "INDEX_NAME", "COLUMN_NAME", "CONSTRAINT" '
                "FROM SYS.INDEX_COLUMNS "
                "WHERE SCHEMA_NAME=:schema AND TABLE_NAME=:table "
                "ORDER BY POSITION"
            ).bindparams(
                schema=self.denormalize_name(schema_name),
                table=self.denormalize_name(table_name),
            )
        )

        indexes: dict[str, ReflectedIndex] = {}
        for name, column, constraint in result.fetchall():
            if constraint == "PRIMARY KEY":
                continue

            if not name.startswith("_SYS"):
                name = self.normalize_name(name)
            column = self.normalize_name(column)

            if name not in indexes:
                indexes[name] = {
                    "name": name,
                    "unique": False,
                    "column_names": [column],
                }

                if constraint is not None:
                    indexes[name]["unique"] = "UNIQUE" in constraint.upper()

            else:
                indexes[name]["column_names"].append(column)

        return sorted(
            list(indexes.values()),
            key=lambda index: (index["name"] is not None, index["name"]),
        )

    def get_pk_constraint(
        self,
        connection: Connection,
        table_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> ReflectedPrimaryKeyConstraint:
        schema_name = schema or self.default_schema_name
        if not self.has_table(connection, table_name, schema_name, **kw):
            raise exc.NoSuchTableError()

        result = connection.execute(
            sql.text(
                "SELECT CONSTRAINT_NAME, COLUMN_NAME FROM SYS.CONSTRAINTS "
                "WHERE SCHEMA_NAME=:schema AND TABLE_NAME=:table AND "
                "IS_PRIMARY_KEY='TRUE' "
                "ORDER BY POSITION"
            ).bindparams(
                schema=self.denormalize_name(schema_name),
                table=self.denormalize_name(table_name),
            )
        )

        constraint_name = None
        constrained_columns = []
        for row in result.fetchall():
            constraint_name = row[0]
            constrained_columns.append(self.normalize_name(row[1]))

        return {
            "name": self.normalize_name(cast(str, constraint_name)),
            "constrained_columns": constrained_columns,
        }

    def get_unique_constraints(
        self,
        connection: Connection,
        table_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> list[ReflectedUniqueConstraint]:
        schema_name = schema or self.default_schema_name
        if not self.has_table(connection, table_name, schema_name, **kw):
            raise exc.NoSuchTableError()

        result = connection.execute(
            sql.text(
                "SELECT CONSTRAINT_NAME, COLUMN_NAME FROM SYS.CONSTRAINTS "
                "WHERE SCHEMA_NAME=:schema AND TABLE_NAME=:table AND "
                "IS_UNIQUE_KEY='TRUE' AND IS_PRIMARY_KEY='FALSE' "
                "ORDER BY CONSTRAINT_NAME, POSITION"
            ).bindparams(
                schema=self.denormalize_name(schema_name),
                table=self.denormalize_name(table_name),
            )
        )

        constraints: list[ReflectedUniqueConstraint] = []
        parsing_constraint = None
        for constraint_name, column_name in result.fetchall():
            if parsing_constraint != constraint_name:
                # Start with new constraint
                parsing_constraint = constraint_name

                constraint: ReflectedUniqueConstraint = {
                    "name": None,
                    "column_names": [],
                    "duplicates_index": None,
                }
                if not constraint_name.startswith("_SYS"):
                    # Constraint has user-defined name
                    constraint["name"] = self.normalize_name(constraint_name)
                    constraint["duplicates_index"] = self.normalize_name(
                        constraint_name
                    )
                constraints.append(constraint)
            constraint["column_names"].append(self.normalize_name(column_name))

        return sorted(
            constraints,
            key=lambda constraint: (constraint["name"] is not None, constraint["name"]),
        )

    def get_check_constraints(
        self,
        connection: Connection,
        table_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> list[ReflectedCheckConstraint]:
        schema_name = schema or self.default_schema_name
        if not self.has_table(connection, table_name, schema_name, **kw):
            raise exc.NoSuchTableError()

        result = connection.execute(
            sql.text(
                "SELECT CONSTRAINT_NAME, CHECK_CONDITION FROM SYS.CONSTRAINTS "
                "WHERE SCHEMA_NAME=:schema AND TABLE_NAME=:table AND "
                "CHECK_CONDITION IS NOT NULL"
            ).bindparams(
                schema=self.denormalize_name(schema_name),
                table=self.denormalize_name(table_name),
            )
        )

        check_conditions: list[ReflectedCheckConstraint] = []

        for row in result.fetchall():
            check_condition: ReflectedCheckConstraint = {
                "name": self.normalize_name(row[0]),
                "sqltext": self.normalize_name(row[1]),
            }
            check_conditions.append(check_condition)

        return sorted(
            check_conditions,
            # technical constraints comes first
            key=lambda constraint: (
                not constraint["name"].startswith("_SYS_"),  # type:ignore[union-attr]
                constraint["name"],
            ),
        )

    def get_table_oid(
        self,
        connection: Connection,
        table_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> int:
        schema_name = schema or self.default_schema_name

        result = connection.execute(
            sql.text(
                "SELECT TABLE_OID FROM SYS.TABLES "
                "WHERE SCHEMA_NAME=:schema AND TABLE_NAME=:table"
            ).bindparams(
                schema=self.denormalize_name(schema_name),
                table=self.denormalize_name(table_name),
            )
        )
        return cast(int, result.scalar())

    def get_table_comment(
        self,
        connection: Connection,
        table_name: str,
        schema: str | None = None,
        **kw: Any,
    ) -> ReflectedTableComment:
        schema_name = schema or self.default_schema_name
        if not self.has_table(connection, table_name, schema_name, **kw):
            raise exc.NoSuchTableError()

        result = connection.execute(
            sql.text(
                "SELECT COMMENTS FROM SYS.TABLES WHERE SCHEMA_NAME=:schema AND TABLE_NAME=:table"
            ).bindparams(
                schema=self.denormalize_name(schema_name),
                table=self.denormalize_name(table_name),
            )
        )

        return {"text": result.scalar()}
