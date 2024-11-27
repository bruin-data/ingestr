import base64
import concurrent.futures
import logging
import random
import socket
import typing

from redshift_connector.error import InterfaceError
from redshift_connector.plugin.credential_provider_constants import azure_headers
from redshift_connector.plugin.saml_credentials_provider import SamlCredentialsProvider
from redshift_connector.redshift_property import RedshiftProperty

_logger: logging.Logger = logging.getLogger(__name__)


#  Class to get SAML Response from Microsoft Azure using OAuth 2.0 API
class BrowserAzureCredentialsProvider(SamlCredentialsProvider):
    """
    Identity Provider Plugin providing federated access to an Amazon Redshift cluster using Azure browser authentication,
    See `Amazon Redshift docs  <https://docs.amazonaws.cn/en_us/redshift/latest/mgmt/options-for-providing-iam-credentials.html#setup-azure-ad-identity-provider/>`_
    for setup instructions.
    """

    def __init__(self: "BrowserAzureCredentialsProvider") -> None:
        super().__init__()
        self.idp_tenant: typing.Optional[str] = None
        self.client_id: typing.Optional[str] = None

        self.idp_response_timeout: int = 120
        self.listen_port: int = 0

        self.redirectUri: typing.Optional[str] = None

    # method to provide a listen socket for authentication
    def get_listen_socket(self: "BrowserAzureCredentialsProvider") -> socket.socket:
        s: socket.socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        _logger.debug("Attempting socket bind on localhost with any free port")
        s.bind(("127.0.0.1", 0))  # bind to any free port
        s.listen()
        self.listen_port = s.getsockname()[1]
        return s

    # method to grab the field parameters specified by end user.
    # This method adds to it Azure specific parameters.
    def add_parameter(self: "BrowserAzureCredentialsProvider", info: RedshiftProperty) -> None:
        super().add_parameter(info)
        # The value of parameter idp_tenant.
        self.idp_tenant = info.idp_tenant
        # The value of parameter client_id.
        self.client_id = info.client_id

        self.idp_response_timeout = info.idp_response_timeout

    # Required method to grab the SAML Response. Used in base class to refresh temporary credentials.
    def get_saml_assertion(self: "BrowserAzureCredentialsProvider") -> str:
        _logger.debug("BrowserAzureCredentialsProvider.get_saml_assertion")

        if self.idp_tenant == "" or self.idp_tenant is None:
            BrowserAzureCredentialsProvider.handle_missing_required_property("idp_tenant")
        if self.client_id == "" or self.client_id is None:
            BrowserAzureCredentialsProvider.handle_missing_required_property("client_id")
        if self.idp_response_timeout < 10:
            BrowserAzureCredentialsProvider.handle_invalid_property_value(
                "idp_response_timeout", "Integer value must be 10 seconds or greater"
            )

        listen_socket: socket.socket = self.get_listen_socket()
        self.redirectUri = "http://localhost:{port}/redshift/".format(port=self.listen_port)
        _logger.debug("Listening for connection using %s", self.redirectUri)

        try:
            token: str = self.fetch_authorization_token(listen_socket)
            saml_assertion: str = self.fetch_saml_response(token)
        except Exception as e:
            raise e
        finally:
            listen_socket.close()
        _logger.debug("Got SAML assertion")
        return self.wrap_and_encode_assertion(saml_assertion)

    #  First authentication phase:
    #  Set the state in order to check if the incoming request belongs to the current authentication process.
    #  Start the Socket Server at the {@linkplain BrowserAzureCredentialsProvider#m_listen_port} port.
    #  Open the default browser with the link asking a User to enter the credentials.
    #  Retrieve the SAML Assertion string from the response. Decode it, format, validate and return.
    def fetch_authorization_token(self: "BrowserAzureCredentialsProvider", listen_socket: socket.socket) -> str:
        _logger.debug("BrowserAzureCredentialsProvider.fetch_authorization_token")
        alphabet: str = "abcdefghijklmnopqrstuvwxyz"
        state: str = "".join(random.sample(alphabet, 10))
        try:
            return_value: str = ""
            with concurrent.futures.ThreadPoolExecutor() as executor:
                future = executor.submit(self.run_server, listen_socket, self.idp_response_timeout, state)
                self.open_browser(state)
                return_value = future.result()

            return str(return_value)
        except socket.error as e:
            exec_msg: str = "A socket error occurred when attempting to fetch Azure authorization token"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except Exception as e:
            exec_msg = "An unknown exception occurred when attempting to fetch Azure authentication token"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e

    # Initiates the request to the IDP and gets the response body
    # Get Base 64 encoded saml assertion from the response body
    def fetch_saml_response(self: "BrowserAzureCredentialsProvider", token):
        _logger.debug("BrowserAzureCredentialsProvider.fetch_saml_response")
        import requests

        url: str = "https://login.microsoftonline.com/{tenant}/oauth2/token".format(tenant=self.idp_tenant)
        # headers to pass with POST request
        headers: typing.Dict[str, str] = azure_headers
        self.validate_url(url)

        # required parameters to pass in POST body
        payload: typing.Dict[str, typing.Optional[str]] = {
            "code": token,
            "requested_token_type": "urn:ietf:params:oauth:token-type:saml2",
            "grant_type": "authorization_code",
            "scope": "openid",
            "resource": self.client_id,
            "client_id": self.client_id,
            "redirect_uri": self.redirectUri,
        }

        _logger.debug("Uri: %s", url)

        try:
            response = requests.post(url, data=payload, headers=headers, verify=self.do_verify_ssl_cert())
            response.raise_for_status()
        except requests.exceptions.HTTPError as e:
            exec_msg: str = ""
            if "response" in vars():
                exec_msg = "Azure authentication https response received but HTTP response code indicates error"
            else:
                exec_msg = "Azure authentication could not receive https response due to an unknown error"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except requests.exceptions.Timeout as e:
            exec_msg = "Azure authentication request timed out"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except requests.exceptions.TooManyRedirects as e:
            exec_msg = "Too many redirects occurred when requesting Azure authentication"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except requests.exceptions.RequestException as e:
            exec_msg = "A unknown error occurred when requesting Azure authentication"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e

        _logger.debug("Azure authentication response length: %s", len(response.text))

        try:
            saml_assertion: str = response.json()["access_token"]
        except TypeError as e:
            exec_msg = "Failed to decode SAML assertion returned from Azure"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except KeyError as e:
            exec_msg = "Azure access_token was not found in SAML assertion"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except Exception as e:
            exec_msg = "An unknown error occurred when decoding SAML assertion returned from Azure"
            raise InterfaceError(exec_msg) from e
        if saml_assertion == "":
            exec_msg = "Azure access_token is empty"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg)

        missing_padding: int = 4 - len(saml_assertion) % 4
        if missing_padding:
            saml_assertion += "=" * missing_padding

        return str(base64.urlsafe_b64decode(saml_assertion))

    # SAML Response is required to be sent to base class. We need to provide a minimum of:
    # samlp:Response XML tag with xmlns:samlp protocol value
    # samlp:Status XML tag and samlpStatusCode XML tag with Value indicating Success
    # followed by Signed SAML Assertion
    def wrap_and_encode_assertion(self: "BrowserAzureCredentialsProvider", saml_assertion: str) -> str:
        saml_response: str = (
            '<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">'
            "<samlp:Status>"
            '<samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/>'
            "</samlp:Status>"
            "{saml_assertion}"
            "</samlp:Response>".format(saml_assertion=saml_assertion[2:-1])
        )

        return str(base64.b64encode(saml_response.encode("utf-8")))[2:-1]

    def run_server(
        self: "BrowserAzureCredentialsProvider", listen_socket: socket.socket, idp_response_timeout: int, state: str
    ) -> str:
        _logger.debug("BrowserAzureCredentialsProvider.run_server")
        conn, addr = listen_socket.accept()
        conn.settimeout(float(idp_response_timeout))
        size: int = 102400
        with conn:
            while True:
                part: bytes = conn.recv(size)
                decoded_part = part.decode()
                state_idx: int = decoded_part.find("state=")

                if state_idx > -1:
                    received_state: str = decoded_part[state_idx + 6 : decoded_part.find("&", state_idx)]

                    if received_state != state:
                        exec_msg = "Incoming state {received} does not match the outgoing state {expected}".format(
                            received=received_state, expected=state
                        )
                        _logger.debug(exec_msg)
                        raise InterfaceError(exec_msg)

                    code_idx: int = decoded_part.find("code=")

                    if code_idx < 0:
                        exec_msg = "No code found"
                        _logger.debug(exec_msg)
                        raise InterfaceError(exec_msg)

                    received_code: str = decoded_part[code_idx + 5 : decoded_part.find("&", code_idx)]

                    if received_code == "":
                        exec_msg = "No valid code found"
                        _logger.debug(exec_msg)
                        raise InterfaceError(exec_msg)
                    conn.send(self.close_window_http_resp())
                    return received_code

    # Opens the default browser with the authorization request to the IDP
    def open_browser(self: "BrowserAzureCredentialsProvider", state: str) -> None:
        _logger.debug("BrowserAzureCredentialsProvider.open_browser")
        import webbrowser

        url: str = (
            "https://login.microsoftonline.com/{tenant}"
            "/oauth2/authorize"
            "?scope=openid"
            "&response_type=code"
            "&response_mode=form_post"
            "&client_id={id}"
            "&redirect_uri={uri}"
            "&state={state}".format(tenant=self.idp_tenant, id=self.client_id, uri=self.redirectUri, state=state)
        )
        self.validate_url(url)
        webbrowser.open(url)
