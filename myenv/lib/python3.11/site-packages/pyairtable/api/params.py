from typing import Any, Dict, List, Tuple


class InvalidParamException(ValueError):
    """
    Raised when invalid parameters are passed to ``all()``, ``first()``, etc.
    """


def dict_list_to_request_params(
    param_name: str,
    values: List[Dict[str, str]],
) -> Dict[str, str]:
    """
    Build the dict to be used by request params from dict list

    Expected Airtable Url Params is:
        `?sort[0][field]=FieldOne&sort[0][direction]=asc`

    >>> objects = [
    ...    { "field": "FieldOne", "direction": "asc"},
    ...    { "field": "FieldTwo", "direction": "desc"},
    ... ]
    >>> dict_list_to_request_params("sort", objects)
    {
        "sort[0][field]": "FieldOne",
        "sort[0][direction]: "asc",
        "sort[1][field]": "FieldTwo",
        "sort[1][direction]: "desc",
    }
    """
    return {
        key: value
        for (key, value) in sorted(
            (f"{param_name}[{index}][{key}]", value)
            for index, field_sort in enumerate(values)
            for key, value in field_sort.items()
        )
    }


def field_names_to_sorting_dict(field_names: List[str]) -> List[Dict[str, str]]:
    """
    >>> field_names_to_sorting_dict(["Name", "-Age"])
    [
        { "field": "FieldOne", "direction": "asc"},
        { "field": "FieldTwo", "direction": "desc"},
    ]
    """
    values = []

    for field_name in field_names:
        if field_name.startswith("-"):
            direction = "desc"
            field_name = field_name[1:]
        else:
            direction = "asc"

        sort_param = {"field": field_name, "direction": direction}
        values.append(sort_param)
    return values


#: Mapping of pyairtable option names to Airtable parameter names
OPTIONS_TO_PARAMETERS = {
    # list records
    "cell_format": "cellFormat",
    "fields": "fields",
    "formula": "filterByFormula",
    "max_records": "maxRecords",
    "offset": "offset",
    "page_size": "pageSize",
    "return_fields_by_field_id": "returnFieldsByFieldId",
    "sort": "sort",
    "time_zone": "timeZone",
    "user_locale": "userLocale",
    "view": "view",
    # get webhook payloads
    "limit": "limit",
    "cursor": "cursor",
}


def _option_to_param(name: str) -> str:
    try:
        return OPTIONS_TO_PARAMETERS[name]
    except KeyError:
        raise InvalidParamException(name)


#: List of option names that cannot be passed via POST, only GET
#: See https://github.com/gtalarico/pyairtable/pull/210#discussion_r1046014885
OPTIONS_NOT_SUPPORTED_VIA_POST = ("user_locale", "time_zone")


def options_to_params(options: Dict[str, Any]) -> Dict[str, Any]:
    """
    Convert Airtable options to a dict of query params.

    Args:
        options: A dict of Airtable-specific options. See :ref:`parameters`.

    Returns:
        A dict of query parameters that can be passed to the ``requests`` library.
    """
    params = {_option_to_param(name): value for (name, value) in options.items()}

    if "fields" in params:
        params["fields[]"] = params.pop("fields")
    if "returnFieldsByFieldId" in params:
        params["returnFieldsByFieldId"] = int(params["returnFieldsByFieldId"])
    if "sort" in params:
        sorting_dict_list = field_names_to_sorting_dict(params.pop("sort"))
        params.update(dict_list_to_request_params("sort", sorting_dict_list))

    return params


def options_to_json_and_params(
    options: Dict[str, Any]
) -> Tuple[Dict[str, Any], Dict[str, Any]]:
    """
    Convert Airtable options to a JSON payload with (possibly) leftover query params.

    Args:
        options: A dict of Airtable-specific options. See :ref:`parameters`.

    Returns:
        A 2-tuple that contains the POST data and the non-POSTable query parameters.
    """
    json = {
        _option_to_param(name): value
        for (name, value) in options.items()
        if name not in OPTIONS_NOT_SUPPORTED_VIA_POST
    }
    params = {
        _option_to_param(name): value
        for (name, value) in options.items()
        if name in OPTIONS_NOT_SUPPORTED_VIA_POST
    }

    if "returnFieldsByFieldId" in json:
        json["returnFieldsByFieldId"] = bool(json["returnFieldsByFieldId"])
    if "sort" in json:
        json["sort"] = field_names_to_sorting_dict(json.pop("sort"))

    return (json, params)
