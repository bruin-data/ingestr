from datetime import date, datetime, timezone
from typing import Optional


def date_predicate(column: str, start_date: date, end_date: Optional[date]) -> str:
    """
    Generates a date predicate for the WHERE clause of a
    GAQL query.
    """
    if start_date is None:
        raise ValueError("start_date must be provided")

    if end_date is None:
        end_date = datetime.now(tz=timezone.utc).date()

    clauses = []
    if start_date is not None:
        clauses.append(f"""{column} >= '{start_date.strftime("%Y-%m-%d")}'""")

    if end_date is not None:
        clauses.append(f"""{column} <= '{end_date.strftime("%Y-%m-%d")}'""")

    return " AND ".join(clauses)
