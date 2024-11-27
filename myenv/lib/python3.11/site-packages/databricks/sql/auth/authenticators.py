import abc
import base64
import logging
from typing import Callable, Dict, List

from databricks.sql.auth.oauth import OAuthManager
from databricks.sql.auth.endpoint import get_oauth_endpoints, infer_cloud_from_host

# Private API: this is an evolving interface and it will change in the future.
# Please must not depend on it in your applications.
from databricks.sql.experimental.oauth_persistence import OAuthToken, OAuthPersistence


class AuthProvider:
    def add_headers(self, request_headers: Dict[str, str]):
        pass


HeaderFactory = Callable[[], Dict[str, str]]

# In order to keep compatibility with SDK
class CredentialsProvider(abc.ABC):
    """CredentialsProvider is the protocol (call-side interface)
    for authenticating requests to Databricks REST APIs"""

    @abc.abstractmethod
    def auth_type(self) -> str:
        ...

    @abc.abstractmethod
    def __call__(self, *args, **kwargs) -> HeaderFactory:
        ...


# Private API: this is an evolving interface and it will change in the future.
# Please must not depend on it in your applications.
class AccessTokenAuthProvider(AuthProvider):
    def __init__(self, access_token: str):
        self.__authorization_header_value = "Bearer {}".format(access_token)

    def add_headers(self, request_headers: Dict[str, str]):
        request_headers["Authorization"] = self.__authorization_header_value


# Private API: this is an evolving interface and it will change in the future.
# Please must not depend on it in your applications.
class BasicAuthProvider(AuthProvider):
    def __init__(self, username: str, password: str):
        auth_credentials = f"{username}:{password}".encode("UTF-8")
        auth_credentials_base64 = base64.standard_b64encode(auth_credentials).decode(
            "UTF-8"
        )

        self.__authorization_header_value = f"Basic {auth_credentials_base64}"

    def add_headers(self, request_headers: Dict[str, str]):
        request_headers["Authorization"] = self.__authorization_header_value


# Private API: this is an evolving interface and it will change in the future.
# Please must not depend on it in your applications.
class DatabricksOAuthProvider(AuthProvider):
    SCOPE_DELIM = " "

    def __init__(
        self,
        hostname: str,
        oauth_persistence: OAuthPersistence,
        redirect_port_range: List[int],
        client_id: str,
        scopes: List[str],
    ):
        try:
            cloud_type = infer_cloud_from_host(hostname)
            if not cloud_type:
                raise NotImplementedError("Cannot infer the cloud type from hostname")

            idp_endpoint = get_oauth_endpoints(cloud_type)
            if not idp_endpoint:
                raise NotImplementedError(
                    f"OAuth is not supported for cloud ${cloud_type.value}"
                )

            # Convert to the corresponding scopes in the corresponding IdP
            cloud_scopes = idp_endpoint.get_scopes_mapping(scopes)

            self.oauth_manager = OAuthManager(
                port_range=redirect_port_range,
                client_id=client_id,
                idp_endpoint=idp_endpoint,
            )
            self._hostname = hostname
            self._scopes_as_str = DatabricksOAuthProvider.SCOPE_DELIM.join(cloud_scopes)
            self._oauth_persistence = oauth_persistence
            self._client_id = client_id
            self._access_token = None
            self._refresh_token = None
            self._initial_get_token()
        except Exception as e:
            logging.error(f"unexpected error", e, exc_info=True)
            raise e

    def add_headers(self, request_headers: Dict[str, str]):
        self._update_token_if_expired()
        request_headers["Authorization"] = f"Bearer {self._access_token}"

    def _initial_get_token(self):
        try:
            if self._access_token is None or self._refresh_token is None:
                if self._oauth_persistence:
                    token = self._oauth_persistence.read(self._hostname)
                    if token:
                        self._access_token = token.access_token
                        self._refresh_token = token.refresh_token

            if self._access_token and self._refresh_token:
                self._update_token_if_expired()
            else:
                (access_token, refresh_token) = self.oauth_manager.get_tokens(
                    hostname=self._hostname, scope=self._scopes_as_str
                )
                self._access_token = access_token
                self._refresh_token = refresh_token

                if self._oauth_persistence:
                    self._oauth_persistence.persist(
                        self._hostname, OAuthToken(access_token, refresh_token)
                    )
        except Exception as e:
            logging.error(f"unexpected error in oauth initialization", e, exc_info=True)
            raise e

    def _update_token_if_expired(self):
        try:
            (
                fresh_access_token,
                fresh_refresh_token,
                is_refreshed,
            ) = self.oauth_manager.check_and_refresh_access_token(
                hostname=self._hostname,
                access_token=self._access_token,
                refresh_token=self._refresh_token,
            )
            if not is_refreshed:
                return
            else:
                self._access_token = fresh_access_token
                self._refresh_token = fresh_refresh_token

                if self._oauth_persistence:
                    token = OAuthToken(self._access_token, self._refresh_token)
                    self._oauth_persistence.persist(self._hostname, token)
        except Exception as e:
            logging.error(f"unexpected error in oauth token update", e, exc_info=True)
            raise e


class ExternalAuthProvider(AuthProvider):
    def __init__(self, credentials_provider: CredentialsProvider) -> None:
        self._header_factory = credentials_provider()

    def add_headers(self, request_headers: Dict[str, str]):
        headers = self._header_factory()
        for k, v in headers.items():
            request_headers[k] = v
