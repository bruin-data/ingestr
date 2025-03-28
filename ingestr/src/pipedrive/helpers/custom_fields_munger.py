from typing import Any, Dict, Optional, TypedDict

import dlt

from ..typing import TDataPage


class TFieldMapping(TypedDict):
    name: str
    normalized_name: str
    options: Optional[Dict[str, str]]
    field_type: str


def update_fields_mapping(
    new_fields_mapping: TDataPage, existing_fields_mapping: Dict[str, Any]
) -> Dict[str, Any]:
    """
    Specific function to perform data munging and push changes to custom fields' mapping stored in dlt's state
    The endpoint must be an entity fields' endpoint
    """
    for data_item in new_fields_mapping:
        # 'edit_flag' field contains a boolean value, which is set to 'True' for custom fields and 'False' otherwise.
        if data_item.get("edit_flag"):
            # Regarding custom fields, 'key' field contains pipedrive's hash string representation of its name
            # We assume that pipedrive's hash strings are meant to be an univoque representation of custom fields' name, so dlt's state shouldn't be updated while those values
            # remain unchanged
            existing_fields_mapping = _update_field(data_item, existing_fields_mapping)
        # Built in enum and set fields are mapped if their options have int ids
        # Enum fields with bool and string key options are left intact
        elif data_item.get("field_type") in {"set", "enum"}:
            options = data_item.get("options", [])
            first_option = options[0]["id"] if len(options) >= 1 else None
            if isinstance(first_option, int) and not isinstance(first_option, bool):
                existing_fields_mapping = _update_field(
                    data_item, existing_fields_mapping
                )
    return existing_fields_mapping


def _update_field(
    data_item: Dict[str, Any],
    existing_fields_mapping: Optional[Dict[str, TFieldMapping]],
) -> Dict[str, TFieldMapping]:
    """Create or update the given field's info the custom fields state
    If the field hash already exists in the state from previous runs the name is not updated.
    New enum options (if any) are appended to the state.
    """
    existing_fields_mapping = existing_fields_mapping or {}
    key = data_item["key"]
    options = data_item.get("options", [])
    new_options_map = {str(o["id"]): o["label"] for o in options}
    existing_field = existing_fields_mapping.get(key)
    if not existing_field:
        existing_fields_mapping[key] = dict(
            name=data_item["name"],
            normalized_name=_normalized_name(data_item["name"]),
            options=new_options_map,
            field_type=data_item["field_type"],
        )
        return existing_fields_mapping
    existing_options = existing_field.get("options", {})
    if not existing_options or existing_options == new_options_map:
        existing_field["options"] = new_options_map
        existing_field["field_type"] = data_item[
            "field_type"
        ]  # Add for backwards compat
        return existing_fields_mapping
    # Add new enum options to the existing options array
    # so that when option is renamed the original label remains valid
    new_option_keys = set(new_options_map) - set(existing_options)
    for key in new_option_keys:
        existing_options[key] = new_options_map[key]
    existing_field["options"] = existing_options
    return existing_fields_mapping


def _normalized_name(name: str) -> str:
    source_schema = dlt.current.source_schema()
    normalized_name = name.strip()  # remove leading and trailing spaces
    return source_schema.naming.normalize_identifier(normalized_name)


def rename_fields(data: TDataPage, fields_mapping: Dict[str, Any]) -> TDataPage:
    if not fields_mapping:
        return data
    for data_item in data:
        for hash_string, field in fields_mapping.items():
            if hash_string not in data_item:
                continue
            field_value = data_item.pop(hash_string)
            field_name = field["name"]
            options_map = field["options"]
            # Get label instead of ID for 'enum' and 'set' fields
            if field_value and field["field_type"] == "set":  # Multiple choice
                field_value = [
                    options_map.get(str(enum_id), enum_id) for enum_id in field_value
                ]
            elif field_value and field["field_type"] == "enum":
                field_value = options_map.get(str(field_value), field_value)
            data_item[field_name] = field_value
    return data
