# Gorgias
[Gorgias](https://www.gorgias.com/) is a helpdesk for e-commerce merchants, providing customer service via email, social media, SMS, and live chat.

ingestr supports Gorgias as a source.

## URI format
The URI format for Gorgias is as follows:

```plaintext
gorgias://<domain>?api_key=<api-key>&email=<email>
```

URI parameters:
- `domain`: the domain of the Gorgias account without the full `gorgias.com`, e.g. `mycompany`
- `api_key`: the integration token used for authentication with the Gorgias API
- `email`: the email address of the user to connect to the Gorgias API

The URI is used to connect to the Gorgias API for extracting data.

## Examples
```bash
# get all the tickets that are created/updated since 2024-06-19 and write them to `gorgias.ticket_messages` table on BigQuery
ingestr ingest --source-table 'tickets' --source-uri $GORGIAS_URI --dest-uri $BIGQUERY_URI --interval-start 2024-06-19  --dest-table 'gorgias.ticket_messages' --loader-file-format jsonl

# get all the customers and write them to `gorgias.customers` table on DuckDB
ingestr ingest --source-table 'customers' --source-uri $GORGIAS_URI --dest-uri duckdb:///gorgias.duckdb --interval-start 2024-01-01  --dest-table 'dest.customers'
```

## Supported entities
The Gorgias source supports a growing list of entities, feel free to create an issue if you need more entities to be supported.

### Customers
Customers are the users who have interacted with the support team. Each customer has a unique ID and contains information such as the name and email.

You can retrieve customers by using `customers` as the source table.

### Tickets
Tickets are the main entity in Gorgias, representing customer inquiries. Each ticket has a unique ID and contains information such as the customer, status, and messages.

You can retrieve tickets by using `tickets` as the source table.

### Ticket messages
Ticket messages are the messages exchanged between the customer and the support agent in a ticket. Each message has a unique ID and contains information such as the sender, content, and timestamp.

You can retrieve ticket messages by using `ticket_messages` as the source table.

### Satisfaction surveys
Satisfaction surveys are sent to customers after a ticket is resolved to gather feedback on their experience. Each survey has a unique ID and contains information such as the rating and comments.

You can retrieve satisfaction surveys by using `satisfaction_surveys` as the source table.



