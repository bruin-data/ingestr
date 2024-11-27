import logging
import typing

from redshift_connector.error import InterfaceError
from redshift_connector.idp_auth_helper import IdpAuthHelper
from redshift_connector.plugin.i_native_plugin import INativePlugin

if typing.TYPE_CHECKING:
    from redshift_connector.plugin.native_token_holder import NativeTokenHolder
    from redshift_connector.redshift_property import RedshiftProperty

logging.getLogger(__name__).addHandler(logging.NullHandler())
_logger: logging.Logger = logging.getLogger(__name__)


class NativeAuthPluginHelper:
    @staticmethod
    def set_native_auth_plugin_properties(info: "RedshiftProperty") -> None:
        """
        Modifies ``info`` to prepare for authentication with Amazon Redshift

        Parameters
        ----------
        info: RedshiftProperty
            RedshiftProperty object storing user defined and derived attributes used for authentication

        Returns
        -------
        None:None
        """
        if info.credentials_provider:
            # include the authentication token which will be used for authentication via
            # Redshift Native IDP Integration
            _logger.debug("Attempting to get native auth plugin credentials")
            idp_token: str = NativeAuthPluginHelper.get_native_auth_plugin_credentials(info)
            if idp_token:
                _logger.debug("setting info.web_identity_token")
                info.put("web_identity_token", idp_token)

    @staticmethod
    def get_native_auth_plugin_credentials(info: "RedshiftProperty") -> str:
        """
        Retrieves credentials for Amazon Redshift native authentication.

        Parameters
        ----------
        info: RedshiftProperty
            RedshiftProperty object storing user defined and derived attributes used for authentication

        Returns
        -------
        str: An authentication token compatible with Redshift Native IDP Integration (code 14)
        """
        idp_token: typing.Optional[str] = None
        provider = None

        if info.credentials_provider:
            provider = IdpAuthHelper.load_credentials_provider(info)

            if not isinstance(provider, INativePlugin):
                _logger.debug("Native auth will not be used, no credentials provider specified")
                return ""
        else:
            raise InterfaceError(
                "Required credentials_provider parameter is null or empty: {}".format(info.credentials_provider)
            )

        _logger.debug("Native IDP Credential Provider %s:%s", provider, info.credentials_provider)
        _logger.debug("Calling provider.getCredentials()")

        # Provider will cache the credentials, it's OK to call get_credentials() here
        credentials: "NativeTokenHolder" = typing.cast("NativeTokenHolder", provider.get_credentials())

        _logger.debug("credentials is None = %s", credentials is None)
        _logger.debug("credentials.is_expired() = %s", credentials.is_expired())

        if credentials is None or (credentials.expiration is not None and credentials.is_expired()):
            # get idp token
            plugin: INativePlugin = provider
            _logger.debug("Unable to get IdP token from cache. Calling plugin.get_idp_token()")

            idp_token = plugin.get_idp_token()
            _logger.debug("IdP token retrieved")
            info.put("idp_token", idp_token)
        else:
            _logger.debug("Cached idp_token will be used")
            idp_token = credentials.access_token

        return idp_token
