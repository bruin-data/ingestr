"""SQL database source helpers"""

import operator
from typing import (
    Any,
    Callable,
    Dict,
    Iterator,
    List,
    Literal,
    Optional,
    Union,
)

import dlt
from dlt.common.configuration.specs import BaseConfiguration, configspec
from dlt.common.exceptions import MissingDependencyException
from dlt.common.schema import TTableSchemaColumns
from dlt.common.typing import TDataItem
from dlt.sources.credentials import ConnectionStringCredentials
from sqlalchemy import Table, create_engine
from sqlalchemy.engine import Engine

from ingestr.src.sql_database.override import IngestrConnectionStringCredentials

from .schema_types import (
    SelectAny,
    row_tuples_to_arrow,
    table_to_columns,
)

TableBackend = Literal["sqlalchemy", "pyarrow", "pandas", "connectorx"]


class TableLoader:
    def __init__(
        self,
        engine: Engine,
        backend: TableBackend,
        table: Table,
        columns: TTableSchemaColumns,
        chunk_size: int = 1000,
        incremental: Optional[dlt.sources.incremental[Any]] = None,
    ) -> None:
        self.engine = engine
        self.backend = backend
        self.table = table
        self.columns = columns
        self.chunk_size = chunk_size
        self.incremental = incremental
        if incremental:
            try:
                self.cursor_column = table.c[incremental.cursor_path]
            except KeyError as e:
                raise KeyError(
                    f"Cursor column '{incremental.cursor_path}' does not exist in table '{table.name}'"
                ) from e
            self.last_value = incremental.last_value
            self.end_value = incremental.end_value
            self.row_order = getattr(self.incremental, "row_order", None)
        else:
            self.cursor_column = None
            self.last_value = None
            self.end_value = None
            self.row_order = None

    def make_query(self) -> SelectAny:
        table = self.table
        query = table.select()
        if not self.incremental:
            return query
        last_value_func = self.incremental.last_value_func

        # generate where
        if (
            last_value_func is max
        ):  # Query ordered and filtered according to last_value function
            filter_op = operator.ge
            filter_op_end = operator.lt
        elif last_value_func is min:
            filter_op = operator.le
            filter_op_end = operator.gt
        else:  # Custom last_value, load everything and let incremental handle filtering
            return query

        if self.last_value is not None:
            query = query.where(filter_op(self.cursor_column, self.last_value))
            if self.end_value is not None:
                query = query.where(filter_op_end(self.cursor_column, self.end_value))

        # generate order by from declared row order
        order_by = None
        if self.row_order == "asc":
            order_by = self.cursor_column.asc()
        elif self.row_order == "desc":
            order_by = self.cursor_column.desc()
        if order_by is not None:
            query = query.order_by(order_by)

        return query

    def load_rows(self, backend_kwargs: Dict[str, Any] = None) -> Iterator[TDataItem]:
        # make copy of kwargs
        backend_kwargs = dict(backend_kwargs or {})
        query = self.make_query()
        if self.backend == "connectorx":
            yield from self._load_rows_connectorx(query, backend_kwargs)
        else:
            yield from self._load_rows(query, backend_kwargs)

    def _load_rows(self, query: SelectAny, backend_kwargs: Dict[str, Any]) -> TDataItem:
        with self.engine.connect() as conn:
            result = conn.execution_options(yield_per=self.chunk_size).execute(query)
            # NOTE: cursor returns not normalized column names! may be quite useful in case of Oracle dialect
            # that normalizes columns
            # columns = [c[0] for c in result.cursor.description]
            columns = list(result.keys())
            for partition in result.partitions(size=self.chunk_size):
                if self.backend == "sqlalchemy":
                    yield [dict(row._mapping) for row in partition]
                elif self.backend == "pandas":
                    from dlt.common.libs.pandas_sql import _wrap_result

                    yield _wrap_result(
                        partition,
                        columns,
                        **{"dtype_backend": "pyarrow", **backend_kwargs},
                    )
                elif self.backend == "pyarrow":
                    yield row_tuples_to_arrow(
                        partition, self.columns, tz=backend_kwargs.get("tz")
                    )

    def _load_rows_connectorx(
        self, query: SelectAny, backend_kwargs: Dict[str, Any]
    ) -> Iterator[TDataItem]:
        try:
            import connectorx as cx  # type: ignore
        except ImportError:
            raise MissingDependencyException(
                "Connector X table backend", ["connectorx"]
            )

        # default settings
        backend_kwargs = {
            "return_type": "arrow2",
            "protocol": "binary",
            **backend_kwargs,
        }
        conn = backend_kwargs.pop(
            "conn",
            self.engine.url._replace(
                drivername=self.engine.url.get_backend_name()
            ).render_as_string(hide_password=False),
        )
        df = cx.read_sql(
            conn,
            str(query.compile(self.engine, compile_kwargs={"literal_binds": True})),
            **backend_kwargs,
        )
        yield df


def table_rows(
    engine: Engine,
    table: Table,
    chunk_size: int,
    backend: TableBackend,
    incremental: Optional[dlt.sources.incremental[Any]] = None,
    detect_precision_hints: bool = False,
    defer_table_reflect: bool = False,
    table_adapter_callback: Callable[[Table], None] = None,
    backend_kwargs: Dict[str, Any] = None,
) -> Iterator[TDataItem]:
    columns: TTableSchemaColumns = None
    if defer_table_reflect:
        table = Table(
            table.name, table.metadata, autoload_with=engine, extend_existing=True
        )
        if table_adapter_callback:
            table_adapter_callback(table)
        columns = table_to_columns(table, detect_precision_hints)

        # set the primary_key in the incremental
        if incremental and incremental.primary_key is None:
            primary_key = get_primary_key(table)
            if primary_key is not None:
                incremental.primary_key = primary_key
        # yield empty record to set hints
        yield dlt.mark.with_hints(
            [],
            dlt.mark.make_hints(
                primary_key=get_primary_key(table),
                columns=columns,
            ),
        )
    else:
        # table was already reflected
        columns = table_to_columns(table, detect_precision_hints)

    loader = TableLoader(
        engine, backend, table, columns, incremental=incremental, chunk_size=chunk_size
    )
    yield from loader.load_rows(backend_kwargs)


def engine_from_credentials(
    credentials: Union[ConnectionStringCredentials, Engine, str],
) -> Engine:
    if isinstance(credentials, Engine):
        return credentials
    if isinstance(credentials, ConnectionStringCredentials):
        credentials = credentials.to_native_representation()
    return create_engine(credentials)


def get_primary_key(table: Table) -> List[str]:
    """Create primary key or return None if no key defined"""
    primary_key = [c.name for c in table.primary_key]
    return primary_key if len(primary_key) > 0 else None


def unwrap_json_connector_x(field: str) -> TDataItem:
    """Creates a transform function to be added with `add_map` that will unwrap JSON columns
    ingested via connectorx. Such columns are additionally quoted and translate SQL NULL to json "null"
    """
    import pyarrow as pa
    import pyarrow.compute as pc

    def _unwrap(table: TDataItem) -> TDataItem:
        col_index = table.column_names.index(field)
        # remove quotes
        column = pc.replace_substring_regex(table[field], '"(.*)"', "\\1")
        # convert json null to null
        column = pc.replace_with_mask(
            column,
            pc.equal(column, "null").combine_chunks(),
            pa.scalar(None, pa.large_string()),
        )
        return table.set_column(col_index, table.schema.field(col_index), column)

    return _unwrap


@configspec
class SqlDatabaseTableConfiguration(BaseConfiguration):
    incremental: Optional[dlt.sources.incremental] = None  # type: ignore[type-arg]


@configspec
class SqlTableResourceConfiguration(BaseConfiguration):
    credentials: IngestrConnectionStringCredentials = None
    table: str = None
    incremental: Optional[dlt.sources.incremental] = None  # type: ignore[type-arg]
    schema: Optional[str] = None


__source_name__ = "sql_database"
