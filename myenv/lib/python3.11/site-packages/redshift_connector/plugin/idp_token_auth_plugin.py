import logging
import typing

from redshift_connector.error import InterfaceError
from redshift_connector.plugin.common_credentials_provider import (
    CommonCredentialsProvider,
)
from redshift_connector.redshift_property import RedshiftProperty

logging.getLogger(__name__).addHandler(logging.NullHandler())
_logger: logging.Logger = logging.getLogger(__name__)


class IdpTokenAuthPlugin(CommonCredentialsProvider):
    """
    A basic IdP Token auth plugin class. This plugin class allows clients to directly provide any auth token that is handled by Redshift.
    """

    def __init__(self: "IdpTokenAuthPlugin") -> None:
        super().__init__()
        self.token: typing.Optional[str] = None
        self.token_type: typing.Optional[str] = None

    def add_parameter(
        self: "IdpTokenAuthPlugin",
        info: RedshiftProperty,
    ) -> None:
        super().add_parameter(info)
        self.token = info.token
        self.token_type = info.token_type
        _logger.debug("Setting token_type = {}".format(self.token_type))

    def check_required_parameters(self: "IdpTokenAuthPlugin") -> None:
        super().check_required_parameters()
        if not self.token:
            _logger.error("IdC authentication failed: token needs to be provided in connection params")
            raise InterfaceError("IdC authentication failed: The token must be included in the connection parameters.")
        if not self.token_type:
            _logger.error("IdC authentication failed: token_type needs to be provided in connection params")
            raise InterfaceError(
                "IdC authentication failed: The token type must be included in the connection parameters."
            )

    def get_cache_key(self: "IdpTokenAuthPlugin") -> str:  # type: ignore
        pass

    def get_auth_token(self: "IdpTokenAuthPlugin") -> str:
        self.check_required_parameters()
        return typing.cast(str, self.token)
