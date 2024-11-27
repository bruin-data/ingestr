import typing
from abc import abstractmethod

from redshift_connector.error import InterfaceError
from redshift_connector.plugin.i_plugin import IPlugin
from redshift_connector.redshift_property import IAM_URL_PATTERN, RedshiftProperty

if typing.TYPE_CHECKING:
    from redshift_connector.credentials_holder import ABCCredentialsHolder
    from redshift_connector.plugin.native_token_holder import NativeTokenHolder


class IdpCredentialsProvider(IPlugin):
    """
    Abstract base class for authentication plugins.
    """

    def __init__(self: "IdpCredentialsProvider") -> None:
        self.cache: typing.Dict[str, typing.Union[NativeTokenHolder, ABCCredentialsHolder]] = {}

    @staticmethod
    def close_window_http_resp() -> bytes:
        """
        Builds the client facing HTML contents notifying that the authentication window may be closed.
        """
        return str.encode(
            "HTTP/1.1 200 OK\nContent-Type: text/html\n\n"
            + "<html><body>Thank you for using Amazon Redshift! You can now close this window.</body></html>\n"
        )

    @abstractmethod
    def check_required_parameters(self: "IdpCredentialsProvider") -> None:
        """
        Performs validation on client provided parameters used by the IdP.
        """
        pass  # pragma: no cover

    @classmethod
    def validate_url(cls, uri: str) -> None:
        import re

        if not re.fullmatch(pattern=IAM_URL_PATTERN, string=str(uri)):
            raise InterfaceError("URI: {} is an invalid web address".format(uri))
