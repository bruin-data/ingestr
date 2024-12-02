# GitHub README.md

This `dlt` GitHub verified source, accesses the GitHub API from two `dlt` endpoints:

| endpoint | description |
| --- | --- |
| github_reactions | loads the issues and pullRequests from the repository |
| github_repo_events | loads the various repo_events from the repository like emoticons etc. |

## Initialize the pipeline

```bash
dlt init github duckdb
```

Here, we chose DuckDB as the destination. To choose a different destination, replace `duckdb` with your choice of [destination](https://dlthub.com/docs/dlt-ecosystem/destinations).

## Grab GitHub credentials & configure the verified source

To learn about grabbing the GitHub credentials and configuring the verified source, please refer to the [full documentation here.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/github)

## Add credentials

1. Open `.dlt/secrets.toml`

    ```toml
    # Put your secret values and credentials here
    # Note: Do not share this file and do not push it to GitHub!
    # Github access token (must be classic for reactions source)
    [sources.github]
    access_token="GITHUB_API_TOKEN"
    ```

2. Replace `"GITHUB_API_TOKEN"` with the API token with your actual token.
3. Follow the instructions in the [Destinations](https://dlthub.com/docs/dlt-ecosystem/destinations/) document to add credentials for your chosen destination.

## Run the pipeline

1. Install the necessary dependencies by running the following command:

    ```bash
    pip install -r requirements.txt
    ```

2. Now the pipeline can be run by using the command:

    ```bash
    python github_pipeline.py
    ```

3. To make sure that everything is loaded as expected, use the command:

    ```bash
    dlt pipeline github_reactions show
    ```



💡 To explore additional customizations for this pipeline, we recommend referring to the official `dlt` GitHub documentation. It provides comprehensive information and guidance on how to further customize and tailor the pipeline to suit your specific needs. You can find the `dlt` GitHub documentation in [Setup Guide: GitHub](https://dlthub.com/docs/dlt-ecosystem/verified-sources/github).

