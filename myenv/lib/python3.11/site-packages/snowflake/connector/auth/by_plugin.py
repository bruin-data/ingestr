#!/usr/bin/env python
#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

"""This module implements the base class for authenticator classes.

Note:
 **kwargs are added to most functions so that child classes can safely ignore extra in
  arguments in case of a caller API change and named arguments are enforced to prevent
  issues with argument being sent in out of order.
"""

import logging
import time
from abc import ABC, abstractmethod
from enum import Enum, unique
from os import getenv
from typing import TYPE_CHECKING, Any, Iterator

from ..errorcode import ER_FAILED_TO_CONNECT_TO_DB
from ..errors import DatabaseError, Error, OperationalError
from ..sqlstate import SQLSTATE_CONNECTION_WAS_NOT_ESTABLISHED
from ..time_util import TimeoutBackoffCtx

if TYPE_CHECKING:
    from .. import SnowflakeConnection

logger = logging.getLogger(__name__)

"""
Default value for max retry is 1 because
Python requests module already tries twice
by default. Unlike JWT where we need to refresh
token every 10 seconds, general authenticators
wait for 60 seconds before connection timeout
per attempt totaling a 240 sec wait time for a non
JWT based authenticator which is more than enough.
This can be changed ofcourse using MAX_CNXN_RETRY_ATTEMPTS
env variable.
"""
DEFAULT_MAX_CON_RETRY_ATTEMPTS = 1
DEFAULT_AUTH_CLASS_TIMEOUT = 120


@unique
class AuthType(Enum):
    DEFAULT = "SNOWFLAKE"  # default authenticator name
    EXTERNAL_BROWSER = "EXTERNALBROWSER"
    KEY_PAIR = "SNOWFLAKE_JWT"
    OAUTH = "OAUTH"
    ID_TOKEN = "ID_TOKEN"
    USR_PWD_MFA = "USERNAME_PASSWORD_MFA"
    OKTA = "OKTA"


class AuthByPlugin(ABC):
    """External Authenticator interface."""

    def __init__(
        self,
        timeout: int | None = None,
        backoff_generator: Iterator | None = None,
        **kwargs,
    ) -> None:
        self.consent_cache_id_token = False

        self._retry_ctx = TimeoutBackoffCtx(
            timeout=timeout if timeout is not None else DEFAULT_AUTH_CLASS_TIMEOUT,
            max_retry_attempts=kwargs.get(
                "max_retry_attempts",
                int(getenv("MAX_CON_RETRY_ATTEMPTS", DEFAULT_MAX_CON_RETRY_ATTEMPTS)),
            ),
            backoff_generator=backoff_generator,
        )

        # some authenticators may want to override socket level timeout
        # for example, AuthByKeyPair will set this to ensure JWT tokens are refreshed in time
        # if not None, this will override socket_timeout specified in connection
        self._socket_timeout = None

    @property
    def timeout(self) -> int:
        """The timeout of _retry_ctx is guaranteed not to be None during AuthByPlugin initialization"""
        return self._retry_ctx.timeout

    @timeout.setter
    def timeout(self) -> None:
        logger.warning(
            "Attempting to mutate timeout of AuthByPlugin. Create a new instance with desired parameters instead."
        )

    @property
    @abstractmethod
    def type_(self) -> AuthType:
        """Return the Snowflake friendly name of auth class."""
        raise NotImplementedError

    @property
    @abstractmethod
    def assertion_content(self) -> str:
        """Return a safe version of the information used to authenticate with Snowflake.

        This is used for logging, useful for printing temporary tokens, but make sure to
        mask secrets.
        """
        raise NotImplementedError

    @abstractmethod
    def prepare(
        self,
        *,
        conn: SnowflakeConnection,
        authenticator: str,
        service_name: str | None,
        account: str,
        user: str,
        password: str | None,
        **kwargs: Any,
    ) -> str | None:
        """Prepare for authentication.

        This function is useful for situations where we need to reach out to a 3rd-party
        service before authenticating with Snowflake.
        """
        raise NotImplementedError

    @abstractmethod
    def update_body(self, body: dict[Any, Any]) -> None:
        """Update the body of the authentication request."""
        raise NotImplementedError

    @abstractmethod
    def reset_secrets(self) -> None:
        """Reset secret members."""
        raise NotImplementedError

    @abstractmethod
    def reauthenticate(
        self,
        *,
        conn: SnowflakeConnection,
        **kwargs: Any,
    ) -> dict[str, Any]:
        """Re-perform authentication.

        The difference between this and authentication is that secrets will be removed
        from memory by the time this gets called.
        """
        raise NotImplementedError

    def _handle_failure(
        self,
        *,
        conn: SnowflakeConnection,
        ret: dict[Any, Any],
        **kwargs: Any,
    ) -> None:
        """Handles a failure when an issue happens while connecting to Snowflake.

        If the user returns from this function execution will continue. The argument
        data can be manipulated from within this function and so recovery is possible
        from here.
        """
        Error.errorhandler_wrapper(
            conn,
            None,
            DatabaseError,
            {
                "msg": "Failed to connect to DB: {host}:{port}, {message}".format(
                    host=conn._rest._host,
                    port=conn._rest._port,
                    message=ret["message"],
                ),
                "errno": int(ret.get("code", -1)),
                "sqlstate": SQLSTATE_CONNECTION_WAS_NOT_ESTABLISHED,
            },
        )

    def handle_timeout(
        self,
        *,
        authenticator: str,
        service_name: str | None,
        account: str,
        user: str,
        password: str,
        **kwargs: Any,
    ) -> None:
        """Default timeout handler.

        This will trigger if the authenticator
        hasn't implemented one. By default we retry on timeouts and use
        jitter to deduce the time to sleep before retrying. The sleep
        time ranges between 1 and 16 seconds.
        """

        # Some authenticators may not want to delete the parameters to this function
        # Currently, the only authenticator where this is the case is AuthByKeyPair
        if kwargs.pop("delete_params", True):
            del authenticator, service_name, account, user, password

        logger.debug("Default timeout handler invoked for authenticator")
        if not self._retry_ctx.should_retry:
            error = OperationalError(
                msg=f"Could not connect to Snowflake backend after {self._retry_ctx.current_retry_count + 1} attempt(s)."
                "Aborting",
                errno=ER_FAILED_TO_CONNECT_TO_DB,
            )
            raise error
        else:
            logger.debug(
                f"Hit connection timeout, attempt number {self._retry_ctx.current_retry_count + 1}."
                " Will retry in a bit..."
            )
            time.sleep(float(self._retry_ctx.current_sleep_time))
            self._retry_ctx.increment()
