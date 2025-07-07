import dlt

from ingestr.src.mssql.client import MsSqlClient


class MsSqlDestImpl(dlt.destinations.mssql):
    @property
    def client_class(self):
        return MsSqlClient
