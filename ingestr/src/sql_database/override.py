from typing import Optional

from dlt.common.configuration.specs.base_configuration import configspec
from dlt.sources.credentials import ConnectionStringCredentials


@configspec(init=False)
class IngestrConnectionStringCredentials(ConnectionStringCredentials):
    username: Optional[str] = None
    database: Optional[str] = None
