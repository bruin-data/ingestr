def cast_set_to_list(row):
    # this handles just the sqlalchemy backend for now
    if isinstance(row, dict):
        for key in row.keys():
            if isinstance(row[key], set):
                row[key] = list(row[key])
    return row
