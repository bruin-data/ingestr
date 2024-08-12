# Stripe
Stripe is an online payment company that provides a platform for businesses to process payments from customers over the internet.  This verified source uses Stripe's API and `dlt` to extract key data such as customer information, subscription details, event records, etc. and then load it into a database.

This verified source loads data from the following default endpoints:

| Endpoint | Description |
| --- | --- |
| Subscription | recurring payment model offered by the Stripe payment platform. |
| Account | the entities that represent businesses or individuals using the Stripe platform to accept payments. |
| Coupon | promotional codes that businesses can create and offer to customers to provide discounts or other special offers. |
| Customer | individuals or businesses who make purchases or transactions with a business using the Stripe platform. |
| Product | specific item or service that a business offers for sale. |
| Price | represents the specific cost or pricing information associated with a product or subscription plan. |
| Event | a record or notification of a significant occurrence or activity that takes place within a Stripe account. |
| Invoice | a document that represents a request for payment from a customer. |
| BalanceTransaction | represents a record of funds movement within a Stripe account. |

> Please note that the endpoints within the verified source can be tailored to meet your specific requirements, as outlined in the Stripe API reference documentation. Detailed instructions on customizing these endpoints can be found in the customization section [here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/stripe#customization).

## Initialize the pipeline
```bash
dlt init stripe duckdb
```

Here, we chose BigQuery as the destination. Alternatively, you can also choose redshift, duckdb, or any of the other [destinations.](https://dlthub.com/docs/dlt-ecosystem/destinations/)

## Setup verified source
To get the full list of supported endpoints, grab API credentials and initialise the verified source and pipeline example, read the [full documentation here.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/stripe)

## Add credential
1. Open `.dlt/secrets.toml`.
2. Replace the value of **stripe_secret_key** with the one you copied. This will ensure that this source can securely access your Stripe resources.
    ```toml
    # put your secret values and credentials here. do not share this file and do not upload it to github.
    [sources.stripe_analytics]
    stripe_secret_key = "stripe_secret_key"# Please set me up!
    ```

3. Enter credentials for your chosen destination as per the [docs.](https://dlthub.com/docs/dlt-ecosystem/destinations/)

## Running the pipeline example
1. Install the required dependencies by running the following command:
    ```bash
    pip install -r requirements.txt
    ```

2. Now you can build the verified source by using the command:
    ```bash
    python stripe_analytics_pipeline.py
    ```

3. To ensure that everything loads as expected, use the command:
    ```bash
    dlt pipeline <pipeline_name> show
    ```
    For example, the pipeline_name for the above pipeline example is `stripe_analytics`, you can use any custom name instead.


💡 To explore additional customizations for this pipeline, we recommend referring to the official dlt Stripe verified documentation. It provides comprehensive information and guidance on how to further customize and tailor the pipeline to suit your specific needs. You can find the dlt Stripe documentation in [Setup Guide: Stripe](https://dlthub.com/docs/dlt-ecosystem/verified-sources/stripe).

