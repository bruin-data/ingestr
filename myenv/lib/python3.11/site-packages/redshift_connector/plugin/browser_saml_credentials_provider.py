import concurrent.futures
import logging
import re
import socket
import typing
import urllib.parse

from redshift_connector.error import InterfaceError
from redshift_connector.plugin.saml_credentials_provider import SamlCredentialsProvider
from redshift_connector.redshift_property import RedshiftProperty

_logger: logging.Logger = logging.getLogger(__name__)


#  Class to get SAML Response
class BrowserSamlCredentialsProvider(SamlCredentialsProvider):
    """
    Generic Identity Provider Browser Plugin providing multi-factor authentication access to an Amazon Redshift cluster using an identity provider of your choice.
    """

    def __init__(self: "BrowserSamlCredentialsProvider") -> None:
        super().__init__()
        self.login_url: typing.Optional[str] = None

        self.idp_response_timeout: int = 120
        self.listen_port: int = 7890

    # method to grab the field parameters specified by end user.
    # This method adds to it specific parameters.
    def add_parameter(self: "BrowserSamlCredentialsProvider", info: RedshiftProperty) -> None:
        super().add_parameter(info)
        self.login_url = info.login_url

        self.idp_response_timeout = info.idp_response_timeout
        self.listen_port = info.listen_port

    # Required method to grab the SAML Response. Used in base class to refresh temporary credentials.
    def get_saml_assertion(self: "BrowserSamlCredentialsProvider") -> str:
        _logger.debug("BrowserSamlCredentialsProvider.get_saml_assertion")

        if self.login_url == "" or self.login_url is None:
            BrowserSamlCredentialsProvider.handle_missing_required_property("login_url")

        if self.idp_response_timeout < 10:
            BrowserSamlCredentialsProvider.handle_invalid_property_value(
                "idp_response_timeout", "Must be 10 seconds or greater"
            )
        if self.listen_port < 1 or self.listen_port > 65535:
            BrowserSamlCredentialsProvider.handle_invalid_property_value("listen_port", "Must be in range [1,65535]")

        return self.authenticate()

    # Authentication consists of:
    # Start the Socket Server on the port {@link BrowserSamlCredentialsProvider#m_listen_port}.
    # Open the default browser with the link asking a User to enter the credentials.
    # Retrieve the SAML Assertion string from the response.
    def authenticate(self: "BrowserSamlCredentialsProvider") -> str:
        _logger.debug("BrowserSamlCredentialsProvider.authenticate")

        try:
            with concurrent.futures.ThreadPoolExecutor() as executor:
                _logger.debug("Listening for connection on port %s", self.listen_port)
                future = executor.submit(self.run_server, self.listen_port, self.idp_response_timeout)
                self.open_browser()
                return_value: str = future.result()

            samlresponse = urllib.parse.unquote(return_value)
            return str(samlresponse)
        except socket.error as e:
            _logger.debug("Socket error: %s", e)
            raise e
        except Exception as e:
            _logger.debug("Other Exception: %s", e)
            raise e

    def run_server(self: "BrowserSamlCredentialsProvider", listen_port: int, idp_response_timeout: int) -> str:
        _logger.debug("BrowserSamlCredentialsProvider.run_server")
        HOST: str = "127.0.0.1"
        PORT: int = listen_port

        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            _logger.debug("attempting socket bind on host %s port %s", HOST, PORT)
            s.bind((HOST, PORT))
            s.listen()
            conn, addr = s.accept()  # typing.Tuple[Socket, Any]
            _logger.debug("Localhost socket connection established for Browser SAML IdP")
            conn.settimeout(float(idp_response_timeout))
            size: int = 102400
            with conn:
                while True:
                    part: bytes = conn.recv(size)
                    decoded_part: str = part.decode()
                    result: typing.Optional[typing.Match] = re.search(
                        pattern="SAMLResponse[:=]+[\\n\\r]*", string=decoded_part, flags=re.MULTILINE
                    )
                    _logger.debug("Data received contained SAML Response: %s", result is not None)

                    if result is not None:
                        conn.send(self.close_window_http_resp())
                        saml_resp_block: str = decoded_part[result.regs[0][1] :]
                        end_idx: int = saml_resp_block.find("&RelayState=")
                        if end_idx > -1:
                            saml_resp_block = saml_resp_block[:end_idx]
                        return saml_resp_block

    # Opens the default browser with the authorization request to the web service.
    def open_browser(self: "BrowserSamlCredentialsProvider") -> None:
        _logger.debug("BrowserSamlCredentialsProvider.open_browser")
        import webbrowser

        url: typing.Optional[str] = self.login_url

        if url is None:
            BrowserSamlCredentialsProvider.handle_missing_required_property("login_url")
        self.validate_url(typing.cast(str, url))
        webbrowser.open(typing.cast(str, url))
