# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
This module handles how credentials are read in dlt sources
"""

from typing import ClassVar, List, Union

import dlt
from dlt.common.configuration import configspec
from dlt.common.configuration.specs import CredentialsConfiguration
from dlt.common.typing import TSecretValue


@configspec
class ZendeskCredentialsBase(CredentialsConfiguration):
    """
    The Base version of all the ZendeskCredential classes.
    """

    subdomain: str = dlt.config.value
    __config_gen_annotations__: ClassVar[List[str]] = []


@configspec
class ZendeskCredentialsEmailPass(ZendeskCredentialsBase):
    """
    This class is used to store credentials for Email + Password Authentication
    """

    email: str = dlt.config.value
    password: TSecretValue = dlt.secrets.value


@configspec
class ZendeskCredentialsOAuth(ZendeskCredentialsBase):
    """
    This class is used to store credentials for OAuth Token Authentication
    """

    oauth_token: TSecretValue = dlt.secrets.value


@configspec
class ZendeskCredentialsToken(ZendeskCredentialsBase):
    """
    This class is used to store credentials for Token Authentication
    """

    email: str = dlt.config.value
    token: TSecretValue = dlt.secrets.value


TZendeskCredentials = Union[
    ZendeskCredentialsEmailPass, ZendeskCredentialsToken, ZendeskCredentialsOAuth
]
