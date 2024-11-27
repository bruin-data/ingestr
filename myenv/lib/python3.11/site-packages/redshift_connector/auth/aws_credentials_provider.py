import logging
import typing

from redshift_connector.credentials_holder import (
    ABCCredentialsHolder,
    AWSDirectCredentialsHolder,
    AWSProfileCredentialsHolder,
)
from redshift_connector.error import InterfaceError

_logger: logging.Logger = logging.getLogger(__name__)

if typing.TYPE_CHECKING:
    import boto3  # type: ignore

    from redshift_connector.redshift_property import RedshiftProperty


class AWSCredentialsProvider:
    """
    A credential provider class for AWS credentials specified via :func:`~redshift_connector.connect` using `profile` or AWS access keys.
    """

    def __init__(self: "AWSCredentialsProvider") -> None:
        self.cache: typing.Dict[int, typing.Union[AWSDirectCredentialsHolder, AWSProfileCredentialsHolder]] = {}

        self.access_key_id: typing.Optional[str] = None
        self.secret_access_key: typing.Optional[str] = None
        self.session_token: typing.Optional[str] = None
        self.profile: typing.Optional["boto3.Session"] = None

    def get_cache_key(self: "AWSCredentialsProvider") -> int:
        """
        Creates a cache key using the hash of either the end-user provided AWS credential information.

        Returns
        -------
        An `int` hash representation of the non-secret portion of credential information: `int`
        """
        if self.profile:
            return hash(self.profile)
        else:
            return hash(self.access_key_id)

    def get_credentials(
        self: "AWSCredentialsProvider",
    ) -> typing.Union[AWSDirectCredentialsHolder, AWSProfileCredentialsHolder]:
        """
        Retrieves a :class`ABCCredentialsHolder` from cache or builds one.

        Returns
        -------
        An `AWSCredentialsHolder` object containing end-user specified AWS credential information: :class`ABCAWSCredentialsHolder`
        """
        key: int = self.get_cache_key()
        if key not in self.cache:
            try:
                self.refresh()
            except Exception as e:
                exec_msg: str = "Refreshing IdP credentials failed"
                _logger.debug(exec_msg)
                raise InterfaceError(e)

        credentials: typing.Union[AWSDirectCredentialsHolder, AWSProfileCredentialsHolder] = self.cache[key]

        if credentials is None:
            exec_msg = "Unable to load AWS credentials from cache"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg)

        return credentials

    def add_parameter(self: "AWSCredentialsProvider", info: "RedshiftProperty") -> None:
        """
        Defines instance variables used for creating a :class`ABCCredentialsHolder` object and associated :class:`boto3.Session`

        Parameters
        ----------
        info : :class:`RedshiftProperty`
            The :class:`RedshiftProperty` object created using end-user specified values passed to :func:`~redshift_connector.connect`
        """
        self.access_key_id = info.access_key_id
        self.secret_access_key = info.secret_access_key
        self.session_token = info.session_token
        self.profile = info.profile

    def refresh(self: "AWSCredentialsProvider") -> None:
        """
        Establishes a :class:`boto3.Session` using end-user specified AWS credential information
        """
        import boto3  # type: ignore

        args: typing.Dict[str, str] = {}

        if self.profile is not None:
            args["profile_name"] = self.profile
        elif self.access_key_id is not None:
            args["aws_access_key_id"] = self.access_key_id
            args["aws_secret_access_key"] = typing.cast(str, self.secret_access_key)
            if self.session_token is not None:
                args["aws_session_token"] = self.session_token

        session: boto3.Session = boto3.Session(**args)
        credentials: typing.Optional[typing.Union[AWSProfileCredentialsHolder, AWSDirectCredentialsHolder]] = None

        if self.profile is not None:
            credentials = AWSProfileCredentialsHolder(profile=self.profile, session=session)
        else:
            credentials = AWSDirectCredentialsHolder(
                access_key_id=typing.cast(str, self.access_key_id),
                secret_access_key=typing.cast(str, self.secret_access_key),
                session_token=self.session_token,
                session=session,
            )

        key = self.get_cache_key()
        self.cache[key] = credentials
