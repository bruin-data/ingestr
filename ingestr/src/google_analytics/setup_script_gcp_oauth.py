"""
This script will help you obtain an OAuth token from your GCP account with access to GA4. Alternatively service account credentials can be used (see docs)
This script will receive client_id and client_secret to produce an OAuth refresh_token which is then saved in secrets.toml along with client credentials.

Before running this script you must:
1. Ensure your email used for the GCP account has access to the GA4 property.
2. Open a gcp project in your GCP account.
3. Enable the Analytics API in the project
4. Search credentials in the search bar and go to Credentials
5. Create credentials -> OAuth client ID -> Select Desktop App from Application type and give a name to the client.
6. Download the credentials and fill client_id, client_secret and project_id in secrets.toml
7. Go back to credentials and select OAuth consent screen in the left
8. Fill in App name, user support email(your email), authorized domain (localhost.com), dev contact info (your email again)
9. Add the following scope: “https://www.googleapis.com/auth/analytics.readonly”
10. Add your own email as a test user."""

import dlt
from dlt.common.configuration.exceptions import ConfigFieldMissingException
from dlt.common.configuration.inject import with_config
from dlt.sources.credentials import GcpOAuthCredentials


@with_config(sections=("sources", "google_analytics"))
def print_refresh_token(credentials: GcpOAuthCredentials = dlt.secrets.value) -> None:
    """
    Will get client_id, client_secret and project_id from secrets.toml and then will print the refresh token.
    """
    credentials.auth("https://www.googleapis.com/auth/analytics.readonly")
    print("Add to secrets.toml")
    print(f"refresh_token: {credentials.refresh_token}")
    # print(f"Access token: {credentials.token}")


if __name__ == "__main__":
    print(
        """
Before running this script you must:
1. Ensure your email used for the GCP account has access to the GA4 property.
2. Open a gcp project in your GCP account.
3. Enable the Analytics API in the project
4. Search credentials in the search bar and go to Credentials
5. Create credentials -> OAuth client ID -> Select Desktop App from Application type and give a name to the client.
6. Download the credentials and fill client_id, client_secret and project_id in secrets.toml
7. Go back to credentials and select OAuth consent screen in the left
8. Fill in App name, user support email(your email), authorized domain (localhost.com), dev contact info (your email again)
9. Add the following scope: “https://www.googleapis.com/auth/analytics.readonly”
10. Add your own email as a test user."""
    )
    try:
        print_refresh_token()
    except ConfigFieldMissingException:
        print(
            "*****\nMissing secrets! Make sure you added client_id, client_secret and project_id to secrets.toml or environment variables. See details below\n*****"
        )
        raise
