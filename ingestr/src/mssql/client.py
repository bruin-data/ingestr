import struct

from dlt.common.destination.capabilities import DestinationCapabilitiesContext
from dlt.common.schema import Schema
from dlt.destinations.impl.mssql.configuration import MsSqlClientConfiguration
from dlt.destinations.impl.mssql.mssql import (
    HINT_TO_MSSQL_ATTR,
    MsSqlJobClient,
)
from dlt.destinations.impl.mssql.sql_client import (
    PyOdbcMsSqlClient,
    handle_datetimeoffset,
)


class OdbcMsSqlClient(PyOdbcMsSqlClient):
    SQL_COPT_SS_ACCESS_TOKEN = 1256
    SKIP_CREDENTIALS = {"PWD", "AUTHENTICATION", "UID"}

    def open_connection(self):
        cfg = self.credentials._get_odbc_dsn_dict()
        if (
            cfg.get("AUTHENTICATION", "").strip().lower()
            != "activedirectoryaccesstoken"
        ):
            return super().open_connection()

        import pyodbc  # type: ignore

        dsn = ";".join(
            [f"{k}={v}" for k, v in cfg.items() if k not in self.SKIP_CREDENTIALS]
        )

        self._conn = pyodbc.connect(
            dsn,
            timeout=self.credentials.connect_timeout,
            attrs_before={
                self.SQL_COPT_SS_ACCESS_TOKEN: self.serialize_token(cfg["PWD"]),
            },
        )

        # https://github.com/mkleehammer/pyodbc/wiki/Using-an-Output-Converter-function
        self._conn.add_output_converter(-155, handle_datetimeoffset)
        self._conn.autocommit = True
        return self._conn

    def serialize_token(self, token):
        # https://github.com/mkleehammer/pyodbc/issues/228#issuecomment-494773723
        encoded = token.encode("utf_16_le")
        return struct.pack("<i", len(encoded)) + encoded


class MsSqlClient(MsSqlJobClient):
    def __init__(
        self,
        schema: Schema,
        config: MsSqlClientConfiguration,
        capabilities: DestinationCapabilitiesContext,
    ) -> None:
        sql_client = OdbcMsSqlClient(
            config.normalize_dataset_name(schema),
            config.normalize_staging_dataset_name(schema),
            config.credentials,
            capabilities,
        )
        super(MsSqlJobClient, self).__init__(schema, config, sql_client)
        self.config: MsSqlClientConfiguration = config
        self.sql_client = sql_client
        self.active_hints = HINT_TO_MSSQL_ATTR if self.config.create_indexes else {}
        self.type_mapper = capabilities.get_type_mapper()
