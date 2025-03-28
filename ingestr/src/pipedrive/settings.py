"""Pipedrive source settings and constants"""

ENTITY_MAPPINGS = [
    ("activity", "activityFields", {"user_id": 0}),
    ("organization", "organizationFields", None),
    ("person", "personFields", None),
    ("product", "productFields", None),
    ("deal", "dealFields", None),
    ("pipeline", None, None),
    ("stage", None, None),
    ("user", None, None),
]

RECENTS_ENTITIES = {
    "activity": "activities",
    "activityType": "activity_types",
    "deal": "deals",
    "file": "files",
    "filter": "filters",
    "note": "notes",
    "person": "persons",
    "organization": "organizations",
    "pipeline": "pipelines",
    "product": "products",
    "stage": "stages",
    "user": "users",
}
