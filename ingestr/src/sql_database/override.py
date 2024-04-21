from typing import Any, ClassVar, Dict, List, Optional, Union
from dlt.common.configuration.specs.base_configuration import configspec
from dlt.sources.credentials import ConnectionStringCredentials


@configspec
class IngestrConnectionStringCredentials(ConnectionStringCredentials):
    username: Optional[str] = None
