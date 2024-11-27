import logging
import typing
from enum import Enum

from packaging.version import Version

from redshift_connector.error import InterfaceError, ProgrammingError
from redshift_connector.plugin.i_plugin import IPlugin
from redshift_connector.redshift_property import RedshiftProperty

logging.getLogger(__name__).addHandler(logging.NullHandler())
_logger: logging.Logger = logging.getLogger(__name__)


class SupportedSSLMode(Enum):
    """
    Definition of SSL modes supported by Amazon Redshift.
    """

    VERIFY_CA: str = "verify-ca"
    VERIFY_FULL: str = "verify-full"

    @staticmethod
    def default() -> str:
        return SupportedSSLMode.VERIFY_CA.value

    @staticmethod
    def list() -> typing.List[str]:
        return list(map(lambda mode: mode.value, SupportedSSLMode))


class IdpAuthHelper:
    # Subtype of plugin
    SAML_PLUGIN: int = 1
    JWT_PLUGIN: int = 2
    IDC_PLUGIN: int = 3

    @staticmethod
    def get_pkg_version(module_name: str) -> Version:
        """
        Returns a Version object pertaining to the module name provided.
        """
        try:
            from importlib.metadata import version as version
        except ModuleNotFoundError:  # if importlib is not present, fallback to pkg_resources
            import pkg_resources

            return Version(pkg_resources.get_distribution(module_name).version)

        return Version(version(module_name))

    @staticmethod
    def set_auth_properties(info: RedshiftProperty):
        """
        Helper function to handle IAM and Native Auth connection properties and ensure required parameters are specified.
        Parameters
        """

        if info is None:
            raise InterfaceError("Invalid connection property setting. info must be specified")

        # IAM requires an SSL connection to work.
        # Make sure that is set to SSL level VERIFY_CA or higher.
        if info.ssl is True:
            if info.sslmode not in SupportedSSLMode.list():
                info.put("sslmode", SupportedSSLMode.default())
                _logger.debug(
                    "A non-supported value: %s was provided for sslmode. Falling back to default value: %s",
                    info.sslmode,
                    SupportedSSLMode.default(),
                )
        else:
            info.put("sslmode", "")

        # elif (info.iam is False) and any(
        #     (info.credentials_provider, info.access_key_id, info.secret_access_key, info.session_token, info.profile)
        # ):
        #     raise InterfaceError(
        #         "Invalid connection property setting. IAM must be enabled when using credential_provider, "
        #         "AWS credentials, Amazon Redshift authentication profile, or AWS profile"
        #     )
        if info.iam is True:
            _logger.debug("boto3 version: %s", IdpAuthHelper.get_pkg_version("boto3"))
            _logger.debug("botocore version: %s", IdpAuthHelper.get_pkg_version("botocore"))

            # Check for IAM keys and AuthProfile first
            if info.auth_profile is not None:
                if IdpAuthHelper.get_pkg_version("boto3") < Version("1.17.111"):
                    raise ModuleNotFoundError(
                        "boto3 >= 1.17.111 required for authentication via Amazon Redshift authentication profile. "
                        "Please upgrade the installed version of boto3 to use this functionality."
                    )

                if not all((info.access_key_id, info.secret_access_key, info.region)):
                    raise InterfaceError(
                        "Invalid connection property setting. access_key_id, secret_access_key, and region are required "
                        "for authentication via Redshift auth_profile"
                    )
                else:
                    # info.put("region", info.region)
                    # info.put("endpoint_url", info.endpoint_url)

                    resp = IdpAuthHelper.read_auth_profile(
                        auth_profile=typing.cast(str, info.auth_profile),
                        iam_access_key_id=typing.cast(str, info.access_key_id),
                        iam_secret_key=typing.cast(str, info.secret_access_key),
                        iam_session_token=info.session_token,
                        info=info,
                    )
                    info.put_all(resp)

            if info.cluster_identifier is None and not info._is_serverless and not info.is_cname:
                raise InterfaceError(
                    "Invalid connection property setting. cluster_identifier must be provided when IAM is enabled"
                )

            if info.credentials_provider is not None:
                if info.auth_profile is None and any(
                    (info.access_key_id, info.secret_access_key, info.session_token, info.profile)
                ):
                    raise InterfaceError(
                        "Invalid connection property setting. It is not valid to provide both Credentials provider and "
                        "AWS credentials or AWS profile"
                    )
                elif not isinstance(info.credentials_provider, str):
                    raise InterfaceError(
                        "Invalid connection property setting. It is not valid to provide a non-string value to "
                        "credentials_provider."
                    )
            elif info.profile is not None:
                if info.auth_profile is None and any((info.access_key_id, info.secret_access_key, info.session_token)):
                    raise InterfaceError(
                        "Invalid connection property setting. It is not valid to provide any of access_key_id, "
                        "secret_access_key, or session_token when profile is provided"
                    )
            elif info.access_key_id is not None:
                if info.secret_access_key is not None:
                    pass
                elif info.password != "":
                    info.put("secret_access_key", info.password)
                    _logger.debug(
                        "Connection parameter secret_access_key was empty. The value of password will be used for secret_access_key"
                    )
                else:
                    raise InterfaceError(
                        "Invalid connection property setting. "
                        "secret access key must be provided in either secret_access_key or password field"
                    )

                _logger.debug(
                    "Are AWS Credentials present? access_key_id: %s secret_access_key: %s session_token: %s",
                    bool(info.access_key_id),
                    bool(info.secret_access_key),
                    bool(info.session_token),
                )
            elif info.secret_access_key is not None:
                raise InterfaceError(
                    "Invalid connection property setting. access_key_id is required when secret_access_key is "
                    "provided"
                )
            elif info.session_token is not None:
                raise InterfaceError(
                    "Invalid connection property setting. access_key_id and secret_access_key are  required when "
                    "session_token is provided"
                )

        if info.db_groups and info.force_lowercase:
            info.put("db_groups", [group.lower() for group in info.db_groups])

    @staticmethod
    def read_auth_profile(
        auth_profile: str,
        iam_access_key_id: str,
        iam_secret_key: str,
        iam_session_token: typing.Optional[str],
        info: RedshiftProperty,
    ) -> RedshiftProperty:
        import json

        import boto3
        from botocore.exceptions import ClientError

        # 1st phase - authenticate with boto3 client for Amazon Redshift via IAM
        # credentials provided by end user
        creds: typing.Dict[str, str] = {
            "aws_access_key_id": iam_access_key_id,
            "aws_secret_access_key": iam_secret_key,
            "region_name": typing.cast(str, info.region),
        }

        for opt_key, opt_val in (
            ("aws_session_token", iam_session_token),
            ("endpoint_url", info.endpoint_url),
        ):
            if opt_val is not None and opt_val != "":
                creds[opt_key] = opt_val

        try:
            _logger.debug("Establishing boto3 redshift client")
            client = boto3.client(service_name="redshift", **creds)
            _logger.debug("Requesting authentication profile: %s", auth_profile)
            # 2nd phase - request Amazon Redshift authentication profiles and record contents for retrieving
            # temporary credentials for the Amazon Redshift cluster specified by end user
            response = client.describe_authentication_profiles(AuthenticationProfileName=auth_profile)
        except ClientError as e:
            raise InterfaceError(e)

        _logger.debug("Received %s authentication profiles", len(response["AuthenticationProfiles"]))
        # the first matching authentication profile will be used
        profile_content: typing.Union[str] = response["AuthenticationProfiles"][0]["AuthenticationProfileContent"]

        try:
            profile_content_dict: typing.Dict = json.loads(profile_content)
            return RedshiftProperty(**profile_content_dict)
        except ValueError:
            raise ProgrammingError(
                "Unable to decode the JSON content of the Redshift authentication profile: {}".format(auth_profile)
            )

    @staticmethod
    def load_credentials_provider(info: RedshiftProperty) -> IPlugin:
        if not info.credentials_provider:
            raise InterfaceError("No value for credentials_provider was given")
        try:
            klass = dynamic_plugin_import(info.credentials_provider)
        except (AttributeError, ModuleNotFoundError):
            _logger.debug(
                "Failed to load user defined IdP specified in credential_provider connection parameters: %s",
                info.credentials_provider,
            )
            try:
                predefined_idp: str = "redshift_connector.plugin.{}".format(info.credentials_provider)
                klass = dynamic_plugin_import(predefined_idp)
                info.put("credentials_provider", predefined_idp)
            except (AttributeError, ModuleNotFoundError):
                _logger.debug(
                    "Failed to load pre-defined IdP plugin from redshift_connector.plugin: %s",
                    info.credentials_provider,
                )
                raise InterfaceError(
                    "Invalid IdP specified in credential_provider connection parameter: " + info.credentials_provider
                )

        if not issubclass(klass, IPlugin):
            raise InterfaceError("Invalid value passed to credentials_provider: {}".format(info.credentials_provider))
        else:
            provider = klass()  # type: ignore
            provider.add_parameter(info)  # type: ignore
        return provider


def dynamic_plugin_import(name: str):
    components = name.split(".")
    mod = __import__(components[0])
    for comp in components[1:]:
        mod = getattr(mod, comp)
    return mod
