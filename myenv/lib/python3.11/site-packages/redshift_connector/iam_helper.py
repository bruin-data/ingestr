import datetime
import enum
import logging
import typing

from dateutil.tz import tzutc
from packaging.version import Version

from redshift_connector.auth.aws_credentials_provider import AWSCredentialsProvider
from redshift_connector.credentials_holder import (
    ABCAWSCredentialsHolder,
    AWSDirectCredentialsHolder,
    AWSProfileCredentialsHolder,
    CredentialsHolder,
)
from redshift_connector.error import InterfaceError, ProgrammingError
from redshift_connector.idp_auth_helper import IdpAuthHelper
from redshift_connector.native_plugin_helper import NativeAuthPluginHelper
from redshift_connector.plugin.i_native_plugin import INativePlugin
from redshift_connector.plugin.i_plugin import IPlugin
from redshift_connector.plugin.saml_credentials_provider import SamlCredentialsProvider
from redshift_connector.redshift_property import RedshiftProperty

_logger: logging.Logger = logging.getLogger(__name__)


class IamHelper(IdpAuthHelper):
    class IAMAuthenticationType(enum.Enum):
        """
        Defines authentication types supported by redshift-connector
        """

        NONE = enum.auto()
        PROFILE = enum.auto()
        IAM_KEYS_WITH_SESSION = enum.auto()
        IAM_KEYS = enum.auto()
        PLUGIN = enum.auto()

    class GetClusterCredentialsAPIType(enum.Enum):
        """
        Defines supported Python SDK methods used for Redshift credential retrieval
        """

        # https://boto3.amazonaws.com/v1/documentation/api/latest/reference/services/redshift-serverless/client/get_credentials.html#
        SERVERLESS_V1 = "get_credentials()"
        # https://boto3.amazonaws.com/v1/documentation/api/latest/reference/services/redshift/client/get_cluster_credentials.html
        IAM_V1 = "get_cluster_credentials()"
        # https://boto3.amazonaws.com/v1/documentation/api/latest/reference/services/redshift/client/get_cluster_credentials_with_iam.html#
        IAM_V2 = "get_cluster_credentials_with_iam()"

        @staticmethod
        def can_support_v2(provider_type: "IamHelper.IAMAuthenticationType") -> bool:
            """
            Determines if user provided connection options and boto3 version support group federation.
            """
            return (
                provider_type
                in (
                    IamHelper.IAMAuthenticationType.PROFILE,
                    IamHelper.IAMAuthenticationType.IAM_KEYS,
                    IamHelper.IAMAuthenticationType.IAM_KEYS_WITH_SESSION,
                    IamHelper.IAMAuthenticationType.PLUGIN,
                )
            ) and IdpAuthHelper.get_pkg_version("boto3") >= Version("1.24.5")

    credentials_cache: typing.Dict[str, dict] = {}

    @staticmethod
    def get_cluster_credentials_api_type(
        info: RedshiftProperty, provider_type: "IamHelper.IAMAuthenticationType"
    ) -> GetClusterCredentialsAPIType:
        """
        Returns an enum representing the Python SDK method to use for getting temporary IAM credentials.
        """
        _logger.debug("Determining which Redshift API to use for retrieving temporary Redshift instance credentials")
        FAILED_TO_USE_V2_API_ERROR_MSG: str = (
            "Environment does not meet requirements to use {} API. "
            "This could be due to the connection properties provided or the version of boto3 in use. "
            "Please try updating the boto3 version or consider setting group_federation connection parameter to False."
        )

        if not info._is_serverless:
            _logger.debug("Redshift provisioned")
            if not info.group_federation:
                _logger.debug("Provisioned cluster GetClusterCredentialsAPIType.IAM_V1")
                return IamHelper.GetClusterCredentialsAPIType.IAM_V1
            elif IamHelper.GetClusterCredentialsAPIType.can_support_v2(provider_type):
                _logger.debug("Provisioned cluster GetClusterCredentialsAPIType.IAM_V2")
                return IamHelper.GetClusterCredentialsAPIType.IAM_V2
            else:
                raise InterfaceError(FAILED_TO_USE_V2_API_ERROR_MSG.format("GetClusterCredentials V2 API"))
        elif not info.group_federation:
            _logger.debug("Serverless cluster GetClusterCredentialsAPIType.SERVERLESS_V1")
            return IamHelper.GetClusterCredentialsAPIType.SERVERLESS_V1
        elif IamHelper.GetClusterCredentialsAPIType.can_support_v2(provider_type):
            if info.is_cname:
                raise InterfaceError("Custom cluster names are not supported for Redshift Serverless")
            else:
                _logger.debug("Serverless cluster GetClusterCredentialsAPIType.IAM_V2")
                return IamHelper.GetClusterCredentialsAPIType.IAM_V2
        else:
            raise InterfaceError(FAILED_TO_USE_V2_API_ERROR_MSG.format("GetClusterCredentials V2 API"))

    @staticmethod
    def set_iam_properties(info: RedshiftProperty) -> RedshiftProperty:
        """
        Helper function to handle connection properties and ensure required parameters are specified.
        Parameters
        """
        _logger.debug("IamHelper.set_iam_properties")
        provider_type: IamHelper.IAMAuthenticationType = IamHelper.IAMAuthenticationType.NONE
        info.set_is_cname()
        # set properties present for both IAM, Native authentication
        IamHelper.set_auth_properties(info)

        if info._is_serverless and info.iam:
            if IdpAuthHelper.get_pkg_version("boto3") < Version("1.24.11"):
                raise ModuleNotFoundError(
                    "boto3 >= 1.24.11 required for authentication with Amazon Redshift serverless. "
                    "Please upgrade the installed version of boto3 to use this functionality."
                )

        if info.iam and info.is_cname:
            if IdpAuthHelper.get_pkg_version("boto3") < Version("1.26.157"):
                _logger.debug(
                    "boto3 >= 1.26.157 required for authentication with Amazon Redshift using custom domain name. "
                    "Please upgrade the installed version of boto3 to use this functionality."
                )

        # consider overridden connection parameters
        if info.is_serverless_host:
            _logger.debug("Redshift Serverless host detected")
            if not info.serverless_acct_id:
                info.set_serverless_acct_id()
            if not info.serverless_work_group:
                info.set_serverless_work_group_from_host()
        if not info.region:
            info.set_region_from_host()

        if info.iam is True:
            if info.region is None:
                _logger.debug("Setting region via DNS lookup as region was not provided in connection parameters")
                info.set_region_from_endpoint_lookup()

            if info.cluster_identifier is None and not info._is_serverless and not info.is_cname:
                raise InterfaceError(
                    "Invalid connection property setting. cluster_identifier must be provided when IAM is enabled"
                )
            IamHelper.set_iam_credentials(info)
        # Check for Browser based OAuth Native authentication
        NativeAuthPluginHelper.set_native_auth_plugin_properties(info)
        return info

    @staticmethod
    def set_iam_credentials(info: RedshiftProperty) -> None:
        """
        Helper function to create the appropriate credential providers.
        """
        _logger.debug("IamHelper.set_iam_credentials")
        klass: typing.Optional[IPlugin] = None
        provider: typing.Union[IPlugin, AWSCredentialsProvider]

        if info.credentials_provider is not None:
            _logger.debug("IdP plugin will be used for authentication")
            provider = IdpAuthHelper.load_credentials_provider(info)

        else:  # indicates AWS Credentials will be used
            _logger.debug("AWS Credentials provider will be used for authentication")
            provider = AWSCredentialsProvider()
            provider.add_parameter(info)

        if isinstance(provider, SamlCredentialsProvider):
            _logger.debug("SAML based credential provider identified")
            credentials: CredentialsHolder = provider.get_credentials()
            metadata: CredentialsHolder.IamMetadata = credentials.get_metadata()
            if metadata is not None:
                _logger.debug("Using SAML metadata to set connection properties")
                auto_create: bool = metadata.get_auto_create()
                db_user: typing.Optional[str] = metadata.get_db_user()
                saml_db_user: typing.Optional[str] = metadata.get_saml_db_user()
                profile_db_user: typing.Optional[str] = metadata.get_profile_db_user()
                db_groups: typing.List[str] = metadata.get_db_groups()
                force_lowercase: bool = metadata.get_force_lowercase()
                allow_db_user_override: bool = metadata.get_allow_db_user_override()
                if auto_create is True:
                    _logger.debug("setting auto_create %s", auto_create)
                    info.put("auto_create", auto_create)

                if force_lowercase is True:
                    _logger.debug("setting force_lowercase %s", force_lowercase)
                    info.put("force_lowercase", force_lowercase)

                if allow_db_user_override is True:
                    _logger.debug("allow_db_user_override enabled")
                    if saml_db_user is not None:
                        _logger.debug("setting db_user to saml_db_user %s", saml_db_user)
                        info.put("db_user", saml_db_user)
                    elif db_user is not None:
                        _logger.debug("setting db_user to db_user %s", db_user)
                        info.put("db_user", db_user)
                    elif profile_db_user is not None:
                        _logger.debug("setting db_user to profile_db_user %s", profile_db_user)
                        info.put("db_user", profile_db_user)
                else:
                    if db_user is not None:
                        _logger.debug("setting db_user to db_user %s", db_user)
                        info.put("db_user", db_user)
                    elif profile_db_user is not None:
                        _logger.debug("setting db_user to profile_db_user %s", profile_db_user)
                        info.put("db_user", profile_db_user)
                    elif saml_db_user is not None:
                        _logger.debug("setting db_user to saml_db_user %s", saml_db_user)
                        info.put("db_user", saml_db_user)

                if (len(info.db_groups) == 0) and (len(db_groups) > 0):
                    if force_lowercase:
                        _logger.debug("setting db_groups after cast to lowercase")
                        info.db_groups = [group.lower() for group in db_groups]
                    else:
                        _logger.debug("setting db_groups")
                        info.db_groups = db_groups

        if not isinstance(provider, INativePlugin):
            # If the Redshift instance has been identified as using a custom domain name, the hostname must
            # be determined using the redshift client from boto3 API
            if info.is_cname is True and not info.is_serverless:
                IamHelper.set_cluster_identifier(provider, info)

            # Redshift database credentials  will be determined using the redshift client from boto3 API
            IamHelper.set_cluster_credentials(provider, info)

            # Redshift instance host and port must be retrieved
            IamHelper.set_cluster_host_and_port(provider, info)

    @staticmethod
    def get_credentials_cache_key(info: RedshiftProperty, cred_provider: typing.Union[IPlugin, AWSCredentialsProvider]):
        db_groups: str = ""

        if len(info.db_groups) > 0:
            info.put("db_groups", sorted(info.db_groups))
            db_groups = ",".join(info.db_groups)

        cred_key: str = ""

        if cred_provider:
            cred_key = str(cred_provider.get_cache_key())

        return ";".join(
            filter(
                None,
                (
                    cred_key,
                    typing.cast(str, info.db_user if info.db_user else info.user_name),
                    info.db_name,
                    db_groups,
                    typing.cast(str, info.serverless_acct_id if info._is_serverless else info.cluster_identifier),
                    typing.cast(
                        str, info.serverless_work_group if info._is_serverless and info.serverless_work_group else ""
                    ),
                    str(info.auto_create),
                    str(info.duration),
                    # v2 api parameters
                    info.preferred_role,
                    info.web_identity_token,
                    info.role_arn,
                    info.role_session_name,
                    # providers
                    info.profile,
                    info.access_key_id,
                    info.secret_access_key,
                    info.session_token,
                ),
            )
        )

    @staticmethod
    def get_authentication_type(
        provider: typing.Union[IPlugin, AWSCredentialsProvider]
    ) -> "IamHelper.IAMAuthenticationType":
        """
        Returns an enum representing the type of authentication the user is requesting based on connection parameters.
        """
        _logger.debug("IamHelper.get_authentication_type")
        provider_type: IamHelper.IAMAuthenticationType = IamHelper.IAMAuthenticationType.NONE
        if isinstance(provider, IPlugin):
            provider_type = IamHelper.IAMAuthenticationType.PLUGIN
        elif isinstance(provider, AWSCredentialsProvider):
            if provider.profile is not None:
                provider_type = IamHelper.IAMAuthenticationType.PROFILE
            elif provider.session_token is not None:
                provider_type = IamHelper.IAMAuthenticationType.IAM_KEYS_WITH_SESSION
            else:
                provider_type = IamHelper.IAMAuthenticationType.IAM_KEYS
        _logger.debug("Inferred authentication type %s from connection parameters", provider_type)

        return provider_type

    @staticmethod
    def get_boto3_redshift_client(cred_provider: typing.Union[IPlugin, AWSCredentialsProvider], info: RedshiftProperty):
        """
        Returns a boto3 client configured for Amazon Redshift using AWS credentials provided by user, system, or IdP.
        """
        _logger.debug("IamHelper.set_cluster_credentials")
        import boto3  # type: ignore
        import botocore  # type: ignore

        session_args: typing.Dict[str, str] = {
            "service_name": "redshift-serverless" if info._is_serverless else "redshift"
        }
        for opt_key, opt_val in (("region_name", info.region), ("endpoint_url", info.endpoint_url)):
            if opt_val is not None:
                session_args[opt_key] = opt_val

        try:
            credentials_holder: typing.Union[
                CredentialsHolder, ABCAWSCredentialsHolder
            ] = cred_provider.get_credentials()  # type: ignore
            session_credentials: typing.Dict[str, str] = credentials_holder.get_session_credentials()

            _logger.debug("boto3.client(service_name=%s) being used for IAM auth", session_args["service_name"])

            # if AWS credentials were used to create a boto3.Session object, use it
            if credentials_holder.has_associated_session:
                _logger.debug("Using cached boto3 session")
                cached_session: boto3.Session = typing.cast(
                    ABCAWSCredentialsHolder, credentials_holder
                ).get_boto_session()

                client = cached_session.client(**session_args)

            else:
                client = boto3.client(**{**session_credentials, **session_args})
            return client
        except botocore.exceptions.ClientError as e:
            _logger.debug("ClientError when establishing boto3 client: %s", e)
            raise e
        except Exception as e:
            _logger.debug("Other Exception when establishing boto3 client: %s", e)
            raise e

    @staticmethod
    def set_cluster_identifier(
        cred_provider: typing.Union[IPlugin, AWSCredentialsProvider], info: RedshiftProperty
    ) -> None:
        """
        Retrieves the hostname of a Redshift instance using custom domain name using boto3 API
        """
        import boto3  # type: ignore
        import botocore  # type: ignore

        client = IamHelper.get_boto3_redshift_client(cred_provider, info)

        try:
            _logger.debug("Redshift custom domain name in use. Determining cluster identifier.")
            response = client.describe_custom_domain_associations(CustomDomainName=info.host)
            cluster_identifier: str = response["Associations"][0]["CertificateAssociations"][0]["ClusterIdentifier"]
            _logger.debug("Retrieved cluster_identifier=%s", cluster_identifier)
            info.put(key="cluster_identifier", value=cluster_identifier)
        except Exception as e:
            if info.cluster_identifier is None or info.cluster_identifier == "":
                _logger.debug(
                    "Other Exception when requesting cluster identifier for Redshift with custom domain: %s", e
                )
                raise e
            else:
                _logger.debug(
                    "User provided cluster_identifier. Assuming cluster is using NLB/custom domain name. Using cluster_identifier"
                )

    @staticmethod
    def set_cluster_host_and_port(
        cred_provider: typing.Union[IPlugin, AWSCredentialsProvider], info: RedshiftProperty
    ) -> None:
        """
        Sets RedshiftProperty attributes for host and port using user configured connection properties and AWS SDK API calls.
        """
        import boto3  # type: ignore
        import botocore  # type: ignore

        try:
            # we must fetch the Redshift instance host and port name if either are unspecified by the user
            if info.host is None or info.host == "" or info.port is None or info.port == "":
                _logger.debug("retrieving Redshift instance host and port from boto3 redshift client")
                response: dict
                client = IamHelper.get_boto3_redshift_client(cred_provider, info)

                if info._is_serverless:
                    if not info.serverless_work_group:
                        raise InterfaceError("Serverless workgroup is not set.")
                    response = client.get_workgroup(workgroupName=info.serverless_work_group)
                    info.put("host", response["workgroup"]["endpoint"]["address"])
                    info.put("port", response["workgroup"]["endpoint"]["port"])
                else:
                    response = client.describe_clusters(ClusterIdentifier=info.cluster_identifier)
                    info.put("host", response["Clusters"][0]["Endpoint"]["Address"])
                    info.put("port", response["Clusters"][0]["Endpoint"]["Port"])
            _logger.debug("host=%s port=%s", info.host, info.port)
        except botocore.exceptions.ClientError as e:
            _logger.debug("ClientError when requesting cluster identifier for Redshift with custom domain: %s", e)
            raise e
        except Exception as e:
            _logger.debug("Other Exception when requesting cluster identifier for Redshift with custom domain: %s", e)
            raise e

    @staticmethod
    def set_cluster_credentials(
        cred_provider: typing.Union[IPlugin, AWSCredentialsProvider], info: RedshiftProperty
    ) -> None:
        """
        Calls the AWS SDK methods to return temporary credentials.
        The expiration date is returned as the local time set by the client machines OS.
        """
        import boto3  # type: ignore
        import botocore  # type: ignore
        from botocore.exceptions import ClientError

        client = IamHelper.get_boto3_redshift_client(cred_provider, info)
        cred: typing.Optional[typing.Dict[str, typing.Union[str, datetime.datetime]]] = None

        if info.iam_disable_cache is False:
            _logger.debug("iam_disable_cache=False")
            # temporary credentials are cached by redshift_connector and will be used if they have not expired
            cache_key: str = IamHelper.get_credentials_cache_key(info, cred_provider)
            cred = IamHelper.credentials_cache.get(cache_key, None)

            _logger.debug(
                "Searching credential cache for temporary AWS credentials. Found: %s Expiration: %s",
                bool(cache_key in IamHelper.credentials_cache),
                cred["Expiration"] if cred is not None else "N/A",
            )

        if cred is None or typing.cast(datetime.datetime, cred["Expiration"]) < datetime.datetime.now(tz=tzutc()):
            # retries will occur by default ref:
            # https://boto3.amazonaws.com/v1/documentation/api/latest/guide/retries.html#legacy-retry-mode
            _logger.debug("Credentials expired or not found...requesting from boto")
            provider_type: IamHelper.IAMAuthenticationType = IamHelper.get_authentication_type(cred_provider)
            get_creds_api_version: IamHelper.GetClusterCredentialsAPIType = IamHelper.get_cluster_credentials_api_type(
                info, provider_type
            )
            _logger.debug("boto3 get_credentials api version: %s will be used", get_creds_api_version.value)

            if get_creds_api_version == IamHelper.GetClusterCredentialsAPIType.SERVERLESS_V1:
                # https://boto3.amazonaws.com/v1/documentation/api/latest/reference/services/redshift-serverless/client/get_credentials.html#
                get_cred_args: typing.Dict[str, str] = {"dbName": info.db_name}
                # if a connection parameter for serverless workgroup is provided it will
                # be preferred over providing the CustomDomainName. The reason for this
                # is backwards compatibility with the following cases:
                # 0/ Serverless with NLB
                # 1/ Serverless with Custom Domain Name
                # Providing the CustomDomainName parameter to getCredentials will lead to
                # failure if the custom domain name is not registered with Redshift. Hence,
                # the ordering of these conditions is important.
                if info.serverless_work_group:
                    get_cred_args["workgroupName"] = info.serverless_work_group
                elif info.is_cname:
                    get_cred_args["customDomainName"] = info.host
                _logger.debug("Calling get_credentials with parameters %s", get_cred_args)
                cred = typing.cast(
                    typing.Dict[str, typing.Union[str, datetime.datetime]],
                    client.get_credentials(**get_cred_args),
                )
                # re-map expiration for compatibility with redshift credential response
                cred["Expiration"] = cred["expiration"]
                del cred["expiration"]
            elif get_creds_api_version == IamHelper.GetClusterCredentialsAPIType.IAM_V2:
                # https://boto3.amazonaws.com/v1/documentation/api/latest/reference/services/redshift/client/get_cluster_credentials_with_iam.html#
                request_params = {
                    "DbName": info.db_name,
                    "DurationSeconds": info.duration,
                }

                if info.is_cname:
                    request_params["CustomDomainName"] = info.host
                else:
                    request_params["ClusterIdentifier"] = info.cluster_identifier
                _logger.debug("Calling get_cluster_credentials_with_iam with parameters %s", request_params)

                try:
                    cred = typing.cast(
                        typing.Dict[str, typing.Union[str, datetime.datetime]],
                        client.get_cluster_credentials_with_iam(**request_params),
                    )
                except Exception as e:
                    if info.is_cname:
                        _logger.debug(
                            "Failed to get_cluster_credentials_with_iam. Assuming cluster incorrectly classified as cname, retrying..."
                        )
                        del request_params["CustomDomainName"]
                        request_params["ClusterIdentifier"] = info.cluster_identifier

                        _logger.debug(
                            "Retrying calling get_cluster_credentials_with_iam with parameters %s", request_params
                        )

                        cred = typing.cast(
                            typing.Dict[str, typing.Union[str, datetime.datetime]],
                            client.get_cluster_credentials_with_iam(**request_params),
                        )
                    else:
                        raise e

            else:
                if info.db_user is None or info.db_user == "":
                    raise InterfaceError("Connection parameter db_user must be specified when using IAM authentication")
                # https://boto3.amazonaws.com/v1/documentation/api/latest/reference/services/redshift/client/get_cluster_credentials.html
                request_params = {
                    "DbUser": info.db_user,
                    "DbName": info.db_name,
                    "DbGroups": info.db_groups,
                    "AutoCreate": info.auto_create,
                }

                if info.is_cname:
                    request_params["CustomDomainName"] = info.host
                else:
                    request_params["ClusterIdentifier"] = info.cluster_identifier

                _logger.debug("Calling get_cluster_credentials with parameters %s", request_params)

                try:
                    cred = typing.cast(
                        typing.Dict[str, typing.Union[str, datetime.datetime]],
                        client.get_cluster_credentials(**request_params),
                    )
                except Exception as e:
                    if info.is_cname:
                        _logger.debug(
                            "Failed to get_cluster_credentials. Assuming cluster incorrectly classified as cname, retrying..."
                        )
                        del request_params["CustomDomainName"]
                        request_params["ClusterIdentifier"] = info.cluster_identifier

                        _logger.debug("Retrying calling get_cluster_credentials with parameters %s", request_params)

                        cred = typing.cast(
                            typing.Dict[str, typing.Union[str, datetime.datetime]],
                            client.get_cluster_credentials(**request_params),
                        )

                    else:
                        raise e

            if info.iam_disable_cache is False:
                IamHelper.credentials_cache[cache_key] = typing.cast(
                    typing.Dict[str, typing.Union[str, datetime.datetime]], cred
                )
        # redshift-serverless api json response payload slightly differs
        if info._is_serverless:
            info.put("user_name", typing.cast(str, cred["dbUser"]))
            info.put("password", typing.cast(str, cred["dbPassword"]))
        else:
            info.put("user_name", typing.cast(str, cred["DbUser"]))
            info.put("password", typing.cast(str, cred["DbPassword"]))

        _logger.debug("Using temporary aws credentials with expiration: %s", cred.get("Expiration"))
