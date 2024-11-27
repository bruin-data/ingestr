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


class JwtCredentialsProvider(INativePlugin, IdpCredentialsProvider):
    """
    Abstract base class for authentication plugins using JWT for Redshift native authentication.
    """

    KEY_ROLE_ARN: str = "role_arn"
    KEY_WEB_IDENTITY_TOKEN: str = "web_identity_token"
    KEY_DURATION: str = "duration"
    KEY_ROLE_SESSION_NAME: str = "role_session_name"
    DEFAULT_ROLE_SESSION_NAME: str = "jwt_redshift_session"

    def __init__(self: "JwtCredentialsProvider") -> None:
        super().__init__()
        self.last_refreshed_credentials: typing.Optional[NativeTokenHolder] = None

    @abstractmethod
    def get_jwt_assertion(self: "JwtCredentialsProvider") -> str:
        """
        Returns the jwt assertion retrieved following IdP authentication
        """
        pass  # pragma: no cover

    def add_parameter(
        self: "JwtCredentialsProvider",
        info: RedshiftProperty,
    ) -> None:
        self.provider_name = info.provider_name
        self.ssl_insecure = info.ssl_insecure
        self.disable_cache = info.iam_disable_cache
        self.group_federation = False

        if info.role_session_name is not None:
            self.role_session_name = info.role_session_name

    def set_group_federation(self: "JwtCredentialsProvider", group_federation: bool):
        self.group_federation = group_federation

    def get_credentials(self: "JwtCredentialsProvider") -> NativeTokenHolder:
        _logger.debug("JwtCredentialsProvider.get_credentials")
        credentials: typing.Optional[NativeTokenHolder] = None

        if not self.disable_cache:
            _logger.debug("checking cache for credentials")
            key = self.get_cache_key()
            credentials = typing.cast(NativeTokenHolder, self.cache.get(key))

        if not credentials or credentials.is_expired():
            _logger.debug("JWT get_credentials NOT from cache")
            self.refresh()

            if self.disable_cache:
                credentials = self.last_refreshed_credentials
                self.last_refreshed_credentials = None
        else:
            credentials.refresh = False
            _logger.debug("JWT get_credentials from cache")

        if not self.disable_cache:
            credentials = typing.cast(NativeTokenHolder, self.cache[key])
        return typing.cast(NativeTokenHolder, credentials)

    def refresh(self: "JwtCredentialsProvider") -> None:
        _logger.debug("JwtCredentialsProvider.refresh")
        jwt: str = self.get_jwt_assertion()

        if jwt is None:
            exec_msg = "Unable to refresh, no jwt provided"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg)

        credentials: NativeTokenHolder = NativeTokenHolder(access_token=jwt, expiration=None)
        credentials.refresh = True

        _logger.debug("disable_cache=%s", self.disable_cache)
        if not self.disable_cache:
            self.cache[self.get_cache_key()] = credentials

        else:
            self.last_refreshed_credentials = credentials

    def do_verify_ssl_cert(self: "JwtCredentialsProvider") -> bool:
        return self.ssl_insecure

    def get_idp_token(self: "JwtCredentialsProvider") -> str:
        jwt: str = self.get_jwt_assertion()

        return jwt

    def get_sub_type(self: "JwtCredentialsProvider") -> int:
        return IamHelper.JWT_PLUGIN


class BasicJwtCredentialsProvider(JwtCredentialsProvider):
    """
    A basic JWT Credential provider class that can be changed and implemented to work with any desired JWT service provider.
    """

    def __init__(self: "BasicJwtCredentialsProvider") -> None:
        super().__init__()
        self.jwt: typing.Optional[str] = None

    def add_parameter(
        self: "BasicJwtCredentialsProvider",
        info: RedshiftProperty,
    ) -> None:
        super().add_parameter(info)
        self.jwt = info.web_identity_token

        if info.role_session_name is not None:
            self.role_session_name = info.role_session_name

    def check_required_parameters(self: "BasicJwtCredentialsProvider") -> None:
        super().check_required_parameters()
        if not self.jwt:
            BasicJwtCredentialsProvider.handle_missing_required_property("jwt")

    def get_cache_key(self: "BasicJwtCredentialsProvider") -> str:
        return typing.cast(str, self.jwt)

    def get_jwt_assertion(self: "BasicJwtCredentialsProvider") -> str:
        self.check_required_parameters()
        return self.jwt  # type: ignore
