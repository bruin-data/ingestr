import logging
import typing
from abc import abstractmethod

from redshift_connector.error import InterfaceError
from redshift_connector.iam_helper import IamHelper
from redshift_connector.plugin.i_native_plugin import INativePlugin
from redshift_connector.plugin.idp_credentials_provider import IdpCredentialsProvider
from redshift_connector.plugin.native_token_holder import NativeTokenHolder
from redshift_connector.redshift_property import RedshiftProperty

_logger: logging.Logger = logging.getLogger(__name__)


class CommonCredentialsProvider(INativePlugin, IdpCredentialsProvider):
    """
    Abstract base class for authentication plugins using IdC authentication.
    """

    def __init__(self: "CommonCredentialsProvider") -> None:
        super().__init__()
        self.last_refreshed_credentials: typing.Optional[NativeTokenHolder] = None

    @abstractmethod
    def get_auth_token(self: "CommonCredentialsProvider") -> str:
        """
        Returns the auth token retrieved from corresponding plugin
        """
        pass  # pragma: no cover

    def add_parameter(
        self: "CommonCredentialsProvider",
        info: RedshiftProperty,
    ) -> None:
        self.disable_cache = True

    def get_credentials(self: "CommonCredentialsProvider") -> NativeTokenHolder:
        credentials: typing.Optional[NativeTokenHolder] = None

        if not self.disable_cache:
            key = self.get_cache_key()
            credentials = typing.cast(NativeTokenHolder, self.cache.get(key))

        if not credentials or credentials.is_expired():
            if self.disable_cache:
                _logger.debug("Auth token Cache disabled : fetching new token")
            else:
                _logger.debug("Auth token Cache enabled - No auth token found from cache : fetching new token")

            self.refresh()

            if self.disable_cache:
                credentials = self.last_refreshed_credentials
                self.last_refreshed_credentials = None
        else:
            credentials.refresh = False
            _logger.debug("Auth token found from cache")

        if not self.disable_cache:
            credentials = typing.cast(NativeTokenHolder, self.cache[key])
        return typing.cast(NativeTokenHolder, credentials)

    def refresh(self: "CommonCredentialsProvider") -> None:
        auth_token: str = self.get_auth_token()
        _logger.debug("auth token: {}".format(auth_token))

        if auth_token is None:
            raise InterfaceError("IdC authentication failed : An error occurred during the request.")

        credentials: NativeTokenHolder = NativeTokenHolder(access_token=auth_token, expiration=None)
        credentials.refresh = True

        _logger.debug("disable_cache={}".format(str(self.disable_cache)))
        if not self.disable_cache:
            self.cache[self.get_cache_key()] = credentials
        else:
            self.last_refreshed_credentials = credentials

    def get_idp_token(self: "CommonCredentialsProvider") -> str:
        auth_token: str = self.get_auth_token()
        return auth_token

    def set_group_federation(self: "CommonCredentialsProvider", group_federation: bool):
        pass

    def get_sub_type(self: "CommonCredentialsProvider") -> int:
        return IamHelper.IDC_PLUGIN

    def get_cache_key(self: "CommonCredentialsProvider") -> str:
        return ""
