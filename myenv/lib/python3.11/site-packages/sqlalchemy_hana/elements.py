"""Custom SQL elements for SAP HANA."""

from __future__ import annotations

from typing import TYPE_CHECKING, Any

from sqlalchemy import table as table_clause
from sqlalchemy.sql.ddl import DDLElement
from sqlalchemy.sql.dml import DMLWhereBase, Insert
from sqlalchemy.sql.selectable import Select, TableClause

if TYPE_CHECKING:
    AnySelect = Select[Any]


class CreateView(DDLElement):
    """CREATE VIEW element for SAP HANA."""

    __visit_name__ = "create_view"

    def __init__(self, name: str, selectable: AnySelect):
        self.name = name
        self.selectable = selectable


class DropView(DDLElement):
    """DROP VIEW element for SAP HANA."""

    __visit_name__ = "drop_view"

    def __init__(self, name: str):
        self.name = name


def view(name: str, selectable: AnySelect) -> TableClause:
    """Helper function to create a view clause element."""
    clause = table_clause(name)
    clause._columns._populate_separate_keys(
        col._make_proxy(clause) for col in selectable.selected_columns
    )
    return clause


class Upsert(Insert, DMLWhereBase):  # type: ignore[misc]
    __visit_name__ = "upsert"
    # until https://github.com/sqlalchemy/sqlalchemy/issues/8321 is implemented we don't cache
    inherit_cache = False

    @property
    def _effective_plugin_target(self) -> str:
        # somewhere in the internal logic of sqlalchemy an error occurs if this is not set
        return "insert"


def upsert(table: Any) -> Upsert:
    return Upsert(table)


__all__ = ("CreateView", "DropView", "Upsert", "upsert", "view")
