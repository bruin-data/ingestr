import base64
import concurrent.futures
import hashlib
import logging
import os
import socket
import threading
import time
import typing
import webbrowser
from enum import Enum
from urllib.parse import urlencode, urlunsplit

import boto3
from botocore.exceptions import ClientError

from redshift_connector.error import InterfaceError
from redshift_connector.plugin.common_credentials_provider import (
    CommonCredentialsProvider,
)
from redshift_connector.redshift_property import RedshiftProperty

logging.getLogger(__name__).addHandler(logging.NullHandler())
_logger: logging.Logger = logging.getLogger(__name__)


class BrowserIdcAuthPlugin(CommonCredentialsProvider):
    """
    Class to get IdC Token using SSO OIDC APIs
    """

    class OAuthParamNames(Enum):
        """
        Defines OAuth parameter names used when requesting IdC token from the IdC
        """

        STATE_PARAMETER_NAME = "state"
        AUTH_CODE_PARAMETER_NAME = "code"
        REDIRECT_PARAMETER_NAME = "redirect_uri"
        CLIENT_ID_PARAMETER_NAME = "client_id"
        RESPONSE_TYPE_PARAMETER_NAME = "response_type"
        GRANT_TYPE_PARAMETER_NAME = "grant_type"
        SCOPE_PARAMETER_NAME = "scopes"
        CODE_CHALLENGE_PARAMETER_NAME = "code_challenge"
        CHALLENGE_METHOD_PARAMETER_NAME = "code_challenge_method"

    IDC_CLIENT_DISPLAY_NAME = "Amazon Redshift Python connector"
    CLIENT_TYPE = "public"
    CREATE_TOKEN_INTERVAL = 1
    CODE_VERIFIER_LENGTH = 60
    CURRENT_INTERACTION_SCHEMA = "https"
    OIDC_SCHEMA = "oidc"
    AMAZON_COM_SCHEMA = "amazonaws.com"
    REDSHIFT_IDC_CONNECT_SCOPE = "redshift:connect"
    AUTH_CODE_GRANT_TYPE = "authorization_code"
    REDIRECT_URI = "http://127.0.0.1"
    AUTHORIZE_ENDPOINT = "/authorize"
    CHALLENGE_METHOD = "S256"
    DEFAULT_RESPONSE_TIMEOUT = 120
    DEFAULT_LISTEN_PORT = 7890
    STATE_LENGTH = 10

    def __init__(self: "BrowserIdcAuthPlugin") -> None:
        super().__init__()
        self.idp_response_timeout: int = self.DEFAULT_RESPONSE_TIMEOUT
        self.idc_client_display_name: str = self.IDC_CLIENT_DISPLAY_NAME
        self.listen_port: int = self.DEFAULT_LISTEN_PORT
        self.register_client_cache: typing.Dict[str, dict] = {}
        self.idc_region: typing.Optional[str] = None
        self.issuer_url: typing.Optional[str] = None
        self.redirect_uri: typing.Optional[str] = None
        self.sso_oidc_client: boto3.client = None
        self.auth_code: typing.Optional[str] = None

    def add_parameter(
        self: "BrowserIdcAuthPlugin",
        info: RedshiftProperty,
    ) -> None:
        """
        Adds parameters to the BrowserIdcAuthPlugin
        :param info: RedshiftProperty object containing the parameters to be added to the BrowserIdcAuthPlugin.
        :return: None.
        """
        super().add_parameter(info)
        self.issuer_url = info.issuer_url
        _logger.debug("Setting issuer_url = {}".format(self.issuer_url))
        self.idc_region = info.idc_region
        _logger.debug("Setting idc_region = {}".format(self.idc_region))
        if info.idp_response_timeout and info.idp_response_timeout > 10:
            self.idp_response_timeout = info.idp_response_timeout
        _logger.debug("Setting idp_response_timeout = {}".format(self.idp_response_timeout))
        self.listen_port = info.listen_port
        _logger.debug("Setting listen_port = {}".format(self.listen_port))
        if info.idc_client_display_name:
            self.idc_client_display_name = info.idc_client_display_name
        _logger.debug("Setting idc_client_display_name = {}".format(self.idc_client_display_name))

    def check_required_parameters(self: "BrowserIdcAuthPlugin") -> None:
        """
        Checks if the required parameters are set.
        :return: None.
        :raises InterfaceError: Raised when the parameters are not valid.
        """
        super().check_required_parameters()
        if not self.issuer_url:
            _logger.error("IdC authentication failed: issuer_url needs to be provided in connection params")
            raise InterfaceError(
                "IdC authentication failed: The issuer_url must be included in the connection parameters."
            )
        if not self.idc_region:
            _logger.error("IdC authentication failed: idc_region needs to be provided in connection params")
            raise InterfaceError(
                "IdC authentication failed: The idc_region must be included in the connection parameters."
            )

    def get_auth_token(self: "BrowserIdcAuthPlugin") -> str:
        """
        Returns the auth token as per plugin specific implementation.
        :return: str.
        """
        return self.get_idc_token()

    def get_idc_token(self: "BrowserIdcAuthPlugin") -> str:
        """
        Returns the IdC token using SSO OIDC APIs.
        :return: str.
        """
        _logger.debug("BrowserIdcAuthPlugin.get_idc_token")
        try:
            self.check_required_parameters()

            self.sso_oidc_client = boto3.client("sso-oidc", region_name=self.idc_region)
            self.redirect_uri = self.REDIRECT_URI + ":" + str(self.listen_port)

            register_client_result: typing.Dict[str, typing.Any] = self.register_client()
            code_verifier: str = self.generate_code_verifier()
            code_challenge: str = self.generate_code_challenge(code_verifier)
            auth_code: str = self.fetch_authorization_code(code_challenge, register_client_result)
            access_token = self.fetch_access_token(register_client_result, auth_code, code_verifier)

            return access_token

        except InterfaceError as e:
            raise
        except Exception as e:
            _logger.debug("An error occurred while trying to obtain an IdC token : {}".format(str(e)))
            raise InterfaceError("There was an error during authentication.")

    def register_client(self: "BrowserIdcAuthPlugin") -> typing.Dict[str, typing.Any]:
        """
        Registers the client with IdC.
        :param client_type: str
            The client type to be used for registering the client.
        :return: dict
            The register client result from IdC
        """
        _logger.debug("BrowserIdcAuthPlugin.register_client")
        register_client_cache_key: str = f"{self.idc_client_display_name}:{self.idc_region}:{self.listen_port}"

        if (
            register_client_cache_key in self.register_client_cache
            and self.register_client_cache[register_client_cache_key]["clientSecretExpiresAt"] > time.time()
        ):
            _logger.debug(
                "Valid registerClient result found from cache with expiration time: {}".format(
                    str(self.register_client_cache[register_client_cache_key]["clientSecretExpiresAt"])
                )
            )
            return self.register_client_cache[register_client_cache_key]

        try:
            register_client_result: typing.Dict[str, typing.Any] = self.sso_oidc_client.register_client(
                clientName=self.idc_client_display_name,
                clientType=self.CLIENT_TYPE,
                scopes=[self.REDSHIFT_IDC_CONNECT_SCOPE],
                issuerUrl=self.issuer_url,
                redirectUris=[self.redirect_uri],
                grantTypes=[self.AUTH_CODE_GRANT_TYPE],
            )
            self.register_client_cache[register_client_cache_key] = register_client_result
            _logger.debug(
                "Added entry to client cache with expiry: {}".format(
                    str(register_client_result["clientSecretExpiresAt"])
                )
            )
            return register_client_result
        except ClientError as e:
            raise InterfaceError("IdC authentication failed : Error registering client with IdC.")

    def generate_code_verifier(self: "BrowserIdcAuthPlugin") -> str:
        """
        Generates a random code verifier of length 60.
        :return: str
            Returns the generated code verifier.
        """
        _logger.debug("BrowserIdcAuthPlugin.generate_code_verifier")
        random_bytes = os.urandom(self.CODE_VERIFIER_LENGTH)
        base64_encoded = base64.urlsafe_b64encode(random_bytes).decode("utf-8")
        base64_encoded_no_newline = base64_encoded.replace("\n", "")
        code_verifier = base64_encoded_no_newline.replace("=", "")
        return code_verifier

    def generate_code_challenge(self: "BrowserIdcAuthPlugin", code_verifier: str) -> str:
        """
        Generates a random code verifier
        :param code_verifier: str
            The code_verifier is used to generate the code_challenge.
        :return: dict
            Returns the generated base64 encoded code challenge.
        """
        _logger.debug("BrowserIdcAuthPlugin.generate_code_challenge")
        sha256_hash = hashlib.sha256(code_verifier.encode("ascii")).digest()
        code_challenge = base64.urlsafe_b64encode(sha256_hash).rstrip(b"=").decode("ascii")
        return code_challenge

    def fetch_authorization_code(
        self: "BrowserIdcAuthPlugin", code_challenge: str, register_client_result: typing.Dict[str, typing.Any]
    ) -> str:
        """
        Fetches IdC authorization code using the default browser.
        :param code_challenge: str
            The generated code challenge.
        :param register_client_result: dict
            The register client result from IdC.
        :return: str
            The IdC authorization code obtained from the browser.
        """
        state = self.generate_random_state()
        listen_socket: socket.socket = self.get_listen_socket(self.listen_port)

        try:
            listen_socket.settimeout(float(self.idp_response_timeout))
            server_thread = threading.Thread(target=self.run_server, args=(listen_socket, state))
            server_thread.start()

            self.open_browser(state, register_client_result["clientId"], code_challenge)

            server_thread.join()

            return str(self.auth_code)
        except socket.timeout:
            raise InterfaceError("IdC authentication failed : Timeout while retrieving authorization code.")
        except Exception as e:
            raise e
        finally:
            listen_socket.close()

    def fetch_access_token(
        self: "BrowserIdcAuthPlugin",
        register_client_result: typing.Dict[str, typing.Any],
        auth_code: str,
        code_verifier: str,
    ) -> str:
        """
        Fetches IdC access token using SSO OIDC APIs.
        :param register_client_result: dict
            The register client result from IdC.
        :param auth_code: str
            The authorization code result from IdC.
        :param grant_type: str
            The grant type to be used for fetch IdC access token.
        :return: str
            The IdC access token obtained from fetching IdC access token.
        :raises InterfaceError: Raised when the IdC access token is not fetched successfully.
        """
        _logger.debug("BrowserIdcAuthPlugin.fetch_access_token")
        polling_end_time: float = time.time() + self.idp_response_timeout
        polling_interval_in_sec: int = self.CREATE_TOKEN_INTERVAL

        while time.time() < polling_end_time:
            try:
                _logger.debug("Calling IdC method create_token")
                response: typing.Dict[str, typing.Any] = self.sso_oidc_client.create_token(
                    clientId=register_client_result["clientId"],
                    clientSecret=register_client_result["clientSecret"],
                    code=auth_code,
                    grantType=self.AUTH_CODE_GRANT_TYPE,
                    codeVerifier=code_verifier,
                    redirectUri=self.redirect_uri,
                )
                if not response["accessToken"]:
                    raise InterfaceError("IdC authentication failed : The credential token couldn't be created.")
                _logger.debug("Length of IdC accessToken: {}".format(len(response["accessToken"])))
                return response["accessToken"]
            except ClientError as e:
                if e.response["Error"]["Code"] == "AuthorizationPendingException":
                    _logger.debug("Browser authorization pending from user")
                    time.sleep(polling_interval_in_sec)
                else:
                    raise InterfaceError(
                        "IdC authentication failed : Unexpected error occured while fetching access token."
                    )

        raise InterfaceError("IdC authentication failed : The request timed out. Authentication wasn't completed.")

    def generate_random_state(self: "BrowserIdcAuthPlugin") -> str:
        random_bytes = os.urandom(self.STATE_LENGTH)
        random_state = base64.urlsafe_b64encode(random_bytes).decode("utf-8").rstrip("=")
        return random_state

    def run_server(
        self: "BrowserIdcAuthPlugin",
        listen_socket: socket.socket,
        state: str,
    ):
        """
        Runs a server on localhost to listen for the IdC's response with authorization code.
        :param listen_socket: socket.socket
            The socket on which the method listens for a response
        :param idp_response_timeout: int
            The maximum time to listen on the socket, specified in seconds
        :param state: str
            The state generated by the client. This must match the state received from the IdC server
        :return: None
        """
        conn, addr = listen_socket.accept()
        size: int = 102400
        with conn:
            while True:
                part: bytes = conn.recv(size)
                decoded_part = part.decode()
                state_idx: int = decoded_part.find(
                    "{}=".format(BrowserIdcAuthPlugin.OAuthParamNames.STATE_PARAMETER_NAME.value)
                )

                if state_idx > -1:
                    received_state: str = decoded_part[state_idx + 6 : decoded_part.find("&", state_idx)]
                    parsed_state: str = received_state[: received_state.find(" ")]

                    if parsed_state != state:
                        exec_msg = "Incoming state {received} does not match the outgoing state {expected}".format(
                            received=parsed_state, expected=state
                        )
                        _logger.debug(exec_msg)
                        raise InterfaceError(exec_msg)

                    code_idx: int = decoded_part.find(
                        "{}=".format(BrowserIdcAuthPlugin.OAuthParamNames.AUTH_CODE_PARAMETER_NAME.value)
                    )

                    if code_idx < 0:
                        _logger.debug("No authorization code found")
                        raise InterfaceError("No authorization code found")
                    received_code: str = decoded_part[code_idx + 5 : state_idx - 1]

                    if received_code == "":
                        _logger.debug("No valid authorization code found")
                        raise InterfaceError("No valid authorization code found")
                    conn.send(self.close_window_http_resp())
                    self.auth_code = received_code
                    return

    def open_browser(self: "BrowserIdcAuthPlugin", state: str, client_id: str, code_challenge: str) -> None:
        """
        Opens the default browser to allow user authentication with IdC
        :param state: str
            The state generated by the client
        :return: None.
        """
        url: str = self.get_authorization_token_url(state, client_id, code_challenge)

        if url is None:
            BrowserIdcAuthPlugin.handle_missing_required_property("issuer_url")
        self.validate_url(url)

        _logger.debug("Authorization code request URI: {}".format(url))

        try:
            webbrowser.open(url)
        except:
            _logger.debug("Unable to open the browser. Webbrowser environment is not supported")

    def get_authorization_token_url(
        self: "BrowserIdcAuthPlugin", state: str, client_id: str, code_challenge: str
    ) -> str:
        """
        Returns a URL used for requesting authentication token from IdC
        """
        _logger.debug("BrowserIdcAuthPlugin.get_authorization_token_url")

        params: typing.Dict[str, str] = {
            BrowserIdcAuthPlugin.OAuthParamNames.RESPONSE_TYPE_PARAMETER_NAME.value: "code",
            BrowserIdcAuthPlugin.OAuthParamNames.CLIENT_ID_PARAMETER_NAME.value: client_id,
            BrowserIdcAuthPlugin.OAuthParamNames.REDIRECT_PARAMETER_NAME.value: str(self.redirect_uri),
            BrowserIdcAuthPlugin.OAuthParamNames.STATE_PARAMETER_NAME.value: state,
            BrowserIdcAuthPlugin.OAuthParamNames.SCOPE_PARAMETER_NAME.value: self.REDSHIFT_IDC_CONNECT_SCOPE,
            BrowserIdcAuthPlugin.OAuthParamNames.CODE_CHALLENGE_PARAMETER_NAME.value: code_challenge,
            BrowserIdcAuthPlugin.OAuthParamNames.CHALLENGE_METHOD_PARAMETER_NAME.value: self.CHALLENGE_METHOD,
        }

        encoded_params: str = urlencode(params)
        idc_host = self.OIDC_SCHEMA + "." + str(self.idc_region) + "." + self.AMAZON_COM_SCHEMA

        return urlunsplit(
            (
                self.CURRENT_INTERACTION_SCHEMA,
                idc_host,
                self.AUTHORIZE_ENDPOINT,
                encoded_params,
                "",
            )
        )

    def get_listen_socket(self: "BrowserIdcAuthPlugin", listen_port: int) -> socket.socket:
        """
        Returns a listen socket used for user authentication
        """
        s: socket.socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        _logger.debug("Attempting socket bind on port {}".format(str(listen_port)))
        s.bind(("127.0.0.1", listen_port))
        s.listen()
        _logger.debug("Socket bound to port {}".format(s.getsockname()[1]))
        return s
