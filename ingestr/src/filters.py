def cast_set_to_list(row):
    # this handles just the sqlalchemy backend for now
    if isinstance(row, dict):
        for key in row.keys():
            if isinstance(row[key], set):
                row[key] = list(row[key])
    return row


def handle_mysql_empty_dates(row):
    # MySQL returns empty dates as 0000-00-00, which is not a valid date, we handle them here.
    if not isinstance(row, dict):
        return row

    for key in row.keys():
        if not isinstance(row[key], str):
            continue

        if row[key] == "0000-00-00":
            from datetime import date

            row[key] = date(1970, 1, 1)

        elif row[key] == "0000-00-00 00:00:00":
            from datetime import datetime

            row[key] = datetime(1970, 1, 1, 0, 0, 0)
    return row


def table_adapter_exclude_columns(cols: list[str]):
    from dlt.common.libs.sql_alchemy import Table

    def excluder(table: Table):
        cols_to_remove = [col for col in table._columns if col.name in cols]  # type: ignore
        for col in cols_to_remove:
            table._columns.remove(col)  # type: ignore

    return excluder
