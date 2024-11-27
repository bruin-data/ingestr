#!/usr/bin/env python
#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

from typing import TYPE_CHECKING, Any

from ..network import ID_TOKEN_AUTHENTICATOR
from .by_plugin import AuthByPlugin, AuthType
from .webbrowser import AuthByWebBrowser

if TYPE_CHECKING:
    from ..connection import SnowflakeConnection


class AuthByIdToken(AuthByPlugin):
    """Internal IdToken Based Authentication.

    Works by accepting an id_toke and use that to authenticate. Only be used when users are using EXTERNAL_BROWSER_AUTHENTICATOR
    """

    @property
    def type_(self) -> AuthType:
        return AuthType.ID_TOKEN

    @property
    def assertion_content(self) -> str:
        return self._id_token

    def __init__(
        self,
        id_token: str,
        application: str,
        protocol: str | None,
        host: str | None,
        port: str | None,
        **kwargs,
    ) -> None:
        """Initialized an instance with an IdToken."""
        super().__init__(**kwargs)
        self._id_token: str | None = id_token
        self._application = application
        self._protocol = protocol
        self._host = host
        self._port = port

    def reset_secrets(self) -> None:
        self._id_token = None

    def prepare(self, **kwargs: Any) -> None:
        pass

    def reauthenticate(
        self,
        *,
        conn: SnowflakeConnection,
        **kwargs: Any,
    ) -> dict[str, bool]:
        conn.auth_class = AuthByWebBrowser(
            application=self._application,
            protocol=self._protocol,
            host=self._host,
            port=self._port,
            timeout=conn.login_timeout,
            backoff_generator=conn._backoff_generator,
        )
        conn._authenticate(conn.auth_class)
        conn._auth_class.reset_secrets()
        return {"success": True}

    def update_body(self, body: dict[Any, Any]) -> None:
        """Idtoken needs the authenticator and token attributes set."""
        body["data"]["AUTHENTICATOR"] = ID_TOKEN_AUTHENTICATOR
        body["data"]["TOKEN"] = self._id_token
