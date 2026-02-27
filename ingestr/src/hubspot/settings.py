# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Hubspot source settings and constants"""

from dlt.common import pendulum

STARTDATE = pendulum.datetime(year=2000, month=1, day=1)

CRM_CONTACTS_ENDPOINT = (
    "/crm/v3/objects/contacts?associations=companies,deals,products,tickets,quotes"
)
CRM_COMPANIES_ENDPOINT = "/crm/v3/objects/companies?associations=products"
CRM_DEALS_ENDPOINT = (
    "/crm/v3/objects/deals?associations=companies,contacts,products,tickets,quotes"
)
CRM_PRODUCTS_ENDPOINT = (
    "/crm/v3/objects/products?associations=companies,contacts,deals,tickets,quotes"
)
CRM_TICKETS_ENDPOINT = (
    "/crm/v3/objects/tickets?associations=companies,contacts,deals,products,quotes"
)
CRM_QUOTES_ENDPOINT = (
    "/crm/v3/objects/quotes?associations=companies,contacts,deals,products,tickets"
)
CRM_CALLS_ENDPOINT = (
    "/crm/v3/objects/calls?associations=contacts,companies,deals,products,quotes"
)
CRM_EMAILS_ENDPOINT = (
    "/crm/v3/objects/emails?associations=contacts,companies,deals,products,quotes"
)
CRM_FEEDBACK_SUBMISSIONS_ENDPOINT = (
    "/crm/v3/objects/feedback_submissions?associations=contacts,companies,deals,products,quotes"
)
CRM_LINE_ITEMS_ENDPOINT = (
    "/crm/v3/objects/line_items?associations=contacts,companies,deals,products,quotes"
)
CRM_MEETINGS_ENDPOINT = (
    "/crm/v3/objects/meetings?associations=contacts,companies,deals,products,quotes"
)
CRM_NOTES_ENDPOINT = (
    "/crm/v3/objects/notes?associations=contacts,companies,deals,products,quotes"
)
CRM_TASKS_ENDPOINT = (
    "/crm/v3/objects/tasks?associations=contacts,companies,deals,products,quotes"
)
CRM_CARTS_ENDPOINT = (
    "/crm/v3/objects/carts?associations=contacts,companies,deals,products,quotes"
)
CRM_DISCOUNTS_ENDPOINT = (
    "/crm/v3/objects/discounts?associations=contacts,line_items,companies,deals,products,quotes"
)
CRM_FEES_ENDPOINT = (
    "/crm/v3/objects/fees?associations=contacts,line_items,companies,deals,products,quotes"
)
CRM_INVOICES_ENDPOINT = (
    "/crm/v3/objects/invoices?associations=contacts,line_items,companies,fees,products,quotes"
)
CRM_COMMERCE_PAYMENTS_ENDPOINT = (
    "/crm/v3/objects/commerce_payments?associations=contacts,companies,deals,quotes,invoices,products,fees"
)
CRM_TAXES_ENDPOINT = (
    "/crm/v3/objects/tax?associations=line_items,companies,deals,products,quotes,fees"
)
CRM_OWNERS_ENDPOINT = "/crm/v3/owners"
CRM_SCHEMAS_ENDPOINT = "/crm/v3/schemas"

CRM_OBJECT_ENDPOINTS = {
    "contact": CRM_CONTACTS_ENDPOINT,
    "company": CRM_COMPANIES_ENDPOINT,
    "deal": CRM_DEALS_ENDPOINT,
    "product": CRM_PRODUCTS_ENDPOINT,
    "ticket": CRM_TICKETS_ENDPOINT,
    "quote": CRM_QUOTES_ENDPOINT,
    "call": CRM_CALLS_ENDPOINT,
    "email": CRM_EMAILS_ENDPOINT,
    "feedback_submission": CRM_FEEDBACK_SUBMISSIONS_ENDPOINT,
    "line_item": CRM_LINE_ITEMS_ENDPOINT,
    "meeting": CRM_MEETINGS_ENDPOINT,
    "note": CRM_NOTES_ENDPOINT,
    "task": CRM_TASKS_ENDPOINT,
    "cart": CRM_CARTS_ENDPOINT,
    "discount": CRM_DISCOUNTS_ENDPOINT,
    "fee": CRM_FEES_ENDPOINT,
    "invoice": CRM_INVOICES_ENDPOINT,
    "commerce_payment": CRM_COMMERCE_PAYMENTS_ENDPOINT,
    "tax": CRM_TAXES_ENDPOINT,
}

WEB_ANALYTICS_EVENTS_ENDPOINT = "/events/v3/events?objectType={objectType}&objectId={objectId}&occurredAfter={occurredAfter}&occurredBefore={occurredBefore}&sort=-occurredAt"

OBJECT_TYPE_SINGULAR = {
    "companies": "company",
    "contacts": "contact",
    "deals": "deal",
    "tickets": "ticket",
    "products": "product",
    "quotes": "quote",
    "calls": "call",
    "emails": "email",
    "feedback_submissions": "feedback_submission",
    "line_items": "line_item",
    "meetings": "meeting",
    "notes": "note",
    "tasks": "task",
    "carts": "cart",
    "discounts": "discount",
    "fees": "fee",
    "invoices": "invoice",
    "commerce_payments": "commerce_payment",
    "taxes": "tax",
}

OBJECT_TYPE_PLURAL = {v: k for k, v in OBJECT_TYPE_SINGULAR.items()}

# Contacts use "lastmodifieddate"; all other CRM objects use "hs_lastmodifieddate"
LAST_MODIFIED_PROPERTY = {
    "contact": "lastmodifieddate",
}
DEFAULT_LAST_MODIFIED_PROPERTY = "hs_lastmodifieddate"

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

DEFAULT_CALL_PROPS = [
    "hs_call_body",
    "hs_call_direction",
    "hs_call_disposition",
    "hs_call_duration",
    "hs_call_from_number",
    "hs_call_status",
    "hs_call_title",
    "hs_call_to_number",
    "hs_lastmodifieddate",
    "hs_timestamp",
]

DEFAULT_EMAIL_PROPS = [
    "hs_attachment_ids",
    "hs_email_direction",
    "hs_email_headers",
    "hs_email_html",
    "hs_email_status",
    "hs_email_subject",
    "hs_email_text",
    "hs_timestamp",
    "hs_lastmodifieddate",
    "hubspot_owner_id",
]

DEFAULT_FEEDBACK_SUBMISSION_PROPS = [
    "hs_createdate",
    "hs_lastmodifieddate",
    "hs_object_id",
    "hs_sentiment",
    "hs_survey_channel",
]

DEFAULT_LINE_ITEM_PROPS = [
    "amount",
    "description",
    "hs_line_item_currency_code",
    "hs_recurring_billing_end_date",
    "hs_recurring_billing_start_date",
    "hs_lastmodifieddate",
    "hs_sku",
    "name",
    "price",
    "quantity",
    "recurringbillingfrequency",
]

DEFAULT_MEETING_PROPS = [
    "hs_internal_meeting_notes",
    "hs_meeting_body",
    "hs_meeting_end_time",
    "hs_meeting_external_url",
    "hs_meeting_location",
    "hs_meeting_outcome",
    "hs_meeting_start_time",
    "hs_meeting_title",
    "hs_timestamp",
    "hs_lastmodifieddate",
    "hubspot_owner_id",
]

DEFAULT_NOTE_PROPS = [
    "hs_attachment_ids",
    "hs_note_body",
    "hs_timestamp",
    "hs_lastmodifieddate",
    "hubspot_owner_id",
]

DEFAULT_TASK_PROPS = [
    "hs_task_body",
    "hs_task_priority",
    "hs_task_status",
    "hs_task_subject",
    "hs_task_type",
    "hs_timestamp",
    "hs_lastmodifieddate",
    "hubspot_owner_id",
]

DEFAULT_CART_PROPS = [
    "hs_cart_discount",
    "hs_cart_name",
    "hs_cart_url",
    "hs_createdate",
    "hs_currency_code",
    "hs_external_cart_id",
    "hs_external_status",
    "hs_lastmodifieddate",
    "hs_object_id",
    "hs_shipping_cost",
    "hs_source_store",
    "hs_tags",
    "hs_tax",
    "hs_total_price",
]

DEFAULT_DISCOUNT_PROPS = [
    "hs_duration",
    "hs_label",
    "hs_lastmodifieddate",
    "hs_sort_order",
    "hs_type",
    "hs_value",
]

DEFAULT_FEE_PROPS = [
    "hs_label",
    "hs_lastmodifieddate",
    "hs_type",
    "hs_value",
]

DEFAULT_INVOICE_PROPS = [
    "hs_currency",
    "hs_due_date",
    "hs_invoice_date",
    "hs_lastmodifieddate",
    "hs_tax_id",
]

DEFAULT_COMMERCE_PAYMENT_PROPS = [
    "hs_currency_code",
    "hs_customer_email",
    "hs_fees_amount",
    "hs_initial_amount",
    "hs_initiated_date",
    "hs_internal_comment",
    "hs_lastmodifieddate",
    "hs_latest_status",
    "hs_payment_method_type",
    "hs_payout_date",
    "hs_processor_type",
    "hs_reference_number",
    "hs_refunds_amount",
    "hs_billing_address_city",
    "hs_billing_address_country",
]

DEFAULT_TAX_PROPS = [
    "hs_label",
    "hs_lastmodifieddate",
    "hs_type",
    "hs_value",
]

ALL = ("ALL",)
