import json
import logging
import typing

from redshift_connector.error import InterfaceError
from redshift_connector.plugin.credential_provider_constants import okta_headers
from redshift_connector.plugin.saml_credentials_provider import SamlCredentialsProvider
from redshift_connector.redshift_property import RedshiftProperty

_logger: logging.Logger = logging.getLogger(__name__)


# Class to get SAML Response from Okta
class OktaCredentialsProvider(SamlCredentialsProvider):
    """
    Identity Provider Plugin providing single sign-on access to an Amazon Redshift cluster using Okta,
    See `Amazon Redshift docs  <https://docs.aws.amazon.com/redshift/latest/mgmt/options-for-providing-iam-credentials.html#setup-okta-identity-provider>`_
    for setup instructions.
    """

    def __init__(self: "OktaCredentialsProvider") -> None:
        super().__init__()
        self.app_id: typing.Optional[str] = None
        self.app_name: typing.Optional[str] = None

    def add_parameter(self: "OktaCredentialsProvider", info: RedshiftProperty) -> None:
        super().add_parameter(info)
        self.app_id = info.app_id
        self.app_name = info.app_name

    def get_saml_assertion(self: "OktaCredentialsProvider") -> str:
        _logger.debug("OktaCredentialsProvider.get_saml_assertion")
        self.check_required_parameters()
        if self.app_id == "" or self.app_id is None:
            OktaCredentialsProvider.handle_missing_required_property("app_id")

        okta_session_token: str = self.okta_authentication()
        return self.handle_saml_assertion(okta_session_token)

    # Authenticates users credentials via Okta, return Okta session token.
    def okta_authentication(self: "OktaCredentialsProvider") -> str:
        _logger.debug("OktaCredentialsProvider.okta_authentication")
        import requests

        # HTTP Post request to Okta API for session token
        url: str = "https://{host}/api/v1/authn".format(host=self.idp_host)
        _logger.debug("Okta authentication request uri: %s", url)
        self.validate_url(url)
        headers: typing.Dict[str, str] = okta_headers
        payload: typing.Dict[str, typing.Optional[str]] = {"username": self.user_name, "password": self.password}
        _logger.debug("Okta authentication payload contains username=%s", self.user_name)

        try:
            _logger.debug("Issuing Okta authentication request using uri %s verify %s", url, self.do_verify_ssl_cert())
            response: "requests.Response" = requests.post(
                url, data=json.dumps(payload), headers=headers, verify=self.do_verify_ssl_cert()
            )
            _logger.debug("Response code: %s", response.status_code)
            response.raise_for_status()
        except requests.exceptions.HTTPError as e:
            if "response" in vars():
                _logger.debug("Okta authentication response body was non empty")  # type: ignore
            else:
                _logger.debug("Okta authentication response raised an exception. No response returned.")
            _logger.debug(
                "Request for authentication from Okta was unsuccessful. Please verify connection properties are correct. {}".format(
                    str(e)
                )
            )
            raise InterfaceError(e)
        except requests.exceptions.Timeout as e:
            _logger.debug(
                "A timeout occurred when requesting authentication from Okta. Please verify connection properties are correct."
            )
            raise InterfaceError(e)
        except requests.exceptions.TooManyRedirects as e:
            _logger.debug(
                "A error occurred when requesting authentication from Okta. Please verify connection properties are correct."
            )
            raise InterfaceError(e)
        except requests.exceptions.RequestException as e:
            _logger.debug(
                "A unknown error occurred when requesting authentication from Okta. Please verify connection properties are correct."
            )
            raise InterfaceError(e)

        # Retrieve and parse the Okta response for session token
        if response is None:
            exec_msg = (
                "Request for authentication returned empty payload. Please verify connection properties are correct."
            )
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg)
        _logger.debug("Okta_authentication https response length: %s", len(response.content))
        response_payload: typing.Dict[str, typing.Any] = response.json()

        if "status" not in response_payload:
            exec_msg = "Request for authentication with Okta IdP failed. The status key was missing."
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg)
        elif response_payload["status"] != "SUCCESS":
            exec_msg = "Request for authentication with Okta IdP failed due to a unsuccessful status in the authentication response payload. Response status was {}".format(
                response_payload["status"]
            )
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg)
        else:
            _logger.debug("response payload status indicated success. extracting sessionToken")
            return str(response_payload["sessionToken"])

    # Retrieves SAML assertion from Okta containing AWS roles.
    def handle_saml_assertion(self: "OktaCredentialsProvider", okta_session_token: str) -> str:
        _logger.debug("OktaCredentialsProvider.handle_saml_assertion")
        import bs4  # type: ignore
        import requests

        url: str = "https://{host}/home/{app_name}/{app_id}?onetimetoken={session_token}".format(
            host=self.idp_host, app_name=self.app_name, app_id=self.app_id, session_token=okta_session_token
        )
        _logger.debug("OktaAWSAppUrl: %s", url)
        self.validate_url(url)

        try:
            _logger.debug(
                "Issuing request for SAML assertion to Okta IdP using uri=%s verify=%s", url, self.do_verify_ssl_cert()
            )
            response: "requests.Response" = requests.get(url, verify=self.do_verify_ssl_cert())
            _logger.debug("Response code: %s", response.status_code)
            response.raise_for_status()
        except requests.exceptions.HTTPError as e:
            _logger.debug(
                "Request for SAML assertion from Okta was unsuccessful. Please verify connection properties are correct. {}".format(
                    str(e)
                )
            )
            raise InterfaceError(e)
        except requests.exceptions.Timeout as e:
            _logger.debug(
                "A timeout occurred when requesting SAML assertion from Okta. Please verify connection properties are correct."
            )
            raise InterfaceError(e)
        except requests.exceptions.TooManyRedirects as e:
            _logger.debug(
                "A error occurred when requesting SAML assertion from Okta. Please verify connection properties are correct."
            )
            raise InterfaceError(e)
        except requests.exceptions.RequestException as e:
            _logger.debug(
                "A unknown error occurred when requesting SAML assertion from Okta. Please verify connection properties are correct."
            )
            raise InterfaceError(e)

        text: str = response.text
        _logger.debug("Length of response from Okta with SAML response %s", len(response.content))

        try:
            soup = bs4.BeautifulSoup(text, "html.parser")
            saml_response: str = soup.find("input", {"name": "SAMLResponse"})["value"]
            return saml_response
        except Exception as e:
            _logger.debug("An error occurred while parsing SAML response: %s", str(e))
            raise InterfaceError(e)
