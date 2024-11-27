import datetime
import logging
import time
import typing
from abc import ABC, abstractmethod

if typing.TYPE_CHECKING:
    import boto3  # type: ignore


_logger: logging.Logger = logging.getLogger(__name__)


class ABCCredentialsHolder(ABC):
    """
    Abstract base class used to store credentials for establishing a connection to an Amazon Redshift cluster.
    """

    @abstractmethod
    def get_session_credentials(self: "ABCCredentialsHolder"):
        """
        A dictionary mapping end-user specified AWS credential value to :func:`boto3.client` parameters.

        Returns
        _______
        A dictionary mapping parameter names to end-user specified values: `typing.Dict[str,str]`
        """
        pass

    @property
    def has_associated_session(self: "ABCCredentialsHolder") -> bool:
        """
         A boolean value indicating if the current class stores AWS credentials in a :class:`boto3.Session`.

         Returns
         -------
        `True` if the current class provides a :class:`boto3.Session` object, otherwise `False` : `bool`
        """
        return False


class ABCAWSCredentialsHolder(ABC):
    """
    Abstract base class used to store AWS credentials provided by user.
    """

    def __init__(self: "ABCAWSCredentialsHolder", session: "boto3.Session"):
        self.boto_session = session

    @property
    def has_associated_session(self: "ABCAWSCredentialsHolder") -> bool:
        return True

    def get_boto_session(self: "ABCAWSCredentialsHolder") -> "boto3.Session":
        """
        The :class:`boto3.Session` created using the end-user's AWS Credentials.
        Returns
        -------
        A boto3 session created with the end-user's AWS Credentials: :class:`boto3.Session`
        """
        return self.boto_session

    @abstractmethod
    def get_session_credentials(self: "ABCAWSCredentialsHolder") -> typing.Dict[str, str]:
        pass


class AWSDirectCredentialsHolder(ABCAWSCredentialsHolder):
    """
    Credential class used to store AWS credentials provided in :func:`~redshift_connector.connect`.
    """

    def __init__(
        self,
        access_key_id: str,
        secret_access_key: str,
        session_token: typing.Optional[str],
        session: "boto3.Session",
    ):
        super().__init__(session)
        self.access_key_id: str = access_key_id
        self.secret_access_key: str = secret_access_key
        self.session_token: typing.Optional[str] = session_token
        self._session: "boto3.Session" = session

    def get_session_credentials(
        self: "AWSDirectCredentialsHolder",
    ) -> typing.Dict[str, str]:
        creds: typing.Dict[str, str] = {
            "aws_access_key_id": self.access_key_id,
            "aws_secret_access_key": self.secret_access_key,
        }

        if self.session_token is not None:
            creds["aws_session_token"] = self.session_token

        return creds


class AWSProfileCredentialsHolder(ABCAWSCredentialsHolder):
    """
    Credential class used to store AWS Credentials provided in environment IAM credentials.
    """

    def __init__(self, profile: str, session: "boto3.Session"):
        super().__init__(session)
        self.profile = profile

    def get_session_credentials(
        self: "AWSProfileCredentialsHolder",
    ) -> typing.Dict[str, str]:
        return {"profile": self.profile}


class CredentialsHolder(ABCCredentialsHolder):
    """
    credentials class used to store credentials and metadata from SAML assertion.
    """

    def __init__(self: "CredentialsHolder", credentials: typing.Dict[str, typing.Any]) -> None:
        self.metadata: "CredentialsHolder.IamMetadata" = CredentialsHolder.IamMetadata()
        self.credentials: typing.Dict[str, typing.Any] = credentials
        self.expiration: "datetime.datetime" = credentials["Expiration"]

    def set_metadata(self: "CredentialsHolder", metadata: "IamMetadata") -> None:
        self.metadata = metadata

    def get_metadata(self: "CredentialsHolder") -> "CredentialsHolder.IamMetadata":
        return self.metadata

    # The AWS Access Key ID for this credentials object.
    def get_aws_access_key_id(self: "CredentialsHolder") -> str:
        return typing.cast(str, self.credentials["AccessKeyId"])

    # The AWS secret access key that can be used to sign requests.
    def get_aws_secret_key(self: "CredentialsHolder") -> str:
        return typing.cast(str, self.credentials["SecretAccessKey"])

    # The token that users must pass to the service API to use the temporary credentials.
    def get_session_token(self: "CredentialsHolder") -> str:
        return typing.cast(str, self.credentials["SessionToken"])

    def get_session_credentials(self: "CredentialsHolder") -> typing.Dict[str, str]:
        return {
            "aws_access_key_id": self.get_aws_access_key_id(),
            "aws_secret_access_key": self.get_aws_secret_key(),
            "aws_session_token": self.get_session_token(),
        }

    # The date on which the current credentials expire.
    def get_expiration(self: "CredentialsHolder") -> datetime.datetime:
        return self.expiration

    def is_expired(self: "CredentialsHolder") -> bool:
        _logger.debug("AWS Credentials will expire at %s (UTC)", self.expiration)

        return datetime.datetime.now(datetime.timezone.utc) > self.expiration

    class IamMetadata:
        """
        Metadata used to store information from SAML assertion
        """

        def __init__(self: "CredentialsHolder.IamMetadata") -> None:
            self.auto_create: bool = False
            self.db_user: typing.Optional[str] = None
            self.saml_db_user: typing.Optional[str] = None
            self.profile_db_user: typing.Optional[str] = None
            self.db_groups: typing.List[str] = list()
            self.allow_db_user_override: bool = False
            self.force_lowercase: bool = False

        def get_auto_create(self: "CredentialsHolder.IamMetadata") -> bool:
            return self.auto_create

        def set_auto_create(self: "CredentialsHolder.IamMetadata", auto_create: str) -> None:
            _logger.debug("CredentialsHolder.IamMetadata.set_auto_create %s", auto_create)
            if auto_create.lower() == "true":
                self.auto_create = True
            else:
                self.auto_create = False

        def get_db_user(self: "CredentialsHolder.IamMetadata") -> typing.Optional[str]:
            return self.db_user

        def set_db_user(self: "CredentialsHolder.IamMetadata", db_user: str) -> None:
            _logger.debug("CredentialsHolder.IamMetadata.set_db_user %s", db_user)
            self.db_user = db_user

        def get_saml_db_user(
            self: "CredentialsHolder.IamMetadata",
        ) -> typing.Optional[str]:
            return self.saml_db_user

        def set_saml_db_user(self: "CredentialsHolder.IamMetadata", saml_db_user: str) -> None:
            _logger.debug("CredentialsHolder.IamMetadata.set_saml_db_user %s", saml_db_user)
            self.saml_db_user = saml_db_user

        def get_profile_db_user(
            self: "CredentialsHolder.IamMetadata",
        ) -> typing.Optional[str]:
            return self.profile_db_user

        def set_profile_db_user(self: "CredentialsHolder.IamMetadata", profile_db_user: str) -> None:
            _logger.debug("CredentialsHolder.IamMetadata.set_profile_db_user %s", profile_db_user)
            self.profile_db_user = profile_db_user

        def get_db_groups(self: "CredentialsHolder.IamMetadata") -> typing.List[str]:
            return self.db_groups

        def set_db_groups(self: "CredentialsHolder.IamMetadata", db_groups: typing.List[str]) -> None:
            _logger.debug("CredentialsHolder.IamMetadata.set_db_groups %s", db_groups)
            self.db_groups = db_groups

        def get_allow_db_user_override(self: "CredentialsHolder.IamMetadata") -> bool:
            return self.allow_db_user_override

        def set_allow_db_user_override(self: "CredentialsHolder.IamMetadata", allow_db_user_override: str) -> None:
            _logger.debug("CredentialsHolder.IamMetadata.set_allow_db_user_override %s", allow_db_user_override)
            if allow_db_user_override.lower() == "true":
                self.allow_db_user_override = True
            else:
                self.allow_db_user_override = False

        def get_force_lowercase(self: "CredentialsHolder.IamMetadata") -> bool:
            return self.force_lowercase

        def set_force_lowercase(self: "CredentialsHolder.IamMetadata", force_lowercase: str) -> None:
            _logger.debug("CredentialsHolder.IamMetadata.set_allow_db_user_override %s", force_lowercase)
            if force_lowercase.lower() == "true":
                self.force_lowercase = True
            else:
                self.force_lowercase = False
