"""Alembic dialect."""

from __future__ import annotations

from typing import TYPE_CHECKING, Any

from alembic.ddl.base import (
    AddColumn,
    ColumnDefault,
    ColumnName,
    ColumnNullable,
    ColumnType,
    DropColumn,
    RenameTable,
    alter_table,
    format_column_name,
    format_server_default,
    format_table_name,
    format_type,
)
from alembic.ddl.impl import DefaultImpl
from sqlalchemy import ForeignKeyConstraint
from sqlalchemy.ext.compiler import compiles

if TYPE_CHECKING:
    from sqlalchemy_hana.dialect import HANADDLCompiler


class HANAImpl(DefaultImpl):
    """Alembic implementation for SAP HANA."""

    __dialect__ = "hana"
    transactional_ddl = True
    type_synonyms = DefaultImpl.type_synonyms + ({"FLOAT", "DOUBLE"},)

    def start_migrations(self) -> None:
        # Activate transactional DDL statements
        self.execute("SET TRANSACTION AUTOCOMMIT DDL OFF")

    def correct_for_autogen_foreignkeys(
        self,
        conn_fks: set[ForeignKeyConstraint],
        metadata_fks: set[ForeignKeyConstraint],
    ) -> None:
        def _correct(fk: ForeignKeyConstraint) -> None:
            fk.ondelete = "RESTRICT" if fk.ondelete is None else fk.ondelete.upper()
            fk.onupdate = "RESTRICT" if fk.onupdate is None else fk.onupdate.upper()

        for fk in conn_fks:
            _correct(fk)
        for fk in metadata_fks:
            _correct(fk)


@compiles(AddColumn, "hana")
def visit_add_column(element: AddColumn, compiler: HANADDLCompiler, **kw: Any) -> str:
    """Generate SQL statement to add column to existing table."""
    table = alter_table(compiler, element.table_name, element.schema)
    column = compiler.get_column_specification(element.column, **kw)
    return f"{table} ADD ({column})"


@compiles(DropColumn, "hana")
def visit_drop_column(element: DropColumn, compiler: HANADDLCompiler, **kw: Any) -> str:
    """Generate SQL statement to remove column from existing table."""
    table = alter_table(compiler, element.table_name, element.schema)
    column = format_column_name(compiler, element.column.name)
    return f"{table} DROP ({column})"


@compiles(ColumnName, "hana")
def visit_rename_column(element: ColumnName, compiler: HANADDLCompiler) -> str:
    """Generate SQL statement to rename an existing column."""
    table = format_table_name(compiler, element.table_name, element.schema)
    column = format_column_name(compiler, element.column_name)
    new_name = format_column_name(compiler, element.newname)
    return f"RENAME COLUMN {table}.{column} TO {new_name}"


@compiles(ColumnType, "hana")
def visit_column_type(element: ColumnType, compiler: HANADDLCompiler) -> str:
    """Generate SQL statement to adjust type of an existing column."""
    table = alter_table(compiler, element.table_name, element.schema)
    column = format_column_name(compiler, element.column_name)
    new_type = format_type(compiler, element.type_)
    return f"{table} ALTER ({column} {new_type})"


@compiles(ColumnNullable, "hana")
def visit_column_nullable(element: ColumnNullable, compiler: HANADDLCompiler) -> str:
    """Generate SQL statement to make a column nullable or not."""
    assert element.existing_type, "Cannot change nullable without existing type"

    table = alter_table(compiler, element.table_name, element.schema)
    column = format_column_name(compiler, element.column_name)
    type_ = format_type(compiler, element.existing_type)
    null = "NULL" if element.nullable else "NOT NULL"
    return f"{table} ALTER ({column} {type_} {null})"


@compiles(ColumnDefault, "hana")
def visit_column_default(element: ColumnDefault, compiler: HANADDLCompiler) -> str:
    """Generate SQL statement to column default."""
    assert element.existing_type, "Cannot change default without existing type"

    table = alter_table(compiler, element.table_name, element.schema)
    column = format_column_name(compiler, element.column_name)
    type_ = format_type(compiler, element.existing_type)
    default = (
        format_server_default(compiler, element.default)
        if element.default is not None
        else "NULL"
    )
    return f"{table} ALTER ({column} {type_} DEFAULT {default})"


@compiles(RenameTable, "hana")
def visit_rename_table(element: RenameTable, compiler: HANADDLCompiler) -> str:
    """Generate SQL to rename a table."""
    old_table = format_table_name(compiler, element.table_name, element.schema)
    new_table = format_table_name(compiler, element.new_table_name, element.schema)
    return f"RENAME TABLE {old_table} TO {new_table}"
