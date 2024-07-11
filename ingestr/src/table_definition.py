from dataclasses import dataclass


@dataclass
class TableDefinition:
    dataset: str
    table: str


def table_string_to_dataclass(table: str) -> TableDefinition:
    table_fields = table.split(".", 1)
    if len(table_fields) != 2:
        raise ValueError("Table name must be in the format <schema>.<table>")

    return TableDefinition(dataset=table_fields[0], table=table_fields[1])
