"""This module's layout loosely follows example of SQLAlchemy's postgres dialect
"""

import decimal, re, datetime
from dateutil.parser import parse

import sqlalchemy
from sqlalchemy import types, event
from sqlalchemy.engine import default, Engine
from sqlalchemy.exc import DatabaseError, SQLAlchemyError
from sqlalchemy.engine import reflection

from databricks import sql


from databricks.sqlalchemy.dialect.base import (
    DatabricksDDLCompiler,
    DatabricksIdentifierPreparer,
)
from databricks.sqlalchemy.dialect.compiler import DatabricksTypeCompiler

try:
    import alembic
except ImportError:
    pass
else:
    from alembic.ddl import DefaultImpl

    class DatabricksImpl(DefaultImpl):
        __dialect__ = "databricks"


class DatabricksDecimal(types.TypeDecorator):
    """Translates strings to decimals"""

    impl = types.DECIMAL

    def process_result_value(self, value, dialect):
        if value is not None:
            return decimal.Decimal(value)
        else:
            return None


class DatabricksTimestamp(types.TypeDecorator):
    """Translates timestamp strings to datetime objects"""

    impl = types.TIMESTAMP

    def process_result_value(self, value, dialect):
        return value

    def adapt(self, impltype, **kwargs):
        return self.impl


class DatabricksDate(types.TypeDecorator):
    """Translates date strings to date objects"""

    impl = types.DATE

    def process_result_value(self, value, dialect):
        return value

    def adapt(self, impltype, **kwargs):
        return self.impl


class DatabricksDialect(default.DefaultDialect):
    """This dialect implements only those methods required to pass our e2e tests"""

    # Possible attributes are defined here: https://docs.sqlalchemy.org/en/14/core/internals.html#sqlalchemy.engine.Dialect
    name: str = "databricks"
    driver: str = "databricks-sql-python"
    default_schema_name: str = "default"

    preparer = DatabricksIdentifierPreparer  # type: ignore
    type_compiler = DatabricksTypeCompiler
    ddl_compiler = DatabricksDDLCompiler
    supports_statement_cache: bool = True
    supports_multivalues_insert: bool = True
    supports_native_decimal: bool = True
    supports_sane_rowcount: bool = False
    non_native_boolean_check_constraint: bool = False

    @classmethod
    def dbapi(cls):
        return sql

    def create_connect_args(self, url):
        # TODO: can schema be provided after HOST?
        # Expected URI format is: databricks+thrift://token:dapi***@***.cloud.databricks.com?http_path=/sql/***

        kwargs = {
            "server_hostname": url.host,
            "access_token": url.password,
            "http_path": url.query.get("http_path"),
            "catalog": url.query.get("catalog"),
            "schema": url.query.get("schema"),
        }

        self.schema = kwargs["schema"]
        self.catalog = kwargs["catalog"]

        return [], kwargs

    def get_columns(self, connection, table_name, schema=None, **kwargs):
        """Return information about columns in `table_name`.

        Given a :class:`_engine.Connection`, a string
        `table_name`, and an optional string `schema`, return column
        information as a list of dictionaries with these keys:

        name
          the column's name

        type
          [sqlalchemy.types#TypeEngine]

        nullable
          boolean

        default
          the column's default value

        autoincrement
          boolean

        sequence
          a dictionary of the form
              {'name' : str, 'start' :int, 'increment': int, 'minvalue': int,
               'maxvalue': int, 'nominvalue': bool, 'nomaxvalue': bool,
               'cycle': bool, 'cache': int, 'order': bool}

        Additional column attributes may be present.
        """

        _type_map = {
            "boolean": types.Boolean,
            "smallint": types.SmallInteger,
            "int": types.Integer,
            "bigint": types.BigInteger,
            "float": types.Float,
            "double": types.Float,
            "string": types.String,
            "varchar": types.String,
            "char": types.String,
            "binary": types.String,
            "array": types.String,
            "map": types.String,
            "struct": types.String,
            "uniontype": types.String,
            "decimal": DatabricksDecimal,
            "timestamp": DatabricksTimestamp,
            "date": DatabricksDate,
        }

        with self.get_connection_cursor(connection) as cur:
            resp = cur.columns(
                catalog_name=self.catalog,
                schema_name=schema or self.schema,
                table_name=table_name,
            ).fetchall()

        columns = []

        for col in resp:

            # Taken from PyHive. This removes added type info from decimals and maps
            _col_type = re.search(r"^\w+", col.TYPE_NAME).group(0)
            this_column = {
                "name": col.COLUMN_NAME,
                "type": _type_map[_col_type.lower()],
                "nullable": bool(col.NULLABLE),
                "default": col.COLUMN_DEF,
                "autoincrement": False if col.IS_AUTO_INCREMENT == "NO" else True,
            }
            columns.append(this_column)

        return columns

    def get_pk_constraint(self, connection, table_name, schema=None, **kw):
        """Return information about the primary key constraint on
        table_name`.

        Given a :class:`_engine.Connection`, a string
        `table_name`, and an optional string `schema`, return primary
        key information as a dictionary with these keys:

        constrained_columns
          a list of column names that make up the primary key

        name
          optional name of the primary key constraint.

        """
        # TODO: implement this behaviour
        return {"constrained_columns": []}

    def get_foreign_keys(self, connection, table_name, schema=None, **kw):
        """Return information about foreign_keys in `table_name`.

        Given a :class:`_engine.Connection`, a string
        `table_name`, and an optional string `schema`, return foreign
        key information as a list of dicts with these keys:

        name
          the constraint's name

        constrained_columns
          a list of column names that make up the foreign key

        referred_schema
          the name of the referred schema

        referred_table
          the name of the referred table

        referred_columns
          a list of column names in the referred table that correspond to
          constrained_columns
        """
        # TODO: Implement this behaviour
        return []

    def get_indexes(self, connection, table_name, schema=None, **kw):
        """Return information about indexes in `table_name`.

        Given a :class:`_engine.Connection`, a string
        `table_name` and an optional string `schema`, return index
        information as a list of dictionaries with these keys:

        name
          the index's name

        column_names
          list of column names in order

        unique
          boolean
        """
        # TODO: Implement this behaviour
        return []

    def get_table_names(self, connection, schema=None, **kwargs):
        TABLE_NAME = 1
        with self.get_connection_cursor(connection) as cur:
            sql_str = "SHOW TABLES FROM {}".format(
                ".".join([self.catalog, schema or self.schema])
            )
            data = cur.execute(sql_str).fetchall()
            _tables = [i[TABLE_NAME] for i in data]

        return _tables

    def get_view_names(self, connection, schema=None, **kwargs):
        VIEW_NAME = 1
        with self.get_connection_cursor(connection) as cur:
            sql_str = "SHOW VIEWS FROM {}".format(
                ".".join([self.catalog, schema or self.schema])
            )
            data = cur.execute(sql_str).fetchall()
            _tables = [i[VIEW_NAME] for i in data]

        return _tables

    def do_rollback(self, dbapi_connection):
        # Databricks SQL Does not support transactions
        pass

    def has_table(
        self, connection, table_name, schema=None, catalog=None, **kwargs
    ) -> bool:
        """SQLAlchemy docstrings say dialect providers must implement this method"""

        _schema = schema or self.schema
        _catalog = catalog or self.catalog

        # DBR >12.x uses underscores in error messages
        DBR_LTE_12_NOT_FOUND_STRING = "Table or view not found"
        DBR_GT_12_NOT_FOUND_STRING = "TABLE_OR_VIEW_NOT_FOUND"

        try:
            res = connection.execute(
                f"DESCRIBE TABLE {_catalog}.{_schema}.{table_name}"
            )
            return True
        except DatabaseError as e:
            if DBR_GT_12_NOT_FOUND_STRING in str(
                e
            ) or DBR_LTE_12_NOT_FOUND_STRING in str(e):
                return False
            else:
                raise e

    def get_connection_cursor(self, connection):
        """Added for backwards compatibility with 1.3.x"""
        if hasattr(connection, "_dbapi_connection"):
            return connection._dbapi_connection.dbapi_connection.cursor()
        elif hasattr(connection, "raw_connection"):
            return connection.raw_connection().cursor()
        elif hasattr(connection, "connection"):
            return connection.connection.cursor()

        raise SQLAlchemyError(
            "Databricks dialect can't obtain a cursor context manager from the dbapi"
        )

    @reflection.cache
    def get_schema_names(self, connection, **kw):
        # Equivalent to SHOW DATABASES

        # TODO: replace with call to cursor.schemas() once its performance matches raw SQL
        return [row[0] for row in connection.execute("SHOW SCHEMAS")]


@event.listens_for(Engine, "do_connect")
def receive_do_connect(dialect, conn_rec, cargs, cparams):
    """Helpful for DS on traffic from clients using SQLAlchemy in particular"""

    # Ignore connect invocations that don't use our dialect
    if not dialect.name == "databricks":
        return

    if "_user_agent_entry" in cparams:
        new_user_agent = f"sqlalchemy + {cparams['_user_agent_entry']}"
    else:
        new_user_agent = "sqlalchemy"

    cparams["_user_agent_entry"] = new_user_agent

    if sqlalchemy.__version__.startswith("1.3"):
        # SQLAlchemy 1.3.x fails to parse the http_path, catalog, and schema from our connection string
        # These should be passed in as connect_args when building the Engine

        if "schema" in cparams:
            dialect.schema = cparams["schema"]

        if "catalog" in cparams:
            dialect.catalog = cparams["catalog"]
