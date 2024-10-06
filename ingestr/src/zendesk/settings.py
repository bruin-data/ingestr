"""Zendesk source settings and constants"""

from dlt.common import pendulum

DEFAULT_START_DATE = pendulum.datetime(year=2024, month=10, day=3)

INCREMENTAL_PAGE_SIZE = 1000
PAGE_SIZE = 100


CUSTOM_FIELDS_STATE_KEY = "ticket_custom_fields_v2"

# Tuples of (Resource name, endpoint URL, data_key, supports pagination)
# data_key is the key which data list is nested under in responses
# if the data key is None it is assumed to be the same as the resource name
# The last element of the tuple says if endpoint supports cursor pagination
SUPPORT_ENDPOINTS = [
    ("users", "/api/v2/users.json", "users", True),
    ("sla_policies", "/api/v2/slas/policies.json", None, False),
    ("groups", "/api/v2/groups.json", None, True),
    ("organizations", "/api/v2/organizations.json", None, True),
    ("brands", "/api/v2/brands.json", None, True),
]

SUPPORT_EXTRA_ENDPOINTS = [
    ("activities", "/api/v2/activities.json", None, True),
    ("automations", "/api/v2/automations.json", None, True),
    ("macros", "/api/v2/macros.json", None, True),
    ("recipient_addresses", "/api/v2/recipient_addresses.json", None, True),
    ("requests", "/api/v2/requests.json", None, True),
    ("targets", "/api/v2/targets.json", None, False),
    ("ticket_forms", "/api/v2/ticket_forms.json", None, False),
    ("ticket_metrics", "/api/v2/ticket_metrics.json", None, True),
    ("triggers", "/api/v2/triggers.json", None, True),
    ("user_fields", "/api/v2/user_fields.json", None, True),
]

TALK_ENDPOINTS = [
    ("calls", "/api/v2/channels/voice/calls", None, False),
    ("addresses", "/api/v2/channels/voice/addresses", None, False),
    ("greetings", "/api/v2/channels/voice/greetings", None, False),
    ("phone_numbers", "/api/v2/channels/voice/phone_numbers", None, False),
    ("settings", "/api/v2/channels/voice/settings", None, False),
    ("lines", "/api/v2/channels/voice/lines", None, False),
    ("agents_activity", "/api/v2/channels/voice/stats/agents_activity", None, False),
    (
        "current_queue_activity",
        "/api/v2/channels/voice/stats/current_queue_activity",
        None,
        False,
    ),
]

INCREMENTAL_TALK_ENDPOINTS = {
    "calls": "/api/v2/channels/voice/stats/incremental/calls.json",
    "legs": "/api/v2/channels/voice/stats/incremental/legs.json",
}
