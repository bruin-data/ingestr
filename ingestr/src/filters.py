from dlt.common.libs.sql_alchemy import Table


def cast_set_to_list(row):
    # this handles just the sqlalchemy backend for now
    if isinstance(row, dict):
        for key in row.keys():
            if isinstance(row[key], set):
                row[key] = list(row[key])
    return row


def table_adapter_exclude_columns(cols: list[str]):
    def excluder(table: Table):
        cols_to_remove = [col for col in table._columns if col.name in cols]  # type: ignore
        for col in cols_to_remove:
            table._columns.remove(col)  # type: ignore

    return excluder
