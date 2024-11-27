import base64
import logging
import random
import re
import typing
from abc import abstractmethod

from redshift_connector.credentials_holder import CredentialsHolder
from redshift_connector.error import InterfaceError
from redshift_connector.idp_auth_helper import IdpAuthHelper
from redshift_connector.plugin.credential_provider_constants import SAML_RESP_NAMESPACES
from redshift_connector.plugin.idp_credentials_provider import IdpCredentialsProvider
from redshift_connector.redshift_property import RedshiftProperty

_logger: logging.Logger = logging.getLogger(__name__)


class SamlCredentialsProvider(IdpCredentialsProvider):
    """
    Generic Identity Provider Plugin providing single sign-on access to an Amazon Redshift cluster using an identity provider of your choice.
    """

    def __init__(self: "SamlCredentialsProvider") -> None:
        super().__init__()
        self.user_name: typing.Optional[str] = None
        self.password: typing.Optional[str] = None
        self.idp_host: typing.Optional[str] = None
        self.idpPort: int = 443
        self.duration: typing.Optional[int] = None
        self.preferred_role: typing.Optional[str] = None
        self.ssl_insecure: typing.Optional[bool] = None
        self.db_user: typing.Optional[str] = None
        self.db_groups: typing.List[str] = list()
        self.force_lowercase: typing.Optional[bool] = None
        self.auto_create: typing.Optional[bool] = None
        self.region: typing.Optional[str] = None
        self.principal: typing.Optional[str] = None
        self.group_federation: bool = False

        self.cache: dict = {}

    def add_parameter(self: "SamlCredentialsProvider", info: RedshiftProperty) -> None:
        self.user_name = info.user_name
        self.password = info.password
        self.idp_host = info.idp_host
        self.idpPort = info.idpPort
        self.duration = info.duration
        self.preferred_role = info.preferred_role
        self.ssl_insecure = info.ssl_insecure
        self.db_user = info.db_user
        self.db_groups = info.db_groups
        self.force_lowercase = info.force_lowercase
        self.auto_create = info.auto_create
        self.region = info.region
        self.principal = info.principal

    def set_group_federation(self: "SamlCredentialsProvider", group_federation: bool):
        self.group_federation = group_federation

    def get_sub_type(self) -> int:
        return IdpAuthHelper.SAML_PLUGIN

    def do_verify_ssl_cert(self: "SamlCredentialsProvider") -> bool:
        return not self.ssl_insecure

    def get_credentials(self: "SamlCredentialsProvider") -> CredentialsHolder:
        _logger.debug("SamlCredentialsProvider.get_credentials")
        key: str = self.get_cache_key()
        if key not in self.cache or self.cache[key].is_expired():
            try:
                self.refresh()
                _logger.debug("Successfully refreshed credentials")
            except Exception as e:
                _logger.debug("Refreshing IdP credentials failed")
                raise InterfaceError(e)
        # if the SAML response has db_user argument, it will be picked up at this point.
        credentials: CredentialsHolder = self.cache[key]

        if credentials is None:
            exec_msg = "Unable to load AWS credentials from IdP"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg)

        # if db_user argument has been passed in the connection string, add it to metadata.
        if self.db_user:
            _logger.debug("adding db_user to metadata")
            credentials.metadata.set_db_user(self.db_user)

        return credentials

    def refresh(self: "SamlCredentialsProvider") -> None:
        _logger.debug("SamlCredentialsProvider.refresh")
        import boto3  # type: ignore
        import bs4  # type: ignore

        try:
            # get SAML assertion from specific identity provider
            saml_assertion = self.get_saml_assertion()
            _logger.debug("Successfully retrieved SAML assertion")
        except Exception as e:
            exec_msg = "Failed to get SAML assertion"
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg) from e
        # decode SAML assertion into xml format
        doc: bytes = base64.b64decode(saml_assertion)
        _logger.debug("decoded SAML assertion into xml format")
        soup = bs4.BeautifulSoup(doc, "xml")
        attrs = soup.findAll("Attribute")
        # extract RoleArn adn PrincipleArn from SAML assertion
        role_pattern = re.compile(r"arn:aws:iam::\d*:role/\S+")
        provider_pattern = re.compile(r"arn:aws:iam::\d*:saml-provider/\S+")
        roles: typing.Dict[str, str] = {}
        _logger.debug("searching SAML assertion for values matching patterns for RoleArn and PrincipalArn")
        for attr in attrs:
            name: str = attr.attrs["Name"]
            values: typing.Any = attr.findAll("AttributeValue")
            if name == "https://aws.amazon.com/SAML/Attributes/Role":
                _logger.debug("Attribute with name %s found. Checking if pattern match occurs", name)
                for value in values:
                    arns = value.contents[0].split(",")
                    role: str = ""
                    provider: str = ""
                    for arn in arns:
                        arn = arn.strip()  # remove trailing or leading whitespace
                        if role_pattern.match(arn):
                            _logger.debug("RoleArn pattern matched")
                            role = arn
                        if provider_pattern.match(arn):
                            _logger.debug("PrincipleArn pattern matched")
                            provider = arn
                    if role != "" and provider != "":
                        roles[role] = provider
        _logger.debug("Done reading SAML assertion attributes")
        _logger.debug("%s roles identified in SAML assertion", len(roles))

        if len(roles) == 0:
            exec_msg = "No roles were found in SAML assertion. Please verify IdP configuration provides ARNs in the SAML https://aws.amazon.com/SAML/Attributes/Role Attribute."
            _logger.debug(exec_msg)
            raise InterfaceError(exec_msg)
        role_arn: str = ""
        principle: str = ""
        if self.preferred_role:
            _logger.debug("User provided preferred_role, trying to use...")
            role_arn = self.preferred_role
            if role_arn not in roles:
                exec_msg = "User specified preferred_role was not found in SAML assertion https://aws.amazon.com/SAML/Attributes/Role Attribute"
                _logger.debug(exec_msg)
                raise InterfaceError(exec_msg)
            principle = roles[role_arn]
        else:
            _logger.debug(
                "User did not specify a preferred_role. A randomly selected role from the SAML assertion https://aws.amazon.com/SAML/Attributes/Role Attribute will be used."
            )
            role_arn = random.choice(list(roles))
            principle = roles[role_arn]

        client = boto3.client("sts")

        try:
            _logger.debug(
                "Attempting to retrieve temporary AWS credentials using the SAML assertion, principal ARN, and role ARN."
            )
            response = client.assume_role_with_saml(
                RoleArn=role_arn,  # self.preferred_role,
                PrincipalArn=principle,  # self.principal,
                SAMLAssertion=saml_assertion,
            )
            _logger.debug("Extracting temporary AWS credentials from assume_role_with_saml response")
            stscred: typing.Dict[str, typing.Any] = response["Credentials"]
            credentials: CredentialsHolder = CredentialsHolder(stscred)
            # get metadata from SAML assertion
            credentials.set_metadata(self.read_metadata(doc))
            key: str = self.get_cache_key()
            self.cache[key] = credentials
        except AttributeError as e:
            _logger.debug("AttributeError: %s", e)
            raise e
        except KeyError as e:
            _logger.debug("KeyError: %s", e)
            raise e
        except client.exceptions.MalformedPolicyDocumentException as e:
            _logger.debug("MalformedPolicyDocumentException: %s", e)
            raise e
        except client.exceptions.PackedPolicyTooLargeException as e:
            _logger.debug("PackedPolicyTooLargeException: %s", e)
            raise e
        except client.exceptions.IDPRejectedClaimException as e:
            _logger.debug("IDPRejectedClaimException: %s", e)
            raise e
        except client.exceptions.InvalidIdentityTokenException as e:
            _logger.debug("InvalidIdentityTokenException: %s", e)
            raise e
        except client.exceptions.ExpiredTokenException as e:
            _logger.debug("ExpiredTokenException: %s", e)
            raise e
        except client.exceptions.RegionDisabledException as e:
            _logger.debug("RegionDisabledException: %s", e)
            raise e
        except Exception as e:
            _logger.debug("Other Exception: %s", e)
            raise e

    def get_cache_key(self: "SamlCredentialsProvider") -> str:
        return "{username}{password}{idp_host}{idp_port}{duration}{preferred_role}".format(
            username=self.user_name,
            password=self.password,
            idp_host=self.idp_host,
            idp_port=self.idpPort,
            duration=self.duration,
            preferred_role=self.preferred_role,
        )

    @abstractmethod
    def get_saml_assertion(self: "SamlCredentialsProvider"):
        pass

    def check_required_parameters(self: "SamlCredentialsProvider") -> None:
        _logger.debug("SamlCredentialsProvider.check_required_parameters")
        if self.user_name == "" or self.user_name is None:
            SamlCredentialsProvider.handle_missing_required_property("user_name")
        if self.password == "" or self.password is None:
            SamlCredentialsProvider.handle_missing_required_property("password")
        if self.idp_host == "" or self.idp_host is None:
            SamlCredentialsProvider.handle_missing_required_property("idp_host")

    def read_metadata(self: "SamlCredentialsProvider", doc: bytes) -> CredentialsHolder.IamMetadata:
        _logger.debug("SamlCredentialsProvider.read_metadata")
        import bs4  # type: ignore

        try:
            soup = bs4.BeautifulSoup(doc, "xml")
            attrs: typing.Any = []
            namespace_used_idx: int = 0

            # prefer using Attributes in saml-compliant namespace
            for idx, namespace in enumerate(SAML_RESP_NAMESPACES):
                _logger.debug("Looking for attributes under %s namespace", namespace)
                attrs = soup.find_all("{}Attribute".format(namespace))
                if len(attrs) > 0:
                    _logger.debug("Attributes found under SAML response namespace %s", namespace)
                    namespace_used_idx = idx
                    break

            metadata: CredentialsHolder.IamMetadata = CredentialsHolder.IamMetadata()

            for attr in attrs:
                name: str = attr.attrs["Name"]
                _logger.debug("Searching SAML attribute %s for attribute values", name)
                attribute_name: str = "{}AttributeValue".format(SAML_RESP_NAMESPACES[namespace_used_idx])
                values: typing.Any = attr.findAll(attribute_name)
                if len(values) == 0 or not values[0].contents:
                    _logger.debug("No SAML attribute %s found. Continuing to search", attribute_name)
                    # Ignore empty-valued attributes.
                    continue
                value: str = values[0].contents[0]

                if name == "https://redshift.amazon.com/SAML/Attributes/AllowDbUserOverride":
                    metadata.set_allow_db_user_override(value)
                elif name == "https://redshift.amazon.com/SAML/Attributes/DbUser":
                    metadata.set_saml_db_user(value)
                elif name == "https://aws.amazon.com/SAML/Attributes/RoleSessionName":
                    if metadata.get_saml_db_user() is None:
                        metadata.set_saml_db_user(value)
                elif name == "https://redshift.amazon.com/SAML/Attributes/AutoCreate":
                    metadata.set_auto_create(value)
                elif name == "https://redshift.amazon.com/SAML/Attributes/DbGroups":
                    metadata.set_db_groups([value.contents[0].lower() for value in values])
                elif name == "https://redshift.amazon.com/SAML/Attributes/ForceLowercase":
                    metadata.set_force_lowercase(value)

            return metadata
        except AttributeError as e:
            _logger.debug("AttributeError: %s", e)
            raise e
        except KeyError as e:
            _logger.debug("KeyError: %s", e)
            raise e

    def get_form_action(self: "SamlCredentialsProvider", soup) -> typing.Optional[str]:
        for inputtag in soup.find_all(re.compile("(FORM|form)")):
            action: str = inputtag.get("action")
            if action:
                return action
        return None

    def is_text(self: "SamlCredentialsProvider", inputtag) -> bool:
        return typing.cast(bool, "text" == inputtag.get("type"))

    def is_password(self: "SamlCredentialsProvider", inputtag) -> bool:
        return typing.cast(bool, "password" == inputtag.get("type"))
