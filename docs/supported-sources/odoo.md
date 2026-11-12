# Odoo
[Odoo](https://www.odoo.com/) is an open-source suite of business apps covering CRM, sales, purchasing, inventory, accounting, projects, and HR.

ingestr supports Odoo as a source. It works with Odoo Online, Odoo.sh, and self-hosted Odoo servers.

## URI format

```plaintext
odoo://<host>?database=<database>&username=<username>&api_key=<api_key>
```

URI parameters:
- `host`: the address of your Odoo server, such as `mycompany.odoo.com` or `erp.example.com:8069`.
- `api_key`: an API key of the Odoo user to read as. On servers without API keys, the user's password works here too.
- `database`: the Odoo database name. Required on Odoo 18 and older; on Odoo 19 and newer it is only needed when one server hosts several databases.
- `username`: the login of the Odoo user. Required on Odoo 18 and older.
- `scheme`: `https` (default) or `http` for servers without TLS.

ingestr uses the JSON-2 API on Odoo 19 and newer, and the JSON-RPC API on older versions.

## Setting up an Odoo integration

1. Log in to Odoo as the user that ingestr should read as. That user's access rights decide which records and fields ingestr can see.
2. Open your avatar menu, then **Preferences** (or **My Profile**) → **Account Security**.
3. Click **New API Key**, give it a name, and copy the key.

The external API is only available on Odoo Online's Custom plan; it is always available on self-hosted servers and Odoo.sh.

Here's a sample command that copies sale orders into a DuckDB database:

```bash
ingestr ingest \
  --source-uri 'odoo://mycompany.odoo.com?database=mycompany&username=me@example.com&api_key=key_123' \
  --source-table 'sale_orders' \
  --dest-uri duckdb:///odoo.duckdb \
  --dest-table 'odoo.sale_orders'
```

## Tables

Every table has the primary key `id` and uses the `merge` strategy keyed on `write_date`, Odoo's last-modified timestamp. Archived records are included.

| Table | Odoo model | Details |
| --- | --- | --- |
| `partners` | `res.partner` | Customers, vendors, and contacts |
| `partner_categories` | `res.partner.category` | Contact tags |
| `users` | `res.users` | Users |
| `companies` | `res.company` | Companies |
| `currencies` | `res.currency` | Currencies |
| `currency_rates` | `res.currency.rate` | Currency exchange rates |
| `countries` | `res.country` | Countries |
| `product_categories` | `product.category` | Product categories |
| `product_templates` | `product.template` | Product templates |
| `products` | `product.product` | Product variants |
| `uoms` | `uom.uom` | Units of measure |
| `pricelists` | `product.pricelist` | Pricelists |
| `sale_orders` | `sale.order` | Quotations and sales orders |
| `sale_order_lines` | `sale.order.line` | Sales order lines |
| `purchase_orders` | `purchase.order` | Requests for quotation and purchase orders |
| `purchase_order_lines` | `purchase.order.line` | Purchase order lines |
| `crm_leads` | `crm.lead` | Leads and opportunities |
| `crm_stages` | `crm.stage` | CRM pipeline stages |
| `crm_teams` | `crm.team` | Sales teams |
| `account_moves` | `account.move` | Invoices, bills, and journal entries |
| `account_move_lines` | `account.move.line` | Journal items |
| `account_payments` | `account.payment` | Payments |
| `accounts` | `account.account` | Chart of accounts |
| `journals` | `account.journal` | Accounting journals |
| `taxes` | `account.tax` | Taxes |
| `payment_terms` | `account.payment.term` | Payment terms |
| `analytic_lines` | `account.analytic.line` | Analytic lines and timesheets |
| `stock_pickings` | `stock.picking` | Transfers (receipts, deliveries) |
| `stock_moves` | `stock.move` | Stock moves |
| `stock_move_lines` | `stock.move.line` | Detailed stock move lines |
| `stock_quants` | `stock.quant` | On-hand quantities per location |
| `stock_locations` | `stock.location` | Stock locations |
| `warehouses` | `stock.warehouse` | Warehouses |
| `projects` | `project.project` | Projects |
| `project_tasks` | `project.task` | Project tasks |
| `project_task_stages` | `project.task.type` | Task stages |
| `employees` | `hr.employee` | Employees |
| `departments` | `hr.department` | Departments |
| `jobs` | `hr.job` | Job positions |
| `activities` | `mail.activity` | Scheduled activities |
| `messages` | `mail.message` | Chatter messages and notes |

A table is only available when the Odoo app that provides it is installed, such as Sales for `sale_orders` or Inventory for `stock_pickings`.

Any other model, including custom and Studio models, can be read by passing its technical name as the table, such as `--source-table 'fleet.vehicle'` or `--source-table 'x_vehicles'`.

Each table contains the model's stored fields. Many-to-one fields hold the ID of the related record, many-to-many fields hold a JSON array of IDs, and one-to-many and binary (attachment and image) fields are left out. Timestamps are in UTC.

Records deleted in Odoo are not removed from the destination by incremental runs; use `--full-refresh` to rebuild a table.
