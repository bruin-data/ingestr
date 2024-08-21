"""Hubspot source settings and constants"""

from dlt.common import pendulum

STARTDATE = pendulum.datetime(year=2000, month=1, day=1)

CRM_CONTACTS_ENDPOINT = (
    "/crm/v3/objects/contacts?associations=deals,products,tickets,quotes"
)
CRM_COMPANIES_ENDPOINT = (
    "/crm/v3/objects/companies?associations=contacts,deals,products,tickets,quotes"
)
CRM_DEALS_ENDPOINT = "/crm/v3/objects/deals"
CRM_PRODUCTS_ENDPOINT = "/crm/v3/objects/products"
CRM_TICKETS_ENDPOINT = "/crm/v3/objects/tickets"
CRM_QUOTES_ENDPOINT = "/crm/v3/objects/quotes"

CRM_OBJECT_ENDPOINTS = {
    "contact": CRM_CONTACTS_ENDPOINT,
    "company": CRM_COMPANIES_ENDPOINT,
    "deal": CRM_DEALS_ENDPOINT,
    "product": CRM_PRODUCTS_ENDPOINT,
    "ticket": CRM_TICKETS_ENDPOINT,
    "quote": CRM_QUOTES_ENDPOINT,
}

WEB_ANALYTICS_EVENTS_ENDPOINT = "/events/v3/events?objectType={objectType}&objectId={objectId}&occurredAfter={occurredAfter}&occurredBefore={occurredBefore}&sort=-occurredAt"

OBJECT_TYPE_SINGULAR = {
    "companies": "company",
    "contacts": "contact",
    "deals": "deal",
    "tickets": "ticket",
    "products": "product",
    "quotes": "quote",
}

OBJECT_TYPE_PLURAL = {v: k for k, v in OBJECT_TYPE_SINGULAR.items()}

DEFAULT_DEAL_PROPS = [
    "amount",
    "closedate",
    "createdate",
    "dealname",
    "dealstage",
    "hs_lastmodifieddate",
    "hs_object_id",
    "pipeline",
]

DEFAULT_COMPANY_PROPS = [
    "createdate",
    "domain",
    "hs_lastmodifieddate",
    "hs_object_id",
    "name",
]

DEFAULT_CONTACT_PROPS = [
    "createdate",
    "email",
    "firstname",
    "hs_object_id",
    "lastmodifieddate",
    "lastname",
]

DEFAULT_TICKET_PROPS = [
    "createdate",
    "content",
    "hs_lastmodifieddate",
    "hs_object_id",
    "hs_pipeline",
    "hs_pipeline_stage",
    "hs_ticket_category",
    "hs_ticket_priority",
    "subject",
]

DEFAULT_PRODUCT_PROPS = [
    "createdate",
    "description",
    "hs_lastmodifieddate",
    "hs_object_id",
    "name",
    "price",
]

DEFAULT_QUOTE_PROPS = [
    "hs_createdate",
    "hs_expiration_date",
    "hs_lastmodifieddate",
    "hs_object_id",
    "hs_public_url_key",
    "hs_status",
    "hs_title",
]

ALL = ("ALL",)
