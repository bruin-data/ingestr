import logging
import re
import typing

from redshift_connector.error import InterfaceError
from redshift_connector.plugin.saml_credentials_provider import SamlCredentialsProvider

_logger: logging.Logger = logging.getLogger(__name__)

if typing.TYPE_CHECKING:
    from redshift_connector import RedshiftProperty


class AdfsCredentialsProvider(SamlCredentialsProvider):
    """
    Identity Provider Plugin providing federated access to an Amazon Redshift cluster using Active Directory Federation
    Services, See `AWS Big Data Blog <https://aws.amazon.com/blogs/big-data/federate-access-to-your-amazon-redshift-cluster-with-active-directory-federation-services-ad-fs-part-1/>`_
    for setup instructions.
    """

    def __init__(self: "AdfsCredentialsProvider") -> None:
        super().__init__()
        self.login_to_rp: typing.Optional[str]

    def add_parameter(self: "AdfsCredentialsProvider", info: "RedshiftProperty") -> None:
        super().add_parameter(info)
        # The value of parameter login_to_rp
        self.login_to_rp = info.login_to_rp

    # Required method to grab the SAML Response. Used in base class to refresh temporary credentials.
    def get_saml_assertion(self: "AdfsCredentialsProvider") -> typing.Optional[str]:
        _logger.debug("AdfsCredentialsProvider.get_saml_assertion")
        if self.idp_host == "" or self.idp_host is None:
            AdfsCredentialsProvider.handle_missing_required_property("idp_host")

        if self.user_name == "" or self.user_name is None or self.password == "" or self.password is None:
            return self.windows_integrated_authentication()

        return self.form_based_authentication()

    def windows_integrated_authentication(self: "AdfsCredentialsProvider"):
        _logger.debug("AdfsCredentialsProvider.windows_integrated_authentication")
        pass

    def form_based_authentication(self: "AdfsCredentialsProvider") -> str:
        _logger.debug("AdfsCredentialsProvider.form_based_authentication")
        import bs4  # type: ignore
        import requests

        url: str = "https://{host}:{port}/adfs/ls/IdpInitiatedSignOn.aspx?loginToRp={loginToRp}".format(
            host=self.idp_host, port=str(self.idpPort), loginToRp=self.login_to_rp
        )
        _logger.debug("Uri: %s", url)
        self.validate_url(url)

        try:
            _logger.debug("Issuing GET request uri=%s verify=%s", url, self.do_verify_ssl_cert())
            response: "requests.Response" = requests.get(url, verify=self.do_verify_ssl_cert())
            _logger.debug("Response code: %s", response.status_code)
            response.raise_for_status()
        except requests.exceptions.HTTPError as e:
            exec_msg: str = ""
            if "response" in vars():
                exec_msg = (
                    "ADFS form based authentication https response received but HTTP response code indicates error"
                )
            else:
                exec_msg = "ADFS form based authentication could not receive https response due to an unknown error"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except requests.exceptions.Timeout as e:
            exec_msg = "ADFS form based authentication request timed out"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except requests.exceptions.TooManyRedirects as e:
            exec_msg = "Too many redirects occurred when requesting ADFS form based authentication"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except requests.exceptions.RequestException as e:
            exec_msg = "A unknown error occurred when requesting ADFS form based authentication"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e

        _logger.debug("ADFS form based authentication response length: %s", len(response.text))

        try:
            _logger.debug("Attempt to parse ADFS authentication form from response payload")
            soup = bs4.BeautifulSoup(response.text, features="lxml")
            _logger.debug("Successfully parsed ADFS authentication form from response payload")
        except Exception as e:
            exec_msg = "An unknown error occurred while parsing ADFS form based authentication response"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e

        payload: typing.Dict[str, typing.Optional[str]] = {}

        for inputtag in soup.find_all(re.compile("(INPUT|input)")):
            name: str = inputtag.get("name", "")
            value: str = inputtag.get("value", "")

            _logger.debug("Input tag name=%s", name)

            if "username" in name.lower():
                _logger.debug("adding user_name %s under payload key %s", self.user_name, name)
                payload[name] = self.user_name
            elif "authmethod" in name.lower():
                _logger.debug("adding authmethod %s under payload key %s", value, name)
                payload[name] = value
            elif "password" in name.lower():
                _logger.debug("adding password under payload key %s", name)
                payload[name] = self.password
            elif name != "":
                _logger.debug("adding value under payload key %s", name)
                payload[name] = value

        action: typing.Optional[str] = self.get_form_action(soup)
        if action and action.startswith("/"):
            url = "https://{host}:{port}{action}".format(host=self.idp_host, port=str(self.idpPort), action=action)

        _logger.debug("ADFS form action uri: %s", url)
        self.validate_url(url)

        try:
            _logger.debug("Issuing POST request uri=%s verify=%s", url, self.do_verify_ssl_cert())
            response = requests.post(url, data=payload, verify=self.do_verify_ssl_cert())
            _logger.debug("Response code: %s", response.status_code)
            response.raise_for_status()
        except requests.exceptions.HTTPError as e:
            exec_msg = "Request to authenticate with ADFS using form based authentication yielded HTTP error."
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except requests.exceptions.Timeout as e:
            exec_msg = "A timeout occurred when attempting to authenticate with ADFS using form based authentication"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except requests.exceptions.TooManyRedirects as e:
            exec_msg = "Too many redirects occurred when authenticate with ADFS using form based authentication. Verify RedshiftProperties are correct"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        except requests.exceptions.RequestException as e:
            exec_msg = "A unknown error occurred when authenticate with ADFS using form based authentication"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e

        try:
            _logger.debug("Attempt parsing ADFS authentication response payload")
            soup = bs4.BeautifulSoup(response.text, features="lxml")
            _logger.debug("Successfully parsed ADFS authentication response payload")
        except Exception as e:
            exec_msg = "An unknown error occurred while parsing ADFS authentication response"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        assertion: str = ""

        for inputtag in soup.find_all("input"):
            if inputtag.get("name") == "SAMLResponse":
                _logger.debug("SAMLResponse HTML input tag found in ADFS authentication response payload")
                assertion = inputtag.get("value")

        if assertion == "":
            exec_msg = "Failed to find ADFS access_token in authentication response payload"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg)

        return assertion
