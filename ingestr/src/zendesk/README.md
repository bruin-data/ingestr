# Zendesk

Zendesk is a cloud-based customer service and support platform. It offers a range of features including ticket management, self-service options, knowledgebase management, live chat, customer analytics, and conversations.

This guide will allow you to set up a pipeline that can automatically load data from three possible Zendesk API clients *(Zendesk Support, Zendesk Chat, Zendesk Talk)* to a destination of your choice. For a complete list of the endpoints supported by these API clients, see *[settings.py](https://github.com/dlt-hub/verified-sources/blob/master/sources/zendesk/settings.py)* in the Zendesk verified source in the GitHub repository.

## Initialize the pipeline

```bash
dlt init zendesk bigquery
```
Here, we chose bigquery as the destination. Alternatively, you can also choose redshift, duckdb, or any of the other [destinations.](https://dlthub.com/docs/dlt-ecosystem/destinations/)

## Setup verified source

To grab the Zendesk credentials and initialise the verified source and pipeline, please refer to the [full documentation here.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/zendesk)

## Add the credentials

1. Add the credentials for the Zendesk API and your chosen destination in `.dlt/secrets.toml`.
    ```toml
     #Zendesk support credentials
    [sources.zendesk.zendesk_support.credentials]
    password = "set me up" # Include this if you want to authenticate using subdomain + email address + password
    subdomain = "subdomain" # Copy the subdomain from https://[subdomain].zendesk.com
    token = "set me up" # Include this if you want to authenticate using the API token
    email = "set me up" # Include this if you want to authenticate using subdomain + email + password
    oauth_token = "set me up" # Include this if you want to authenticate using an OAuth token

    # Zendesk chat credentials
    [sources.zendesk.zendesk_chat.credentials]
    subdomain = "subdomian # Copy the subdomain from the url https://[subdomain].zendesk.com
    oauth_token = "set me up" # Follow the steps in Zendesk Chat Credentials to get this token

    #Zendesk talk credentials
    [sources.zendesk.zendesk_talk.credentials]
    password = "set me up" # Include this if you want to authenticate using subdomain + email + password
    subdomain = "subdomain" # Copy the subdomain from the url https://[subdomain].zendesk.com
    token = "set me up" # Include this if you want to authenticate using the API token
    email = "set me up" # Include this if you want to authenticate using subdomain + email + password
    oauth_token = "set me up" # Include this if you want to authenticate using an OAuth token
    ```

2. Add only the credentials for the APIs you want to request data from and remove the rest.
3. Enter credentials for your chosen destination as per the [docs.](https://dlthub.com/docs/dlt-ecosystem/destinations/)

## Running the pipeline

1. Install the requirements for the pipeline by running the following command:
    ```bash
    pip install -r requirements.txt
    ```

2. Run the pipeline using the following command:
    ```bash
    python3 zendesk_pipeline.py
    ```

3. To make sure everything loads as expected, use the command:
    ```bash
    dlt pipeline <pipeline_name> show
    ```
    For example, the pipeline_name for the above pipeline example is `zendesk_pipeline`, you may also use any custom name instead.



💡 To explore additional customizations for this pipeline, we recommend referring to the official dlt Zendesk verified source documentation. It provides comprehensive information and guidance on how to further customize and tailor the pipeline to suit your specific needs. You can find the dlt Zendesk documentation in [Setup Guide: Zendesk.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/zendesk)
