import copy
import socket
import typing

if typing.TYPE_CHECKING:
    from redshift_connector import RedshiftProperty


def make_divider_block() -> str:
    return "=" * 35


def mask_secure_info_in_props(info: "RedshiftProperty") -> "RedshiftProperty":
    from redshift_connector import RedshiftProperty

    logging_allow_list: typing.Tuple[str, ...] = (
        # "access_key_id",
        "allow_db_user_override",
        "app_id",
        "app_name",
        "application_name",
        "auth_profile",
        "auto_create",
        # "client_id",
        "client_protocol_version",
        # "client_secret",
        "cluster_identifier",
        "credentials_provider",
        "database_metadata_current_db_only",
        "db_groups",
        "db_name",
        "db_user",
        "duration",
        "endpoint_url",
        "force_lowercase",
        "group_federation",
        "host",
        "iam",
        "iam_disable_cache",
        "idc_client_display_name",
        "idc_region",
        "identity_namespace",
        "idp_host",
        "idpPort",
        "idp_response_timeout",
        "idp_tenant",
        "issuer_url",
        "is_serverless",
        "listen_port",
        "login_url",
        "max_prepared_statements",
        "numeric_to_float",
        "partner_sp_id",
        # "password",
        "port",
        "preferred_role",
        "principal",
        "profile",
        "provider_name",
        "region",
        "replication",
        "role_arn",
        "role_session_name",
        "scope",
        # "secret_access_key",
        "serverless_acct_id",
        "serverless_work_group",
        # "session_token",
        "source_address",
        "ssl",
        "ssl_insecure",
        "sslmode",
        "tcp_keepalive",
        "token_type",
        "timeout",
        "unix_sock",
        "user_name",
        # "web_identity_token",
    )

    if info is None:
        return info

    temp: RedshiftProperty = RedshiftProperty()

    def is_populated(field: typing.Optional[str]):
        return field is not None and field != ""

    for parameter, value in info.__dict__.items():
        if parameter in logging_allow_list:
            temp.put(parameter, value)
        elif is_populated(value):
            try:
                temp.put(parameter, "***")
            except AttributeError:
                pass

    return temp
