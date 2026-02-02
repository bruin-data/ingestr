# Shopify
[Shopify](https://www.shopify.com/) is a comprehensive e-commerce platform that enables individuals and businesses to create online stores.

ingestr supports Shopify as a source.

## URI format
The URI format for Shopify is as follows:

```plaintext
shopify://<shopify store URL>?api_key=token
```

URI parameters:
- `shopify store URI`: the URL of the Shopify store you'd like to connect to, e.g. `myawesomestore.myshopify.com`
- `api_key`: the API key used for authentication with the Shopify API

The URI is used to connect to the Shopify API for extracting data. More details on setting up Shopify integrations can be found [here](https://shopify.dev/docs/admin-api/getting-started).

## Setting up a Shopify Integration

To use the Shopify API, you must create and install a custom app in your store. API credentials are generated as part of this process.

Steps to get your API credentials:
1) Open Shopify admin: `https://admin.shopify.com/store/your-store-name`
2) **Settings** → **Apps and sales channels**
3) **Develop apps** (top-right). If prompted, enable custom app development.
4) If you see **Legacy custom apps**, open it; otherwise click **Create an app**.
5) Name the app (e.g., "My Integration") and select yourself as the app developer.
6) **Configuration** → **Admin API access scopes**: grant only the permissions you need (e.g., `read_products`, `read_orders`, `write_products`).
7) **Install app** and confirm.
8) Open **API credentials** and copy:
   - **API key**
   - **API secret key**
   - **Admin API access token** (typically used to authenticate API requests)

Important: The access token is displayed only once. Copy and store it securely.

Once you complete these steps, you will have the API key and your store name (e.g. `my-store.myshopify.com`) to connect. Example: if your API key is stored in `SHOPIFY_API_KEY` and your store is `my-store`, the command below will copy Shopify data into DuckDB:

```sh
SHOPIFY_API_KEY=your_api_key \
ingestr ingest --source-uri "shopify://my-store.myshopify.com?api_key=${SHOPIFY_API_KEY}" --source-table "orders" --dest-uri "duckdb:///shopify.duckdb" --dest-table "dest.orders"
```

The result of this command will be a table in the `shopify.duckdb` database with JSON columns.

## Tables
Shopify source allows ingesting the following sources into separate tables:
| Table | PK | Inc Key | Inc Strategy | Details |
|-------|----|---------|--------------|---------|
| [orders](https://shopify.dev/api/admin-rest/2023-10/resources/order) | id | updated_at | merge | Retrieves Shopify order data including customer info, line items, and shipping details |
| [customers](https://shopify.dev/api/admin-rest/2023-10/resources/customer) | id | updated_at | merge | Retrieves Shopify customer data including contact info and order history |
| [discounts](https://shopify.dev/docs/api/admin-graphql/2024-07/queries/discountNodes) | id | updated_at | merge | Retrieves Shopify discount data using GraphQL API (use instead of deprecated price_rules) |
| [products](https://shopify.dev/api/admin-rest/2023-10/resources/product) | id | updated_at | merge | Retrieves Shopify product information including variants, images, and inventory |
| [inventory_items](https://shopify.dev/api/admin-rest/2023-10/resources/inventoryitem) | id | updated_at | merge | Retrieves Shopify inventory item details and stock levels |
| [transactions](https://shopify.dev/api/admin-rest/2023-10/resources/transaction) | id | id | merge | Retrieves Shopify transaction data for payments and refunds |
| [balance](https://shopify.dev/api/admin-rest/2023-10/resources/balance) | currency | - | merge | Retrieves Shopify balance information for financial tracking |
| [events](https://shopify.dev/api/admin-rest/2023-10/resources/event) | id | created_at |merge| Retrieves Shopify event data for audit trails and activity tracking |
| [price_rules](https://shopify.dev/api/admin-rest/2023-10/resources/pricerule) | id | updated_at | merge | **DEPRECATED** - Use `discounts` table instead |

Use these as `--source-table` parameter in the `ingestr ingest` command.
