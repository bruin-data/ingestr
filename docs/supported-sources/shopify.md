# Shopify
[Shopify](https://www.shopify.com/) is a comprehensive e-commerce platform that enables individuals and businesses to create online stores.

ingestr supports Shopify as a source.

## URI format
The URI format for Shopify is as follows:

```plaintext
shopify://<shopify store URL>?api_key=token
```

URI parameters:
- `shopify store URL`: the Shopify store domain, e.g. `myawesomestore.myshopify.com`
- `api_key`: the **Admin API access token** from your Shopify app. The query parameter is named `api_key` for compatibility, but its value is not the app's API key.

For this token-based connection, these are the only required values: the store domain and its Admin API access token. You do not need to provide the app's client ID or client secret.

The URI is used to connect to the Shopify API for extracting data. More details on setting up Shopify integrations can be found [here](https://shopify.dev/docs/admin-api/getting-started).

## Setting up a Shopify Integration

To use the Shopify API, create a custom app in the Shopify Dev Dashboard and install it in your store.

### Step 1: Create or Select an App

1. Go to the [Shopify Dev Dashboard](https://dev.shopify.com/dashboard)
2. Select an existing app or create a new one

### Step 2: Configure API Scopes

In the app configuration, make sure the app has read scopes for the data you want to ingest:
- `read_products`
- `read_customers`
- `read_orders`
- `read_inventory`
- `read_locations`

After changing scopes:
1. Create a new app version
2. Release the new app version

### Step 3: Install the App in Your Store

1. Open the Shopify store admin: `https://admin.shopify.com/store/your-store-name`
2. Go to **Settings** → **Apps and sales channels**
3. Find and open your app
4. Install or reinstall the app so the new scopes become active

### Step 4: Get the Admin API Access Token

1. After installation, go back to **Apps and sales channels**
2. Click on your app
3. Click **API credentials**
4. Copy the **Admin API access token**

> **Important**: The access token is displayed only once. Copy and store it securely.

Once you have the Admin API access token and your store name (e.g. `my-store.myshopify.com`), you can connect. Example: if the access token is stored in `SHOPIFY_ADMIN_API_ACCESS_TOKEN` and your store is `my-store`, the command below will copy Shopify data into DuckDB:

```sh
export SHOPIFY_ADMIN_API_ACCESS_TOKEN=your_admin_api_access_token
ingestr ingest --source-uri "shopify://my-store.myshopify.com?api_key=${SHOPIFY_ADMIN_API_ACCESS_TOKEN}" --source-table "orders" --dest-uri "duckdb:///shopify.duckdb" --dest-table "dest.orders"
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
| [events](https://shopify.dev/api/admin-rest/2023-10/resources/event) | id | created_at | merge | Retrieves Shopify event data for audit trails and activity tracking |
| [price_rules](https://shopify.dev/api/admin-rest/2023-10/resources/pricerule) | id | updated_at | merge | **DEPRECATED** - Use `discounts` table instead |

Use these as `--source-table` parameter in the `ingestr ingest` command.
