import base64
import hashlib
import json
import logging
import secrets
import webbrowser
from datetime import datetime, timezone
from http.server import HTTPServer
from typing import List

import oauthlib.oauth2
import requests
from oauthlib.oauth2.rfc6749.errors import OAuth2Error
from requests.exceptions import RequestException

from databricks.sql.auth.oauth_http_handler import OAuthHttpSingleRequestHandler
from databricks.sql.auth.endpoint import OAuthEndpointCollection

logger = logging.getLogger(__name__)


class IgnoreNetrcAuth(requests.auth.AuthBase):
    """This auth method is a no-op.

    We use it to force requestslib to not use .netrc to write auth headers
    when making .post() requests to the oauth token endpoints, since these
    don't require authentication.

    In cases where .netrc is outdated or corrupt, these requests will fail.

    See issue #121
    """

    def __call__(self, r):
        return r


class OAuthManager:
    def __init__(
        self,
        port_range: List[int],
        client_id: str,
        idp_endpoint: OAuthEndpointCollection,
    ):
        self.port_range = port_range
        self.client_id = client_id
        self.redirect_port = None
        self.idp_endpoint = idp_endpoint

    @staticmethod
    def __token_urlsafe(nbytes=32):
        return secrets.token_urlsafe(nbytes)

    @staticmethod
    def __get_redirect_url(redirect_port: int):
        return f"http://localhost:{redirect_port}"

    def __fetch_well_known_config(self, hostname: str):
        known_config_url = self.idp_endpoint.get_openid_config_url(hostname)

        try:
            response = requests.get(url=known_config_url, auth=IgnoreNetrcAuth())
        except RequestException as e:
            logger.error(
                f"Unable to fetch OAuth configuration from {known_config_url}.\n"
                "Verify it is a valid workspace URL and that OAuth is "
                "enabled on this account."
            )
            raise e

        if response.status_code != 200:
            msg = (
                f"Received status {response.status_code} OAuth configuration from "
                f"{known_config_url}.\n Verify it is a valid workspace URL and "
                "that OAuth is enabled on this account."
            )
            logger.error(msg)
            raise RuntimeError(msg)
        try:
            return response.json()
        except requests.exceptions.JSONDecodeError as e:
            logger.error(
                f"Unable to decode OAuth configuration from {known_config_url}.\n"
                "Verify it is a valid workspace URL and that OAuth is "
                "enabled on this account."
            )
            raise e

    @staticmethod
    def __get_challenge():
        verifier_string = OAuthManager.__token_urlsafe(32)
        digest = hashlib.sha256(verifier_string.encode("UTF-8")).digest()
        challenge_string = (
            base64.urlsafe_b64encode(digest).decode("UTF-8").replace("=", "")
        )
        return verifier_string, challenge_string

    def __get_authorization_code(self, client, auth_url, scope, state, challenge):
        handler = OAuthHttpSingleRequestHandler("Databricks Sql Connector")

        last_error = None
        for port in self.port_range:
            try:
                with HTTPServer(("", port), handler) as httpd:
                    redirect_url = OAuthManager.__get_redirect_url(port)
                    (auth_req_uri, _, _) = client.prepare_authorization_request(
                        authorization_url=auth_url,
                        redirect_url=redirect_url,
                        scope=scope,
                        state=state,
                        code_challenge=challenge,
                        code_challenge_method="S256",
                    )
                    logger.info(f"Opening {auth_req_uri}")

                    webbrowser.open_new(auth_req_uri)
                    logger.info(
                        f"Listening for OAuth authorization callback at {redirect_url}"
                    )
                    httpd.handle_request()
                self.redirect_port = port
                break
            except OSError as e:
                if e.errno == 48:
                    logger.info(f"Port {port} is in use")
                    last_error = e
            except Exception as e:
                logger.error("unexpected error", e)
        if self.redirect_port is None:
            logger.error(
                f"Tried all the ports {self.port_range} for oauth redirect, but can't find free port"
            )
            raise last_error

        if not handler.request_path:
            msg = f"No path parameters were returned to the callback at {redirect_url}"
            logger.error(msg)
            raise RuntimeError(msg)
        # This is a kludge because the parsing library expects https callbacks
        # We should probably set it up using https
        full_redirect_url = (
            f"https://localhost:{self.redirect_port}/{handler.request_path}"
        )
        try:
            authorization_code_response = client.parse_request_uri_response(
                full_redirect_url, state=state
            )
        except OAuth2Error as e:
            logger.error(f"OAuth Token Request error {e.description}")
            raise e
        return authorization_code_response

    def __send_auth_code_token_request(
        self, client, token_request_url, redirect_url, code, verifier
    ):
        token_request_body = client.prepare_request_body(
            code=code, redirect_uri=redirect_url
        )
        data = f"{token_request_body}&code_verifier={verifier}"
        return self.__send_token_request(token_request_url, data)

    @staticmethod
    def __send_token_request(token_request_url, data):
        headers = {
            "Accept": "application/json",
            "Content-Type": "application/x-www-form-urlencoded",
        }
        response = requests.post(
            url=token_request_url, data=data, headers=headers, auth=IgnoreNetrcAuth()
        )
        return response.json()

    def __send_refresh_token_request(self, hostname, refresh_token):
        oauth_config = self.__fetch_well_known_config(hostname)
        token_request_url = oauth_config["token_endpoint"]
        client = oauthlib.oauth2.WebApplicationClient(self.client_id)
        token_request_body = client.prepare_refresh_body(
            refresh_token=refresh_token, client_id=client.client_id
        )
        return OAuthManager.__send_token_request(token_request_url, token_request_body)

    @staticmethod
    def __get_tokens_from_response(oauth_response):
        access_token = oauth_response["access_token"]
        refresh_token = (
            oauth_response["refresh_token"]
            if "refresh_token" in oauth_response
            else None
        )
        return access_token, refresh_token

    def check_and_refresh_access_token(
        self, hostname: str, access_token: str, refresh_token: str
    ):
        now = datetime.now(tz=timezone.utc)
        # If we can't decode an expiration time, this will be expired by default.
        expiration_time = now
        try:
            # This token has already been verified and we are just parsing it.
            # If it has been tampered with, it will be rejected on the server side.
            # This avoids having to fetch the public key from the issuer and perform
            # an unnecessary signature verification.
            access_token_payload = access_token.split(".")[1]
            # add padding
            access_token_payload = access_token_payload + "=" * (
                -len(access_token_payload) % 4
            )
            decoded = json.loads(base64.standard_b64decode(access_token_payload))
            expiration_time = datetime.fromtimestamp(decoded["exp"], tz=timezone.utc)
        except Exception as e:
            logger.error(e)
            raise e

        if expiration_time > now:
            # The access token is fine. Just return it.
            return access_token, refresh_token, False

        if not refresh_token:
            msg = f"OAuth access token expired on {expiration_time}."
            logger.error(msg)
            raise RuntimeError(msg)

        # Try to refresh using the refresh token
        logger.debug(
            f"Attempting to refresh OAuth access token that expired on {expiration_time}"
        )
        oauth_response = self.__send_refresh_token_request(hostname, refresh_token)
        fresh_access_token, fresh_refresh_token = self.__get_tokens_from_response(
            oauth_response
        )
        return fresh_access_token, fresh_refresh_token, True

    def get_tokens(self, hostname: str, scope=None):
        oauth_config = self.__fetch_well_known_config(hostname)
        # We are going to override oauth_config["authorization_endpoint"] use the
        # /oidc redirector on the hostname, which may inject additional parameters.
        auth_url = self.idp_endpoint.get_authorization_url(hostname)

        state = OAuthManager.__token_urlsafe(16)
        (verifier, challenge) = OAuthManager.__get_challenge()
        client = oauthlib.oauth2.WebApplicationClient(self.client_id)

        try:
            auth_response = self.__get_authorization_code(
                client, auth_url, scope, state, challenge
            )
        except OAuth2Error as e:
            msg = f"OAuth Authorization Error: {e.description}"
            logger.error(msg)
            raise e

        assert self.redirect_port is not None
        redirect_url = OAuthManager.__get_redirect_url(self.redirect_port)

        token_request_url = oauth_config["token_endpoint"]
        code = auth_response["code"]
        oauth_response = self.__send_auth_code_token_request(
            client, token_request_url, redirect_url, code, verifier
        )
        return self.__get_tokens_from_response(oauth_response)
