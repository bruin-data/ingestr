# Freshdesk

> **Warning!**
>
> This source is a Community source and was tested only once. Currently, we **don't test** it on a regular basis.
> If you have any problem with this source, ask for help in our [Slack Community](https://dlthub.com/community).

This verified source enables data loading from the Freshdesk API to your preferred destination. It supports loading data from various endpoints, providing flexibility in the data you can retrieve.

Resources that can be loaded using this verified source are:
| S.No. | Name      | Description                                                                               |
| ----- | --------- | ----------------------------------------------------------------------------------------- |
| 1.    | agents    |  Users responsible for managing and resolving customer inquiries and support tickets.     |
| 2.    | companies |  Customer organizations or groups that agents support.                                    |
| 3.    | contacts  |  Individuals or customers who reach out for support.                                      |
| 4.    | groups    |  Agents organized based on specific criteria.                                             |
| 5.    | roles     |  Predefined sets of permissions that determine what actions an agent can perform.         |
| 6.    | tickets   |  Customer inquiries or issues submitted via various channels like email, chat, phone, etc. |

## Initialize the pipeline with Freshdesk source
```bash
dlt init freshdesk duckdb
```

Here, we chose DuckDB as the destination. Alternatively, you can choose redshift, bigquery, or any other [destinations](https://dlthub.com/docs/dlt-ecosystem/destinations/).

## Grab Freshdesk credentials

To grab the Freshdesk credentials, log in and open "Profile Settings" from the profile icon. Grab the API key displayed on the right side.

## Add credential

1. Open ".dlt/secrets.toml". Enter the API key:
    ```toml
    [sources.freshdesk]
    api_secret_key = "api_key" # please set me up!
    ```

2. In the ".dlt/config.toml". Enter the domain:
   ```toml
   [sources.freshdesk]
   domain = "your_freshdesk_domain" # please set me up!
   ```

3. Enter credentials for your chosen destination as per the [docs](https://dlthub.com/docs/dlt-ecosystem/destinations/).

## Run the pipeline

1. Before running the pipeline, ensure that you have installed all the necessary dependencies by running the command:

    ```bash
    pip install -r requirements.txt

    ```
2. You're now ready to run the pipeline! To get started, run the following command:

    ```bash
    python freshdesk_pipeline.py

    ```
3. Once the pipeline has finished running, you can verify that everything loaded correctly by using the following command:

    ```bash
    dlt pipeline <pipeline_name> show
    ```

    Note that in the above command, replace `<pipeline_name>` with the name of your pipeline. For example, if you named your pipeline "freshdesk_pipeline" you would run:

    ```bash
    dlt pipeline freshdesk_pipeline show
    ```
