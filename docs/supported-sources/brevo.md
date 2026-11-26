# Brevo

[Brevo](https://www.brevo.com/) (formerly Sendinblue) is a marketing platform for email, SMS, and transactional messaging.

ingestr supports Brevo as a source.

## URI format

The URI format for Brevo is as follows:

```plaintext
brevo://?api_key=<api-key>
```

URI parameters:

- `api_key`: A Brevo API key (v3).

## Setting up a Brevo Integration

To get an API key:

1. Log in to your Brevo account.
2. Open **SMTP & API** → **API Keys** in your account settings.
3. Click **Generate a new API key** and copy it.

Once you have the key, here's a sample command that copies Brevo contacts into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'brevo://?api_key=your_api_key' \
  --source-table 'contacts' \
  --dest-uri duckdb:///brevo.duckdb \
  --dest-table 'brevo.contacts'
```

## Tables

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| [contacts](https://developers.brevo.com/reference/get-contacts) | id | modifiedAt | merge | All contacts with their attributes, list memberships, and blocklist flags. Incremental runs only fetch contacts modified since the interval start. |
| [lists](https://developers.brevo.com/reference/get-lists) | id | - | replace | Contact lists. |
| [segments](https://developers.brevo.com/reference/get-segments) | id | - | replace | Contact segments. |
| [folders](https://developers.brevo.com/reference/get-folders) | id | - | replace | Folders that group contact lists. |
| [contact_attributes](https://developers.brevo.com/reference/get-attributes) | category, name | - | replace | Contact attribute definitions, including the values of category attributes. |
| [email_campaigns](https://developers.brevo.com/reference/get-email-campaigns) | id | - | replace | Email campaigns of every status, including archived ones (`status` is `archive`), with their global statistics. HTML content is excluded. |
| [sms_campaigns](https://developers.brevo.com/reference/get-sms-campaigns) | id | - | replace | SMS campaigns of every status, including archived ones. |
| [email_templates](https://developers.brevo.com/reference/get-smtp-templates) | id | - | replace | Transactional email templates. |
| [transactional_email_events](https://developers.brevo.com/reference/get-email-event-report) | - | event_date | delete+insert | One row per transactional email event (request, delivered, opened, click, bounce, and so on). `event_date` is the day of the event's `date`. |
| [transactional_email_reports](https://developers.brevo.com/reference/get-smtp-report) | - | date | delete+insert | Daily totals for transactional email. |
| [blocked_contacts](https://developers.brevo.com/reference/get-transac-blocked-contacts) | email | - | replace | Contacts blocked or unsubscribed from transactional email, with the reason. |
| [companies](https://developers.brevo.com/reference/get-all-companies) | id | modifiedAt | merge | CRM companies with their attributes and linked contacts and deals. `modifiedAt` is copied from the `last_updated_at` attribute. |
| [deals](https://developers.brevo.com/reference/get-all-deals) | id | modifiedAt | merge | CRM deals with their attributes and links. `modifiedAt` is copied from the `last_updated_date` attribute. |
| [crm_tasks](https://developers.brevo.com/reference/get-all-tasks) | id | - | replace | CRM tasks. |
| [crm_notes](https://developers.brevo.com/reference/get-all-notes) | id | - | replace | CRM notes. |
| [orders](https://developers.brevo.com/reference/get-orders) | id | updatedAt | merge | Ecommerce orders. Brevo keeps every update of an order; the table keeps the latest one. |
| [products](https://developers.brevo.com/reference/get-products) | id | modifiedAt | merge | Ecommerce products, including deleted ones (`isDeleted` is true). |
| [categories](https://developers.brevo.com/reference/get-categories) | id | modifiedAt | merge | Ecommerce categories, including deleted ones (`isDeleted` is true). |
| [sms_events](https://developers.brevo.com/reference/get-sms-events) | - | event_date | delete+insert | One row per transactional SMS event. `event_date` is the day of the event's `date`. |
| [whatsapp_events](https://developers.brevo.com/reference/get-whatsapp-event-report) | - | event_date | delete+insert | One row per WhatsApp event. `event_date` is the day of the event's `date`. |
| [whatsapp_campaigns](https://developers.brevo.com/reference/get-whats-app-campaigns) | id | - | replace | WhatsApp campaigns. |
| [custom_events](https://developers.brevo.com/reference/get-events) | - | event_date | delete+insert | Custom events tracked for contacts, with their properties. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

## Notes

- `transactional_email_events`, `transactional_email_reports`, `sms_events` and `whatsapp_events` are loaded in whole days. Each run replaces the days from `--interval-start` through `--interval-end`, inclusive. Without an interval, they load the last 90 days.
- `event_date` is the event's day in the timezone set in your Brevo account.
- Brevo returns `createdAt` and `modifiedAt` of email templates in Central European Time but marks them as UTC, so they can be an hour or two ahead.
- A full refresh of the event and report tables without an interval keeps only the last 90 days, and of `custom_events` only the last 6 months.
- The ecommerce tables need ecommerce enabled in Brevo, and the SMS and WhatsApp tables need those channels on your plan.
- Deleted contacts are not returned by Brevo, so they stay in the destination `contacts` table.
