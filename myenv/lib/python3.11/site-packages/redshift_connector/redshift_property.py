import logging
import typing

from redshift_connector.config import DEFAULT_PROTOCOL_VERSION

SERVERLESS_HOST_PATTERN: str = r"(.+)\.(.+).redshift-serverless(-dev)?\.amazonaws\.com(.)*"
SERVERLESS_WITH_WORKGROUP_HOST_PATTERN: str = r"(.+)\.(.+)\.(.+).redshift-serverless(-dev)?\.amazonaws\.com(.)*"
IAM_URL_PATTERN: str = r"^(https)://[-a-zA-Z0-9+&@#/%?=~_!:,.']*[-a-zA-Z0-9+&@#/%=~_']"
PROVISIONED_HOST_PATTERN: str = r"(.+)\.(.+)\.(.+).redshift(-dev)?\.amazonaws\.com(.)*"

_logger: logging.Logger = logging.getLogger(__name__)


class RedshiftProperty:
    def __init__(self: "RedshiftProperty", **kwargs):
        """
        Initialize a RedshiftProperty object.
        """
        if not kwargs:
            # The access key for the IAM role or IAM user configured for IAM database authentication
            self.access_key_id: typing.Optional[str] = None
            # This option specifies whether the driver uses the DbUser value from the SAML assertion
            # or the value that is specified in the DbUser connection property in the connection URL.
            self.allow_db_user_override: bool = False
            # The Okta-provided unique ID associated with your Redshift application.
            self.app_id: typing.Optional[str] = None
            # The name of the Okta application that you use to authenticate the connection to Redshift.
            self.app_name: str = "amazon_aws_redshift"
            self.application_name: typing.Optional[str] = None
            self.auth_profile: typing.Optional[str] = None
            # Indicates whether the user should be created if it does not already exist.
            self.auto_create: bool = False
            # The client ID associated with the user name in the Azure AD portal. Only used for Azure AD.
            self.client_id: typing.Optional[str] = None
            # client's requested transfer protocol version. See config.py for supported protocols
            self.client_protocol_version: int = DEFAULT_PROTOCOL_VERSION
            # The client secret as associated with the client ID in the AzureAD portal. Only used for Azure AD.
            self.client_secret: typing.Optional[str] = None
            # The name of the Redshift Cluster to use.
            self.cluster_identifier: typing.Optional[str] = None
            # The class path to a specific credentials provider plugin class.
            self.credentials_provider: typing.Optional[str] = None
            # Boolean indicating if application supports multidatabase datashare catalogs.
            # Default value of True indicates the application is does not support multidatabase datashare
            # catalogs for backwards compatibility.
            self.database_metadata_current_db_only: bool = True
            # A list of existing database group names that the DbUser joins for the current session.
            # If not specified, defaults to PUBLIC.
            self.db_groups: typing.List[str] = list()
            # database name
            self.db_name: str = ""
            # The user name.
            self.db_user: typing.Optional[str] = None
            # The length of time, in seconds
            self.duration: int = 900
            self.endpoint_url: typing.Optional[str] = None
            # Forces the database group names to be lower case.
            self.force_lowercase: bool = False
            # The host to connect to.
            self.host: str = ""
            self.iam: bool = False
            self.iam_disable_cache: bool = False
            self.idc_client_display_name: typing.Optional[str] = None
            self.idc_region: typing.Optional[str] = None
            self.identity_namespace: typing.Optional[str] = None
            # The IdP (identity provider) host you are using to authenticate into Redshift.
            self.idp_host: typing.Optional[str] = None
            # timeout for authentication via Browser IDP
            self.idp_response_timeout: int = 120
            # The Azure AD tenant ID for your Redshift application.Only used for Azure AD.
            self.idp_tenant: typing.Optional[str] = None
            # The port used by an IdP (identity provider).
            self.idpPort: int = 443
            self.issuer_url: typing.Optional[str] = None
            self.listen_port: int = 7890
            # property for specifying loginToRp used by AdfsCredentialsProvider
            self.login_to_rp: str = "urn:amazon:webservices"
            self.login_url: typing.Optional[str] = None
            # max number of prepared statements
            self.max_prepared_statements: int = 1000
            # parameter for PingIdentity
            self.partner_sp_id: typing.Optional[str] = None
            # The password.
            self.password: str = ""
            # The port to connect to.
            self.port: int = 5439
            # The IAM role you want to assume during the connection to Redshift.
            self.preferred_role: typing.Optional[str] = None
            # The Amazon Resource Name (ARN) of the SAML provider in IAM that describes the IdP.
            self.principal: typing.Optional[str] = None
            # The name of a profile in a AWS credentials or config file that contains values for connection options
            self.profile: typing.Optional[str] = None
            # The AWS region where the cluster specified by cluster_identifier is located.
            self.region: typing.Optional[str] = None
            # Used to run in streaming replication mode. If your server character encoding is not ascii or utf8,
            # then you need to provide values as bytes
            self.replication: typing.Optional[str] = None
            self.role_arn: typing.Optional[str] = None
            self.role_session_name: typing.Optional[str] = None
            # The secret access key for the IAM role or IAM user configured for IAM database authentication
            self.secret_access_key: typing.Optional[str] = None
            # session_token is required only for an IAM role with temporary credentials.
            # session_token is not used for an IAM user.
            self.session_token: typing.Optional[str] = None
            # The source IP address which initiates the connection to the Amazon Redshift server.
            self.source_address: typing.Optional[str] = None
            # if SSL authentication will be used
            self.ssl: bool = True
            # This property indicates whether the IDP hosts server certificate should be verified.
            self.ssl_insecure: bool = True
            # ssl mode: verify-ca or verify-full.
            self.sslmode: str = "verify-ca"
            # Use this property to enable or disable TCP keepalives.
            self.tcp_keepalive: bool = True
            # This is the time in seconds before the connection to the server will time out.
            self.timeout: typing.Optional[int] = None
            self.token: typing.Optional[str] = None
            self.token_type: typing.Optional[str] = None
            # The path to the UNIX socket to access the database through
            self.unix_sock: typing.Optional[str] = None
            # The user name.
            self.user_name: str = ""
            self.web_identity_token: typing.Optional[str] = None
            # The name of the Redshift Native Auth Provider
            self.provider_name: typing.Optional[str] = None
            self.scope: str = ""
            self.numeric_to_float: bool = False
            self.is_serverless: bool = False
            self.serverless_acct_id: typing.Optional[str] = None
            self.serverless_work_group: typing.Optional[str] = None
            self.group_federation: bool = False
            # flag indicating if host name and RedshiftProperty indicate Redshift with custom domain name is used
            self.is_cname: bool = False

        else:
            for k, v in kwargs.items():
                setattr(self, k, v)

    def __str__(self: "RedshiftProperty") -> str:
        rp = self.__dict__
        rp["is_serverless_host"] = self.is_serverless_host
        rp["_is_serverless"] = self._is_serverless
        return str(rp)

    def put_all(self, other):
        """
        Merges two RedshiftProperty objects overriding pre-defined attributes with the value provided by other, if present.
        """
        from copy import deepcopy

        for k, v in other.__dict__.items():
            if k in ("is_serverless_host", "_is_serverless"):
                continue
            setattr(self, k, deepcopy(v))

    def put(self: "RedshiftProperty", key: str, value: typing.Any):
        """
        Sets the value of the specified attribute if the value provided is not None.
        """
        if value is not None:
            setattr(self, key, value)

    @property
    def is_serverless_host(self: "RedshiftProperty") -> bool:
        """
        If the host indicate Redshift serverless will be used for connection.
        """

        if not self.host:
            _logger.debug("host field is empty, cannot be serverless host")
            return False

        import re

        return bool(re.fullmatch(pattern=SERVERLESS_HOST_PATTERN, string=str(self.host))) or bool(
            re.fullmatch(pattern=SERVERLESS_WITH_WORKGROUP_HOST_PATTERN, string=str(self.host))
        )

    @property
    def is_provisioned_host(self: "RedshiftProperty") -> bool:
        """
        Returns True if host matches Regex for Redshift provisioned. Otherwise returns False.
        """
        if not self.host:
            return False

        import re

        return bool(re.fullmatch(pattern=PROVISIONED_HOST_PATTERN, string=str(self.host)))

    def set_is_cname(self: "RedshiftProperty") -> None:
        """
        Sets RedshiftProperty is_cname attribute based on RedshiftProperty attribute values and host name Regex matching.
        """
        is_cname: bool = False
        _logger.debug("determining if host indicates Redshift instance with custom name")

        if self.is_provisioned_host:
            _logger.debug("cluster identified as Redshift provisioned")
        elif self.is_serverless_host:
            _logger.debug("cluster identified as Redshift serverless")
        elif self.is_serverless:
            if self.serverless_work_group is not None:
                _logger.debug("cluster identified as Redshift serverless with NLB")
            else:
                _logger.debug("cluster identified as Redshift serverless with with custom name")
                is_cname = True
        else:
            _logger.debug("cluster identified as Redshift provisioned with with custom name/NLB")
            is_cname = True

        self.put(key="is_cname", value=is_cname)

    @property
    def _is_serverless(self: "RedshiftProperty"):
        """
        Returns True if host matches serverless pattern or if is_serverless flag set by user. Otherwise returns False.
        """
        return self.is_serverless_host or self.is_serverless

    def set_serverless_acct_id(self: "RedshiftProperty") -> None:
        """
        Sets the AWS account id as parsed from the Redshift serverless endpoint.
        """
        _logger.debug("RedshiftProperty.set_serverless_acct_id")
        import re

        for serverless_pattern in (SERVERLESS_WITH_WORKGROUP_HOST_PATTERN, SERVERLESS_HOST_PATTERN):
            m2 = re.fullmatch(pattern=serverless_pattern, string=self.host)

            if m2:
                _logger.debug("host matches serverless pattern %s", serverless_pattern)
                self.put(key="serverless_acct_id", value=m2.group(typing.cast(int, m2.lastindex) - 1))
                _logger.debug("serverless_acct_id set to %s", self.region)
                break

    def set_region_from_host(self: "RedshiftProperty") -> None:
        """
        Sets the AWS region as parsed from the Redshift instance endpoint.
        """
        _logger.debug("RedshiftProperty.set_region_from_host")
        import re

        if self.is_serverless_host:
            patterns: typing.Tuple[str, ...] = (SERVERLESS_WITH_WORKGROUP_HOST_PATTERN, SERVERLESS_HOST_PATTERN)
        else:
            patterns = (PROVISIONED_HOST_PATTERN,)

        for host_pattern in patterns:
            m2 = re.fullmatch(pattern=host_pattern, string=self.host)

            if m2:
                _logger.debug("host matches pattern %s", host_pattern)
                self.put(key="region", value=m2.group(typing.cast(int, m2.lastindex)))
                _logger.debug("region set to %s", self.region)
                break

    def set_region_from_endpoint_lookup(self: "RedshiftProperty") -> None:
        """
        Sets the AWS region as determined from a DNS lookup of the Redshift instance endpoint.
        """
        import socket

        _logger.debug("set_region_from_endpoint_lookup")

        if not all((self.host, self.port)):
            _logger.debug("host and port were unspecified, exiting set_region_from_endpoint_lookup")
            return
        try:
            addr_response: typing.List[
                typing.Tuple[
                    socket.AddressFamily,
                    socket.SocketKind,
                    int,
                    str,
                    typing.Union[typing.Tuple[str, int], typing.Tuple[str, int, int, int]],
                ]
            ] = socket.getaddrinfo(host=self.host, port=self.port, family=socket.AF_INET)
            _logger.debug("%s", addr_response)
            host_response: typing.Tuple[str, typing.List, typing.List] = socket.gethostbyaddr(addr_response[0][4][0])
            ec2_instance_host: str = host_response[0]
            _logger.debug("underlying ec2 instance host %s", ec2_instance_host)
            ec2_region: str = ec2_instance_host.split(".")[1]

            # https://docs.aws.amazon.com/vpc/latest/userguide/vpc-dns.html#vpc-dns-hostnames
            if ec2_region == "compute-1":
                ec2_region = "us-east-1"
            self.put(key="region", value=ec2_region)
        except:
            msg: str = "Unable to automatically determine AWS region from host {} port {}. Please check host and port connection parameters are correct.".format(
                self.host, self.port
            )
            _logger.debug(msg)

    def set_serverless_work_group_from_host(self: "RedshiftProperty") -> None:
        """
        Sets the work_group as parsed from the Redshift serverless endpoint.
        """
        _logger.debug("RedshiftProperty.set_serverless_work_group_from_host")
        import re

        m2 = re.fullmatch(pattern=SERVERLESS_WITH_WORKGROUP_HOST_PATTERN, string=self.host)

        if m2:
            _logger.debug("host matches serverless pattern %s", m2)
            self.put(key="serverless_work_group", value=m2.group(1))
            _logger.debug("serverless_work_group set to %s", self.region)
