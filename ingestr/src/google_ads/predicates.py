from typing import Optional
from datetime import datetime 

def date_predicate(column: str, start_date: Optional[datetime], end_date: Optional[datetime]) -> str:
    """
    Generates a date predicate for the WHERE clause of a
    GAQL query.
    """
    if start_date is None and end_date is None:
        raise ValueError("At least one of start_date or end_date must be provided.")
    
    clauses = []
    if start_date is not None:
        clauses.append(f"""{column} >= '{start_date.strftime("%Y-%m-%d")}'""")
    
    if end_date is not None:
        clauses.append(f"""{column} <= '{start_date.strftime("%Y-%m-%d")}'""")
    
    return " AND ".join(clauses)
    
