import logging
import re
import typing

from redshift_connector.error import InterfaceError
from redshift_connector.plugin.saml_credentials_provider import SamlCredentialsProvider
from redshift_connector.redshift_property import RedshiftProperty

_logger: logging.Logger = logging.getLogger(__name__)


class PingCredentialsProvider(SamlCredentialsProvider):
    """
    Identity Provider Plugin providing single sign-on access to an Amazon Redshift cluster using PingOne,
    See `Amazon Redshift docs  <https://docs.aws.amazon.com/redshift/latest/mgmt/options-for-providing-iam-credentials.html#setup-pingfederate-identity-provider>`_
    for setup instructions.
    """

    def __init__(self: "PingCredentialsProvider") -> None:
        super().__init__()
        self.partner_sp_id: typing.Optional[str] = None

    def add_parameter(self: "PingCredentialsProvider", info: RedshiftProperty) -> None:
        super().add_parameter(info)
        self.partner_sp_id = info.partner_sp_id

    # Required method to grab the SAML Response. Used in base class to refresh temporary credentials.
    def get_saml_assertion(self: "PingCredentialsProvider") -> str:
        _logger.debug("PingCredentialsProvider.get_saml_assertion")
        import bs4  # type: ignore
        import requests

        self.check_required_parameters()

        with requests.Session() as session:
            if self.partner_sp_id is None:
                self.partner_sp_id = "urn%3Aamazon%3Awebservices"

            url: str = "https://{host}:{port}/idp/startSSO.ping?PartnerSpId={sp_id}".format(
                host=self.idp_host, port=str(self.idpPort), sp_id=self.partner_sp_id
            )

            try:
                _logger.debug(
                    "Issuing GET request for Ping IdP login page using uri=%s verify=%s", url, self.do_verify_ssl_cert()
                )
                response: "requests.Response" = session.get(url, verify=self.do_verify_ssl_cert())
                _logger.debug("Response code: %s", response.status_code)
                response.raise_for_status()
            except requests.exceptions.HTTPError as e:
                exec_msg: str = ""
                if "response" in vars():
                    exec_msg = (
                        "Get_saml_assertion https response received. Please verify connection properties are correct."
                    )
                    _logger.debug(exec_msg)  # type: ignore
                else:
                    exec_msg = "Get_saml_assertion could not receive https response due to an error. Please verify connection properties are correct."
                    _logger.debug(exec_msg)
                _logger.debug(
                    "Request for SAML assertion when refreshing credentials was unsuccessful. Please verify connection properties are correct.{}".format(
                        str(e)
                    )
                )
                raise InterfaceError(exec_msg) from e
            except requests.exceptions.Timeout as e:
                exec_msg = "A timeout occurred when requesting Ping IdP login page. Please verify connection properties are correct."
                _logger.debug(exec_msg)
                raise InterfaceError(exec_msg) from e
            except requests.exceptions.TooManyRedirects as e:
                exec_msg = "A error occurred when requesting Ping IdP login page. Please verify connection properties are correct."
                _logger.debug(exec_msg)
                raise InterfaceError(exec_msg) from e
            except requests.exceptions.RequestException as e:
                exec_msg = "A unknown error occurred when requesting Ping IdP login page. Please verify connection properties are correct."
                _logger.debug(exec_msg)
                raise InterfaceError(exec_msg) from e

            _logger.debug("response length: %s", len(response.content))

            try:
                soup = bs4.BeautifulSoup(response.text)
            except Exception as e:
                _logger.debug("An error occurred while parsing Ping IdP login page: %s", str(e))
                raise InterfaceError(e)

            payload: typing.Dict[str, typing.Optional[str]] = {}
            username: bool = False
            pwd: bool = False

            _logger.debug(
                "Looking for username and password input tags in Ping IdP login page in order to build authentication request payload"
            )
            for inputtag in soup.find_all(re.compile("(INPUT|input)")):
                name: str = inputtag.get("name", "")
                id: str = inputtag.get("id", "")
                value: str = inputtag.get("value", "")
                _logger.debug("name=%s , id=%s", name, id)

                if username is False and self.is_text(inputtag) and id == "username":
                    _logger.debug("Using tag with name %s for username", name)
                    payload[name] = self.user_name
                    username = True
                elif self.is_password(inputtag) and ("pass" in name):
                    _logger.debug("Using tag with name %s for password", name)
                    if pwd is True:
                        exec_msg = "Failed to parse Ping IdP login form. More than one password field was found on the Ping IdP login page"
                        _logger.debug(exec_msg)
                        raise InterfaceError(exec_msg)
                    payload[name] = self.password
                    pwd = True
                elif name != "":
                    payload[name] = value

            if username is False:
                _logger.debug("username tag still not found, continuing search using secondary preferred tags")
                for inputtag in soup.find_all(re.compile("(INPUT|input)")):
                    name = inputtag.get("name", "")
                    if self.is_text(inputtag) and ("user" in name or "email" in name):
                        _logger.debug("Using tag with name %s for username", name)
                        payload[name] = self.user_name
                        username = True

            if (username is False) or (pwd is False):
                error_msg: str = "Failed to parse Ping IdP login form field(s):"
                if username is False:
                    error_msg += " username"
                if pwd is False:
                    error_msg += " password"
                error_msg += " from response payload."
                _logger.debug(error_msg)
                raise InterfaceError(error_msg)

            action: typing.Optional[str] = self.get_form_action(soup)
            if action and action.startswith("/"):
                url = "https://{host}:{port}{action}".format(host=self.idp_host, port=str(self.idpPort), action=action)
            _logger.debug("Action uri: %s", url)

            try:
                _logger.debug(
                    "Issuing authentication request to Ping IdP using uri %s verify %s", url, self.do_verify_ssl_cert()
                )
                response = session.post(url, data=payload, verify=self.do_verify_ssl_cert())
                _logger.debug("Response code: %s", response.status_code)
                response.raise_for_status()
            except requests.exceptions.HTTPError as e:
                _logger.debug(
                    "Request to refresh credentials was unsuccessful. Please verify connection properties are correct.{}".format(
                        str(e)
                    )
                )
                raise InterfaceError(e)
            except requests.exceptions.Timeout as e:
                _logger.debug(
                    "A timeout occurred when attempting to refresh credentials. Please verify connection properties are correct."
                )
                raise InterfaceError(e)
            except requests.exceptions.TooManyRedirects as e:
                _logger.debug(
                    "A TooManyRedirect error occurred when refreshing credentials. Please verify connection properties are correct."
                )
                raise InterfaceError(e)
            except requests.exceptions.RequestException as e:
                _logger.debug(
                    "A RequestException error occurred when refreshing credentials. Please verify connection properties are correct."
                )
                raise InterfaceError(e)

            try:
                soup = bs4.BeautifulSoup(response.text)
            except Exception as e:
                exec_msg = "An error occurred while parsing Ping IdP authentication response"
                _logger.debug(exec_msg)
                raise InterfaceError(exec_msg) from e

            assertion: str = ""
            for inputtag in soup.find_all("input"):
                if inputtag.get("name") == "SAMLResponse":
                    _logger.debug("SAMLResponse tag found")
                    assertion = inputtag.get("value")

            if assertion == "":
                exec_msg = "Failed to retrieve SAMLAssertion. A input tag named SAMLResponse was not identified in the Ping IdP authentication response"
                _logger.debug(exec_msg)
                raise InterfaceError(exec_msg)

            return assertion
