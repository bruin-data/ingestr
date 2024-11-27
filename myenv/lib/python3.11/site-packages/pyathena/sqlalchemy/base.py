# -*- coding: utf-8 -*-
from __future__ import annotations

import re
from typing import (
    TYPE_CHECKING,
    Any,
    Dict,
    List,
    Mapping,
    MutableMapping,
    Optional,
    Pattern,
    Set,
    Tuple,
    Type,
    Union,
    cast,
)

import botocore
from sqlalchemy import exc, schema, text, types, util
from sqlalchemy.engine import Engine, reflection
from sqlalchemy.engine.default import DefaultDialect
from sqlalchemy.sql.compiler import (
    ILLEGAL_INITIAL_CHARACTERS,
    DDLCompiler,
    GenericTypeCompiler,
    IdentifierPreparer,
    SQLCompiler,
)

import pyathena
from pyathena.model import (
    AthenaFileFormat,
    AthenaPartitionTransform,
    AthenaRowFormatSerde,
)
from pyathena.sqlalchemy.types import TINYINT, AthenaDate, AthenaTimestamp
from pyathena.sqlalchemy.util import _HashableDict
from pyathena.util import strtobool

if TYPE_CHECKING:
    from types import ModuleType

    from sqlalchemy import (
        URL,
        Cast,
        CheckConstraint,
        ClauseElement,
        Column,
        Connection,
        Dialect,
        ExecutableDDLElement,
        ForeignKeyConstraint,
        FunctionElement,
        GenerativeSelect,
        PoolProxiedConnection,
        PrimaryKeyConstraint,
        Table,
        UniqueConstraint,
    )
    from sqlalchemy.engine.interfaces import (
        ReflectedForeignKeyConstraint,
        ReflectedIndex,
        ReflectedPrimaryKeyConstraint,
        SchemaTranslateMapType,
    )
    from sqlalchemy.sql.base import _DialectArgDict
    from sqlalchemy.sql.ddl import CreateColumn, CreateTable
    from sqlalchemy.sql.schema import SchemaItem

# https://docs.aws.amazon.com/athena/latest/ug/reserved-words.html#list-of-ddl-reserved-words
DDL_RESERVED_WORDS: Set[str] = {
    "ALL",
    "ALTER",
    "AND",
    "ARRAY",
    "AS",
    "AUTHORIZATION",
    "BETWEEN",
    "BIGINT",
    "BINARY",
    "BOOLEAN",
    "BOTH",
    "BY",
    "CASE",
    "CASHE",
    "CAST",
    "CHAR",
    "COLUMN",
    "COMMIT",
    "CONF",
    "CONSTRAINT",
    "CREATE",
    "CROSS",
    "CUBE",
    "CURRENT",
    "CURRENT_DATE",
    "CURRENT_TIMESTAMP",
    "CURSOR",
    "DATABASE",
    "DATE",
    "DAYOFWEEK",
    "DECIMAL",
    "DELETE",
    "DESCRIBE",
    "DISTINCT",
    "DOUBLE",
    "DROP",
    "ELSE",
    "END",
    "EXCHANGE",
    "EXISTS",
    "EXTENDED",
    "EXTERNAL",
    "EXTRACT",
    "FALSE",
    "FETCH",
    "FLOAT",
    "FLOOR",
    "FOLLOWING",
    "FOR",
    "FOREIGN",
    "FROM",
    "FULL",
    "FUNCTION",
    "GRANT",
    "GROUP",
    "GROUPING",
    "HAVING",
    "IF",
    "IMPORT",
    "IN",
    "INNER",
    "INSERT",
    "INT",
    "INTEGER",
    "INTERSECT",
    "INTERVAL",
    "INTO",
    "IS",
    "JOIN",
    "LATERAL",
    "LEFT",
    "LESS",
    "LIKE",
    "LOCAL",
    "MACRO",
    "MAP",
    "MORE",
    "NONE",
    "NOT",
    "NULL",
    "NUMERIC",
    "OF",
    "ON",
    "ONLY",
    "OR",
    "ORDER",
    "OUT",
    "OUTER",
    "OVER",
    "PARTIALSCAN",
    "PARTITION",
    "PERCENT",
    "PRECEDING",
    "PRECISION",
    "PRESERVE",
    "PRIMARY",
    "PROCEDURE",
    "RANGE",
    "READS",
    "REDUCE",
    "REFERENCES",
    "REGEXP",
    "REVOKE",
    "RIGHT",
    "RLIKE",
    "ROLLBACK",
    "ROLLUP",
    "ROW",
    "ROWS",
    "SELECT",
    "SET",
    "SMALLINT",
    "START",
    "TABLE",
    "TABLESAMPLE",
    "THEN",
    "TIME",
    "TIMESTAMP",
    "TO",
    "TRANSFORM",
    "TRIGGER",
    "TRUE",
    "TRUNCATE",
    "UNBOUNDED",
    "UNION",
    "UNIQUEJOIN",
    "UPDATE",
    "USER",
    "USING",
    "UTC_TIMESTAMP",
    "VALUES",
    "VARCHAR",
    "VIEWS",
    "WHEN",
    "WHERE",
    "WINDOW",
    "WITH",
}
# https://docs.aws.amazon.com/athena/latest/ug/reserved-words.html#list-of-reserved-words-sql-select
SELECT_STATEMENT_RESERVED_WORDS: Set[str] = {
    "ALL",
    "ALTER",
    "AND",
    "ARRAY",
    "AS",
    "AUTHORIZATION",
    "BETWEEN",
    "BIGINT",
    "BINARY",
    "BOOLEAN",
    "BOTH",
    "BY",
    "CASE",
    "CASHE",
    "CAST",
    "CHAR",
    "COLUMN",
    "COMMIT",
    "CONF",
    "CONSTRAINT",
    "CREATE",
    "CROSS",
    "CUBE",
    "CURRENT",
    "CURRENT_DATE",
    "CURRENT_TIMESTAMP",
    "CURSOR",
    "DATABASE",
    "DATE",
    "DAYOFWEEK",
    "DECIMAL",
    "DELETE",
    "DESCRIBE",
    "DISTINCT",
    "DOUBLE",
    "DROP",
    "ELSE",
    "END",
    "EXCHANGE",
    "EXISTS",
    "EXTENDED",
    "EXTERNAL",
    "EXTRACT",
    "FALSE",
    "FETCH",
    "FLOAT",
    "FLOOR",
    "FOLLOWING",
    "FOR",
    "FOREIGN",
    "FROM",
    "FULL",
    "FUNCTION",
    "GRANT",
    "GROUP",
    "GROUPING",
    "HAVING",
    "IF",
    "IMPORT",
    "IN",
    "INNER",
    "INSERT",
    "INT",
    "INTEGER",
    "INTERSECT",
    "INTERVAL",
    "INTO",
    "IS",
    "JOIN",
    "LATERAL",
    "LEFT",
    "LESS",
    "LIKE",
    "LOCAL",
    "MACRO",
    "MAP",
    "MORE",
    "NONE",
    "NOT",
    "NULL",
    "NUMERIC",
    "OF",
    "ON",
    "ONLY",
    "OR",
    "ORDER",
    "OUT",
    "OUTER",
    "OVER",
    "PARTIALSCAN",
    "PARTITION",
    "PERCENT",
    "PRECEDING",
    "PRECISION",
    "PRESERVE",
    "PRIMARY",
    "PROCEDURE",
    "RANGE",
    "READS",
    "REDUCE",
    "REFERENCES",
    "REGEXP",
    "REVOKE",
    "RIGHT",
    "RLIKE",
    "ROLLBACK",
    "ROLLUP",
    "ROW",
    "ROWS",
    "SELECT",
    "SET",
    "SMALLINT",
    "START",
    "TABLE",
    "TABLESAMPLE",
    "THEN",
    "TIME",
    "TIMESTAMP",
    "TO",
    "TRANSFORM",
    "TRIGGER",
    "TRUE",
    "TRUNCATE",
    "UNBOUNDED",
    "UNION",
    "UNIQUEJOIN",
    "UPDATE",
    "USER",
    "USING",
    "UTC_TIMESTAMP",
    "VALUES",
    "VARCHAR",
    "VIEWS",
    "WHEN",
    "WHERE",
    "WINDOW",
    "WITH",
}
RESERVED_WORDS: Set[str] = set(sorted(DDL_RESERVED_WORDS | SELECT_STATEMENT_RESERVED_WORDS))

ischema_names: Dict[str, Type[Any]] = {
    "boolean": types.BOOLEAN,
    "float": types.FLOAT,
    # TODO: types.DOUBLE is not defined in SQLAlchemy 1.4.
    "double": types.FLOAT,
    "real": types.FLOAT,
    "tinyint": TINYINT,
    "smallint": types.SMALLINT,
    "integer": types.INTEGER,
    "int": types.INTEGER,
    "bigint": types.BIGINT,
    "decimal": types.DECIMAL,
    "char": types.CHAR,
    "varchar": types.VARCHAR,
    "string": types.String,
    "date": types.DATE,
    "timestamp": types.TIMESTAMP,
    "binary": types.BINARY,
    "varbinary": types.BINARY,
    "array": types.String,
    "map": types.String,
    "struct": types.String,
    "row": types.String,
    "json": types.String,
}


class AthenaDMLIdentifierPreparer(IdentifierPreparer):
    reserved_words: Set[str] = SELECT_STATEMENT_RESERVED_WORDS


class AthenaDDLIdentifierPreparer(IdentifierPreparer):
    reserved_words = DDL_RESERVED_WORDS
    illegal_initial_characters = ILLEGAL_INITIAL_CHARACTERS.union("_")

    def __init__(
        self,
        dialect: "Dialect",
        initial_quote: str = "`",
        final_quote: Optional[str] = None,
        escape_quote: str = "`",
        quote_case_sensitive_collations: bool = True,
        omit_schema: bool = False,
    ):
        super().__init__(
            dialect=dialect,
            initial_quote=initial_quote,
            final_quote=final_quote,
            escape_quote=escape_quote,
            quote_case_sensitive_collations=quote_case_sensitive_collations,
            omit_schema=omit_schema,
        )


class AthenaStatementCompiler(SQLCompiler):
    def visit_char_length_func(self, fn: "FunctionElement[Any]", **kw):
        return f"length{self.function_argspec(fn, **kw)}"

    def visit_cast(self, cast: "Cast[Any]", **kwargs):
        if (isinstance(cast.type, types.VARCHAR) and cast.type.length is None) or isinstance(
            cast.type, types.String
        ):
            type_clause = "VARCHAR"
        elif isinstance(cast.type, types.CHAR) and cast.type.length is None:
            type_clause = "CHAR"
        elif isinstance(cast.type, (types.BINARY, types.VARBINARY)):
            type_clause = "VARBINARY"
        elif isinstance(cast.type, (types.FLOAT, types.Float, types.REAL)):
            # https://docs.aws.amazon.com/athena/latest/ug/data-types.html
            # In Athena, use float in DDL statements like CREATE TABLE
            # and real in SQL functions like SELECT CAST.
            type_clause = "REAL"
        else:
            type_clause = cast.typeclause._compiler_dispatch(self, **kwargs)
        return f"CAST({cast.clause._compiler_dispatch(self, **kwargs)} AS {type_clause})"

    def limit_clause(self, select: "GenerativeSelect", **kw):
        text = []
        if select._offset_clause is not None:
            text.append(" OFFSET " + self.process(select._offset_clause, **kw))
        if select._limit_clause is not None:
            text.append(" LIMIT " + self.process(select._limit_clause, **kw))
        return "\n".join(text)

    def get_from_hint_text(self, table, text):
        return text

    def format_from_hint_text(self, sqltext, table, hint, iscrud):
        hint_upper = hint.upper()
        if any(
            [
                hint_upper.startswith("FOR TIMESTAMP AS OF"),
                hint_upper.startswith("FOR SYSTEM_TIME AS OF"),
                hint_upper.startswith("FOR VERSION AS OF"),
                hint_upper.startswith("FOR SYSTEM_VERSION AS OF"),
            ]
        ):
            if "AS" in sqltext:
                _, alias = sqltext.split(" AS ", 1)
                return f"{table.original.fullname} {hint} AS {alias}"

        return f"{sqltext} {hint}"


class AthenaTypeCompiler(GenericTypeCompiler):
    def visit_FLOAT(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return self.visit_REAL(type_, **kw)

    def visit_REAL(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return "FLOAT"

    def visit_DOUBLE(self, type_, **kw) -> str:  # noqa: N802
        return "DOUBLE"

    def visit_DOUBLE_PRECISION(self, type_, **kw) -> str:  # noqa: N802
        return "DOUBLE"

    def visit_NUMERIC(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return self.visit_DECIMAL(type_, **kw)

    def visit_DECIMAL(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        if type_.precision is None:
            return "DECIMAL"
        elif type_.scale is None:
            return f"DECIMAL({type_.precision})"
        else:
            return f"DECIMAL({type_.precision}, {type_.scale})"

    def visit_TINYINT(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return "TINYINT"

    def visit_INTEGER(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return "INTEGER"

    def visit_SMALLINT(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return "SMALLINT"

    def visit_BIGINT(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return "BIGINT"

    def visit_TIMESTAMP(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return "TIMESTAMP"

    def visit_DATETIME(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return self.visit_TIMESTAMP(type_, **kw)

    def visit_DATE(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return "DATE"

    def visit_TIME(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        raise exc.CompileError(f"Data type `{type_}` is not supported")

    def visit_CLOB(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return self.visit_BINARY(type_, **kw)

    def visit_NCLOB(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return self.visit_BINARY(type_, **kw)

    def visit_CHAR(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        if type_.length:
            return cast(str, self._render_string_type(type_, "CHAR"))
        return "STRING"

    def visit_NCHAR(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return self.visit_CHAR(type_, **kw)

    def visit_VARCHAR(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        if type_.length:
            return cast(str, self._render_string_type(type_, "VARCHAR"))
        return "STRING"

    def visit_NVARCHAR(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return self.visit_VARCHAR(type_, **kw)

    def visit_TEXT(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return "STRING"

    def visit_BLOB(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return self.visit_BINARY(type_, **kw)

    def visit_BINARY(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return "BINARY"

    def visit_VARBINARY(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return self.visit_BINARY(type_, **kw)

    def visit_BOOLEAN(self, type_: Type[Any], **kw) -> str:  # noqa: N802
        return "BOOLEAN"

    def visit_string(self, type_, **kw):  # noqa: N802
        return "STRING"

    def visit_unicode(self, type_, **kw):  # noqa: N802
        return "STRING"

    def visit_unicode_text(self, type_, **kw):  # noqa: N802
        return "STRING"

    def visit_null(self, type_, **kw):  # noqa: N802
        return "NULL"

    def visit_tinyint(self, type_, **kw):  # noqa: N802
        return self.visit_TINYINT(type_, **kw)


class AthenaDDLCompiler(DDLCompiler):
    @property
    def preparer(self) -> IdentifierPreparer:
        return self._preparer

    @preparer.setter
    def preparer(self, value: IdentifierPreparer):
        pass

    def __init__(
        self,
        dialect: "Dialect",
        statement: "ExecutableDDLElement",
        schema_translate_map: Optional["SchemaTranslateMapType"] = None,
        render_schema_translate: bool = False,
        compile_kwargs: Mapping[str, Any] = util.immutabledict(),
    ):
        self._preparer = AthenaDDLIdentifierPreparer(dialect)
        super().__init__(
            dialect=dialect,
            statement=statement,
            render_schema_translate=render_schema_translate,
            schema_translate_map=schema_translate_map,
            compile_kwargs=compile_kwargs,
        )

    def _escape_comment(self, value: str) -> str:
        value = value.replace("\\", "\\\\").replace("'", r"\'")
        # DDL statements raise a KeyError if the placeholders aren't escaped
        if self.dialect.identifier_preparer._double_percents:
            value = value.replace("%", "%%")
        return f"'{value}'"

    def _get_comment_specification(self, comment: str) -> str:
        return f"COMMENT {self._escape_comment(comment)}"

    def _get_bucket_count(
        self, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> Optional[str]:
        if dialect_opts["bucket_count"]:
            bucket_count = dialect_opts["bucket_count"]
        elif connect_opts:
            bucket_count = connect_opts.get("bucket_count")
        else:
            bucket_count = None
        return cast(str, bucket_count) if bucket_count is not None else None

    def _get_file_format(
        self, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> Optional[str]:
        if dialect_opts["file_format"]:
            file_format = dialect_opts["file_format"]
        elif connect_opts:
            file_format = connect_opts.get("file_format")
        else:
            file_format = None
        return cast(Optional[str], file_format)

    def _get_file_format_specification(
        self, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> str:
        file_format = self._get_file_format(dialect_opts, connect_opts)
        text = []
        if file_format:
            text.append(f"STORED AS {file_format}")
        return "\n".join(text)

    def _get_row_format(
        self, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> Optional[str]:
        if dialect_opts["row_format"]:
            row_format = dialect_opts["row_format"]
        elif connect_opts:
            row_format = connect_opts.get("row_format")
        else:
            row_format = None
        return cast(Optional[str], row_format)

    def _get_row_format_specification(
        self, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> str:
        row_format = self._get_row_format(dialect_opts, connect_opts)
        text = []
        if row_format:
            text.append(f"ROW FORMAT {row_format}")
        return "\n".join(text)

    def _get_serde_properties(
        self, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> Optional[Union[str, Dict[str, Any]]]:
        if dialect_opts["serdeproperties"]:
            serde_properties = dialect_opts["serdeproperties"]
        elif connect_opts:
            serde_properties = connect_opts.get("serdeproperties")
        else:
            serde_properties = None
        return cast(Optional[str], serde_properties)

    def _get_serde_properties_specification(
        self, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> str:
        serde_properties = self._get_serde_properties(dialect_opts, connect_opts)
        text = []
        if serde_properties:
            text.append("WITH SERDEPROPERTIES (")
            if isinstance(serde_properties, dict):
                text.append(",\n".join([f"\t'{k}' = '{v}'" for k, v in serde_properties.items()]))
            else:
                text.append(serde_properties)
            text.append(")")
        return "\n".join(text)

    def _get_table_location(
        self, table, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> Optional[str]:
        if dialect_opts["location"]:
            location = cast(str, dialect_opts["location"])
            location += "/" if not location.endswith("/") else ""
        elif connect_opts:
            base_location = (
                cast(str, connect_opts["location"])
                if "location" in connect_opts
                else cast(str, connect_opts.get("s3_staging_dir"))
            )
            schema = table.schema if table.schema else connect_opts["schema_name"]
            location = f"{base_location}{schema}/{table.name}/"
        else:
            location = None
        return location

    def _get_table_location_specification(
        self, table, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> str:
        location = self._get_table_location(table, dialect_opts, connect_opts)
        text = []
        if location:
            text.append(f"LOCATION '{location}'")
        else:
            if connect_opts:
                raise exc.CompileError(
                    "`location` or `s3_staging_dir` parameter is required "
                    "in the connection string"
                )
            else:
                raise exc.CompileError(
                    "The location of the table should be specified "
                    "by the dialect keyword argument `awsathena_location`"
                )
        return "\n".join(text)

    def _get_table_properties(
        self, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> Optional[Union[Dict[str, str], str]]:
        if dialect_opts["tblproperties"]:
            table_properties = cast(str, dialect_opts["tblproperties"])
        elif connect_opts:
            table_properties = cast(str, connect_opts.get("tblproperties"))
        else:
            table_properties = None
        return table_properties

    def _get_compression(
        self, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> Optional[str]:
        if dialect_opts["compression"]:
            compression = cast(str, dialect_opts["compression"])
        elif connect_opts:
            compression = cast(str, connect_opts.get("compression"))
        else:
            compression = None
        return compression

    def _get_table_properties_specification(
        self, dialect_opts: "_DialectArgDict", connect_opts: Dict[str, Any]
    ) -> str:
        properties = self._get_table_properties(dialect_opts, connect_opts)
        if properties:
            if isinstance(properties, dict):
                table_properties = [",\n".join([f"\t'{k}' = '{v}'" for k, v in properties.items()])]
            else:
                table_properties = [properties]
        else:
            table_properties = []

        compression = self._get_compression(dialect_opts, connect_opts)
        if compression:
            file_format = self._get_file_format(dialect_opts, connect_opts)
            row_format = self._get_row_format(dialect_opts, connect_opts)
            if file_format:
                if file_format == AthenaFileFormat.FILE_FORMAT_PARQUET:
                    table_properties.append(f"\t'parquet.compress' = '{compression}'")
                elif file_format == AthenaFileFormat.FILE_FORMAT_ORC:
                    table_properties.append(f"\t'orc.compress' = '{compression}'")
                else:
                    table_properties.append(f"\t'write.compress' = '{compression}'")
            elif row_format:
                if AthenaRowFormatSerde.is_parquet(row_format):
                    table_properties.append(f"\t'parquet.compress' = '{compression}'")
                elif AthenaRowFormatSerde.is_orc(row_format):
                    table_properties.append(f"\t'orc.compress' = '{compression}'")
                else:
                    table_properties.append(f"\t'write.compress' = '{compression}'")

        text = []
        if table_properties:
            text.append("TBLPROPERTIES (")
            text.append(",\n".join(table_properties))
            text.append(")")
        return "\n".join(text)

    def get_column_specification(self, column: "Column[Any]", **kwargs) -> str:
        if type(column.type) in [types.Integer, types.INTEGER, types.INT]:
            # https://docs.aws.amazon.com/athena/latest/ug/create-table.html
            # In Data Definition Language (DDL) queries like CREATE TABLE,
            # use the int keyword to represent an integer
            type_ = "INT"
        else:
            type_ = self.dialect.type_compiler.process(column.type, type_expression=column)
        text = [f"{self.preparer.format_column(column)} {type_}"]
        if column.comment:
            text.append(f"{self._get_comment_specification(column.comment)}")
        return " ".join(text)

    def visit_check_constraint(self, constraint: "CheckConstraint", **kw) -> Optional[str]:
        return None

    def visit_column_check_constraint(self, constraint: "CheckConstraint", **kw) -> Optional[str]:
        return None

    def visit_foreign_key_constraint(
        self, constraint: "ForeignKeyConstraint", **kw
    ) -> Optional[str]:
        return None

    def visit_primary_key_constraint(
        self, constraint: "PrimaryKeyConstraint", **kw
    ) -> Optional[str]:
        return None

    def visit_unique_constraint(self, constraint: "UniqueConstraint", **kw) -> Optional[str]:
        return None

    def _get_connect_option_partitions(self, connect_opts: Dict[str, Any]) -> List[str]:
        if connect_opts:
            partition = cast(str, connect_opts.get("partition"))
            partitions = partition.split(",") if partition else []
        else:
            partitions = []
        return partitions

    def _get_connect_option_buckets(self, connect_opts: Dict[str, Any]) -> List[str]:
        if connect_opts:
            bucket = cast(str, connect_opts.get("cluster"))
            buckets = bucket.split(",") if bucket else []
        else:
            buckets = []
        return buckets

    def _prepared_partitions(self, column):
        # https://docs.aws.amazon.com/athena/latest/ug/querying-iceberg-creating-tables.html#querying-iceberg-partitioning
        column_dialect_opts = column.dialect_options["awsathena"]
        partition_transform = column_dialect_opts["partition_transform"]

        column_name = self.preparer.format_column(column)
        transform_column = None

        partitions = []

        if partition_transform:
            if AthenaPartitionTransform.is_valid(partition_transform):
                if partition_transform == AthenaPartitionTransform.PARTITION_TRANSFORM_BUCKET:
                    bucket_count = column_dialect_opts["partition_transform_bucket_count"]
                    if bucket_count:
                        transform_column = f"{bucket_count}, {column_name}"
                elif partition_transform == AthenaPartitionTransform.PARTITION_TRANSFORM_TRUNCATE:
                    truncate_length = column_dialect_opts["partition_transform_truncate_length"]
                    if truncate_length:
                        transform_column = f"{truncate_length}, {column_name}"
                else:
                    transform_column = column_name

                if transform_column:
                    partitions.append(f"\t{partition_transform}({transform_column})")
        else:
            partitions.append(f"\t{column_name}")

        return partitions

    def _prepared_columns(
        self, table, is_iceberg, create_columns: List["CreateColumn"], connect_opts: Dict[str, Any]
    ) -> Tuple[List[str], List[str], List[str]]:
        columns, partitions, buckets = [], [], []
        conn_partitions = self._get_connect_option_partitions(connect_opts)
        conn_buckets = self._get_connect_option_buckets(connect_opts)
        for create_column in create_columns:
            column = create_column.element
            column_dialect_opts = column.dialect_options["awsathena"]
            try:
                processed = self.process(create_column)
                if processed is not None:
                    if (
                        column_dialect_opts["partition"]
                        or column.name in conn_partitions
                        or f"{table.name}.{column.name}" in conn_partitions
                    ):
                        # https://docs.aws.amazon.com/athena/latest/ug/querying-iceberg-creating-tables.html#querying-iceberg-partitioning
                        if is_iceberg:
                            partitions.extend(self._prepared_partitions(column=column))
                            columns.append(f"\t{processed}")
                        else:
                            partitions.append(f"\t{processed}")
                    else:
                        columns.append(f"\t{processed}")
                    if (
                        column_dialect_opts["cluster"]
                        or column.name in conn_buckets
                        or f"{table.name}.{column.name}" in conn_buckets
                    ):
                        buckets.append(f"\t{self.preparer.format_column(column)}")
            except exc.CompileError as e:
                raise exc.CompileError(
                    f"(in table '{table.description}', column '{column.name}'): {e.args[0]}"
                ) from e
        return columns, partitions, buckets

    def visit_create_table(self, create: "CreateTable", **kwargs) -> str:
        table = create.element
        dialect_opts = table.dialect_options["awsathena"]
        dialect = cast(AthenaDialect, self.dialect)
        connect_opts = dialect._connect_options

        table_properties = self._get_table_properties_specification(
            dialect_opts, connect_opts
        ).lower()
        is_iceberg = False
        if ("table_type" in table_properties) and ("iceberg" in table_properties):
            is_iceberg = True

        if is_iceberg:
            # https://docs.aws.amazon.com/athena/latest/ug/querying-iceberg-creating-tables.html
            text = ["\nCREATE TABLE"]
        else:
            text = ["\nCREATE EXTERNAL TABLE"]

        if create.if_not_exists:
            text.append("IF NOT EXISTS")
        text.append(self.preparer.format_table(table))
        text.append("(")
        text = [" ".join(text)]

        columns, partitions, buckets = self._prepared_columns(
            table, is_iceberg, create.columns, connect_opts
        )
        text.append(",\n".join(columns))
        text.append(")")

        if table.comment:
            text.append(self._get_comment_specification(table.comment))

        if partitions:
            text.append("PARTITIONED BY (")
            text.append(",\n".join(partitions))
            text.append(")")

        bucket_count = self._get_bucket_count(dialect_opts, connect_opts)
        if buckets and bucket_count:
            text.append("CLUSTERED BY (")
            text.append(",\n".join(buckets))
            text.append(f") INTO {bucket_count} BUCKETS")

        text.append(f"{self.post_create_table(table)}\n")
        return "\n".join(text)

    def post_create_table(self, table: "Table") -> str:
        dialect_opts: "_DialectArgDict" = table.dialect_options["awsathena"]
        dialect = cast(AthenaDialect, self.dialect)
        connect_opts = dialect._connect_options
        text = [
            self._get_row_format_specification(dialect_opts, connect_opts),
            self._get_serde_properties_specification(dialect_opts, connect_opts),
            self._get_file_format_specification(dialect_opts, connect_opts),
            self._get_table_location_specification(table, dialect_opts, connect_opts),
            self._get_table_properties_specification(dialect_opts, connect_opts),
        ]
        return "\n".join([t for t in text if t])


class AthenaDialect(DefaultDialect):
    name: str = "awsathena"
    preparer: Type[IdentifierPreparer] = AthenaDMLIdentifierPreparer
    statement_compiler: Type[SQLCompiler] = AthenaStatementCompiler
    ddl_compiler: Type[DDLCompiler] = AthenaDDLCompiler
    type_compiler: Type[GenericTypeCompiler] = AthenaTypeCompiler
    default_paramstyle: str = pyathena.paramstyle
    cte_follows_insert: bool = True
    supports_alter: bool = False
    supports_pk_autoincrement: Optional[bool] = False
    supports_default_values: bool = False
    supports_empty_insert: bool = False
    supports_multivalues_insert: bool = True
    supports_native_decimal: bool = True
    supports_native_boolean: bool = True
    supports_unicode_statements: Optional[bool] = True
    supports_unicode_binds: Optional[bool] = True
    supports_statement_cache: bool = True
    returns_unicode_strings: Optional[bool] = True
    description_encoding: Optional[bool] = None
    postfetch_lastrowid: bool = False
    construct_arguments: Optional[
        List[Tuple[Type[Union["SchemaItem", "ClauseElement"]], Mapping[str, Any]]]
    ] = [
        (
            schema.Table,
            {
                "location": None,
                "compression": None,
                "row_format": None,
                "file_format": None,
                "serdeproperties": None,
                "tblproperties": None,
                "bucket_count": None,
            },
        ),
        (
            schema.Column,
            {
                "partition": False,
                "partition_transform": None,
                "partition_transform_bucket_count": None,
                "partition_transform_truncate_length": None,
                "cluster": False,
            },
        ),
    ]

    colspecs = {
        types.DATE: AthenaDate,
        types.DATETIME: AthenaTimestamp,
        types.TIMESTAMP: AthenaTimestamp,
    }

    ischema_names: Dict[str, Type[Any]] = ischema_names

    _connect_options: Dict[str, Any] = dict()  # type: ignore
    _pattern_column_type: Pattern[str] = re.compile(r"^([a-zA-Z]+)(?:$|[\(|<](.+)[\)|>]$)")

    def __init__(self, json_deserializer=None, json_serializer=None, **kwargs):
        DefaultDialect.__init__(self, **kwargs)
        self._json_deserializer = json_deserializer
        self._json_serializer = json_serializer

    @classmethod
    def import_dbapi(cls) -> "ModuleType":
        return pyathena

    @classmethod
    def dbapi(cls) -> "ModuleType":  # type: ignore
        return pyathena

    def _raw_connection(self, connection: Union[Engine, "Connection"]) -> "PoolProxiedConnection":
        if isinstance(connection, Engine):
            return connection.raw_connection()
        return connection.connection

    def create_connect_args(self, url: "URL") -> Tuple[Tuple[str], MutableMapping[str, Any]]:
        # Connection string format:
        #   awsathena+rest://
        #   {aws_access_key_id}:{aws_secret_access_key}@athena.{region_name}.amazonaws.com:443/
        #   {schema_name}?s3_staging_dir={s3_staging_dir}&...
        self._connect_options = self._create_connect_args(url)
        return cast(Tuple[str], tuple()), self._connect_options

    def _create_connect_args(self, url: "URL") -> Dict[str, Any]:
        opts: Dict[str, Any] = {
            "aws_access_key_id": url.username if url.username else None,
            "aws_secret_access_key": url.password if url.password else None,
            "region_name": re.sub(
                r"^athena\.([a-z0-9-]+)\.amazonaws\.(com|com.cn)$", r"\1", url.host
            )
            if url.host
            else None,
            "schema_name": url.database if url.database else "default",
        }
        opts.update(url.query)
        if "verify" in opts:
            verify = opts["verify"]
            try:
                verify = bool(strtobool(verify))
            except ValueError:
                # Probably a file name of the CA cert bundle to use
                pass
            opts.update({"verify": verify})
        if "duration_seconds" in opts:
            opts.update({"duration_seconds": int(opts["duration_seconds"])})
        if "poll_interval" in opts:
            opts.update({"poll_interval": float(opts["poll_interval"])})
        if "kill_on_interrupt" in opts:
            opts.update({"kill_on_interrupt": bool(strtobool(opts["kill_on_interrupt"]))})
        if "result_reuse_enable" in opts:
            opts.update({"result_reuse_enable": bool(strtobool(opts["result_reuse_enable"]))})
        if "result_reuse_minutes" in opts:
            opts.update({"result_reuse_minutes": int(opts["result_reuse_minutes"])})
        return opts

    @reflection.cache
    def _get_schemas(self, connection, **kw):
        raw_connection = self._raw_connection(connection)
        catalog = raw_connection.catalog_name  # type: ignore
        with raw_connection.driver_connection.cursor() as cursor:  # type: ignore
            try:
                return cursor.list_databases(catalog)
            except pyathena.error.OperationalError as e:
                cause = e.__cause__
                if (
                    isinstance(cause, botocore.exceptions.ClientError)
                    and cause.response["Error"]["Code"] == "InvalidRequestException"
                ):
                    return []
                raise

    @reflection.cache
    def _get_table(self, connection, table_name: str, schema: Optional[str] = None, **kw):
        raw_connection = self._raw_connection(connection)
        schema = schema if schema else raw_connection.schema_name  # type: ignore
        with raw_connection.driver_connection.cursor() as cursor:  # type: ignore
            try:
                return cursor.get_table_metadata(table_name, schema_name=schema, logging_=False)
            except pyathena.error.OperationalError as e:
                cause = e.__cause__
                if (
                    isinstance(cause, botocore.exceptions.ClientError)
                    and cause.response["Error"]["Code"] == "MetadataException"
                ):
                    raise exc.NoSuchTableError(table_name) from e
                raise

    @reflection.cache
    def _get_tables(self, connection, schema: Optional[str] = None, **kw):
        raw_connection = self._raw_connection(connection)
        schema = schema if schema else raw_connection.schema_name  # type: ignore
        with raw_connection.driver_connection.cursor() as cursor:  # type: ignore
            return cursor.list_table_metadata(schema_name=schema)

    def get_schema_names(self, connection, **kw):
        schemas = self._get_schemas(connection, **kw)
        return [s.name for s in schemas]

    def get_table_names(self, connection: "Connection", schema: Optional[str] = None, **kw):
        # Tables created by Athena are always classified as `EXTERNAL_TABLE`,
        # but Athena can also query tables classified as `MANAGED_TABLE`.
        # Managed Tables are created by default when creating tables via Spark when
        # Glue has been enabled as the Hive Metastore for Elastic Map Reduce (EMR) clusters.
        # With Athena Federation, tables in the database that are connected to Athena via lambda
        # function, is classified as `EXTERNAL` and fully queryable
        tables = self._get_tables(connection, schema, **kw)
        return [
            t.name
            for t in tables
            if t.table_type in ["EXTERNAL_TABLE", "MANAGED_TABLE", "EXTERNAL"]
        ]

    def get_view_names(self, connection: "Connection", schema: Optional[str] = None, **kw):
        tables = self._get_tables(connection, schema, **kw)
        return [t.name for t in tables if t.table_type == "VIRTUAL_VIEW"]

    def get_table_comment(
        self, connection: "Connection", table_name: str, schema: Optional[str] = None, **kw
    ):
        metadata = self._get_table(connection, table_name, schema=schema, **kw)
        return {"text": metadata.comment}

    def get_table_options(
        self, connection: "Connection", table_name: str, schema: Optional[str] = None, **kw
    ):
        metadata = self._get_table(connection, table_name, schema=schema, **kw)
        # TODO The metadata retrieved from the API does not seem to include bucketing information.
        return {
            "awsathena_location": metadata.location,
            "awsathena_compression": metadata.compression,
            "awsathena_row_format": metadata.row_format,
            "awsathena_file_format": metadata.file_format,
            "awsathena_serdeproperties": _HashableDict(metadata.serde_properties),
            "awsathena_tblproperties": _HashableDict(metadata.table_properties),
        }

    def has_table(
        self, connection: "Connection", table_name: str, schema: Optional[str] = None, **kw
    ):
        try:
            columns = self.get_columns(connection, table_name, schema)
            return True if columns else False
        except exc.NoSuchTableError:
            return False

    @reflection.cache
    def get_view_definition(
        self, connection: Connection, view_name: str, schema: Optional[str] = None, **kw
    ):
        raw_connection = self._raw_connection(connection)
        schema = schema if schema else raw_connection.schema_name  # type: ignore
        query = f"""SHOW CREATE VIEW "{schema}"."{view_name}";"""
        try:
            res = connection.scalars(text(query))
        except exc.OperationalError as e:
            raise exc.NoSuchTableError(f"{schema}.{view_name}") from e
        else:
            return "\n".join([r for r in res])

    @reflection.cache
    def get_columns(
        self, connection: "Connection", table_name: str, schema: Optional[str] = None, **kw
    ):
        metadata = self._get_table(connection, table_name, schema=schema, **kw)
        columns = [
            {
                "name": c.name,
                "type": self._get_column_type(c.type),
                "nullable": True,
                "default": None,
                "autoincrement": False,
                "comment": c.comment,
                "dialect_options": {"awsathena_partition": None},
            }
            for c in metadata.columns
        ]
        columns += [
            {
                "name": c.name,
                "type": self._get_column_type(c.type),
                "nullable": True,
                "default": None,
                "autoincrement": False,
                "comment": c.comment,
                "dialect_options": {"awsathena_partition": True},
            }
            for c in metadata.partition_keys
        ]
        return columns

    def _get_column_type(self, type_: str):
        match = self._pattern_column_type.match(type_)
        if match:
            name = match.group(1).lower()
            length = match.group(2)
        else:
            name = type_.lower()
            length = None

        if name in self.ischema_names:
            col_type = self.ischema_names[name]
        else:
            util.warn(f"Did not recognize type '{type_}'")
            col_type = types.NullType

        args = []
        if length:
            if col_type is types.DECIMAL:
                precision, scale = length.split(",")
                args = [int(precision), int(scale)]
            elif col_type is types.CHAR or col_type is types.VARCHAR:
                args = [int(length)]

        return col_type(*args)

    def get_foreign_keys(
        self, connection: "Connection", table_name: str, schema: Optional[str] = None, **kw
    ) -> List["ReflectedForeignKeyConstraint"]:
        # Athena has no support for foreign keys.
        return []  # pragma: no cover

    def get_pk_constraint(
        self, connection: "Connection", table_name: str, schema: Optional[str] = None, **kw
    ) -> "ReflectedPrimaryKeyConstraint":
        # Athena has no support for primary keys.
        return {"name": None, "constrained_columns": []}  # pragma: no cover

    def get_indexes(
        self, connection: "Connection", table_name: str, schema: Optional[str] = None, **kw
    ) -> List["ReflectedIndex"]:
        # Athena has no support for indexes.
        return []  # pragma: no cover

    def do_rollback(self, dbapi_connection: "PoolProxiedConnection") -> None:
        # No transactions for Athena
        pass  # pragma: no cover

    def _check_unicode_returns(
        self, connection: "Connection", additional_tests: Optional[List[Any]] = None
    ) -> bool:
        # Requests gives back Unicode strings
        return True  # pragma: no cover

    def _check_unicode_description(self, connection: "Connection") -> bool:
        # Requests gives back Unicode strings
        return True  # pragma: no cover
