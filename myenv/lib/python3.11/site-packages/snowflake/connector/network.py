#!/usr/bin/env python
#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import collections
import contextlib
import gzip
import itertools
import json
import logging
import re
import time
import uuid
from collections import OrderedDict
from threading import Lock
from typing import TYPE_CHECKING, Any

import OpenSSL.SSL

from snowflake.connector.secret_detector import SecretDetector
from snowflake.connector.vendored.requests.models import PreparedRequest
from snowflake.connector.vendored.urllib3.connectionpool import (
    HTTPConnectionPool,
    HTTPSConnectionPool,
)

from . import ssl_wrap_socket
from .compat import (
    BAD_GATEWAY,
    BAD_REQUEST,
    FORBIDDEN,
    GATEWAY_TIMEOUT,
    INTERNAL_SERVER_ERROR,
    METHOD_NOT_ALLOWED,
    OK,
    REQUEST_TIMEOUT,
    SERVICE_UNAVAILABLE,
    TOO_MANY_REQUESTS,
    UNAUTHORIZED,
    BadStatusLine,
    IncompleteRead,
    urlencode,
    urlparse,
)
from .constants import (
    _CONNECTIVITY_ERR_MSG,
    _SNOWFLAKE_HOST_SUFFIX_REGEX,
    HTTP_HEADER_ACCEPT,
    HTTP_HEADER_CONTENT_TYPE,
    HTTP_HEADER_SERVICE_NAME,
    HTTP_HEADER_USER_AGENT,
)
from .description import (
    CLIENT_NAME,
    CLIENT_VERSION,
    COMPILER,
    IMPLEMENTATION,
    OPERATING_SYSTEM,
    PLATFORM,
    PYTHON_VERSION,
    SNOWFLAKE_CONNECTOR_VERSION,
)
from .errorcode import (
    ER_CONNECTION_IS_CLOSED,
    ER_CONNECTION_TIMEOUT,
    ER_FAILED_TO_CONNECT_TO_DB,
    ER_FAILED_TO_RENEW_SESSION,
    ER_FAILED_TO_REQUEST,
    ER_RETRYABLE_CODE,
)
from .errors import (
    BadGatewayError,
    BadRequest,
    DatabaseError,
    Error,
    ForbiddenError,
    GatewayTimeoutError,
    InterfaceError,
    InternalServerError,
    MethodNotAllowed,
    OperationalError,
    OtherHTTPRetryableError,
    ProgrammingError,
    RefreshTokenError,
    ServiceUnavailableError,
    TooManyRequests,
)
from .sqlstate import (
    SQLSTATE_CONNECTION_NOT_EXISTS,
    SQLSTATE_CONNECTION_REJECTED,
    SQLSTATE_CONNECTION_WAS_NOT_ESTABLISHED,
)
from .time_util import (
    DEFAULT_MASTER_VALIDITY_IN_SECONDS,
    TimeoutBackoffCtx,
    get_time_millis,
)
from .tool.probe_connection import probe_connection
from .vendored import requests
from .vendored.requests import Response, Session
from .vendored.requests.adapters import HTTPAdapter
from .vendored.requests.auth import AuthBase
from .vendored.requests.exceptions import (
    ConnectionError,
    ConnectTimeout,
    InvalidProxyURL,
    ReadTimeout,
    SSLError,
)
from .vendored.requests.utils import prepend_scheme_if_needed, select_proxy
from .vendored.urllib3.exceptions import ProtocolError
from .vendored.urllib3.poolmanager import ProxyManager
from .vendored.urllib3.util.url import parse_url

if TYPE_CHECKING:
    from .connection import SnowflakeConnection
logger = logging.getLogger(__name__)

"""
Monkey patch for PyOpenSSL Socket wrapper
"""
ssl_wrap_socket.inject_into_urllib3()

# known applications
APPLICATION_SNOWSQL = "SnowSQL"

# requests parameters
REQUESTS_RETRY = 1  # requests library builtin retry
DEFAULT_SOCKET_CONNECT_TIMEOUT = 1 * 60  # don't reduce less than 45 seconds

# return codes
QUERY_IN_PROGRESS_CODE = "333333"  # GS code: the query is in progress
QUERY_IN_PROGRESS_ASYNC_CODE = "333334"  # GS code: the query is detached

ID_TOKEN_EXPIRED_GS_CODE = "390110"
SESSION_EXPIRED_GS_CODE = "390112"  # GS code: session expired. need to renew
MASTER_TOKEN_NOTFOUND_GS_CODE = "390113"
MASTER_TOKEN_EXPIRED_GS_CODE = "390114"
MASTER_TOKEN_INVALD_GS_CODE = "390115"
ID_TOKEN_INVALID_LOGIN_REQUEST_GS_CODE = "390195"
BAD_REQUEST_GS_CODE = "390400"

# other constants
CONTENT_TYPE_APPLICATION_JSON = "application/json"
ACCEPT_TYPE_APPLICATION_SNOWFLAKE = "application/snowflake"

REQUEST_TYPE_RENEW = "RENEW"

HEADER_AUTHORIZATION_KEY = "Authorization"
HEADER_SNOWFLAKE_TOKEN = 'Snowflake Token="{token}"'

REQUEST_ID = "requestId"
REQUEST_GUID = "request_guid"
SNOWFLAKE_HOST_SUFFIX = ".snowflakecomputing.com"


SNOWFLAKE_CONNECTOR_VERSION = SNOWFLAKE_CONNECTOR_VERSION
PYTHON_VERSION = PYTHON_VERSION
OPERATING_SYSTEM = OPERATING_SYSTEM
PLATFORM = PLATFORM
IMPLEMENTATION = IMPLEMENTATION
COMPILER = COMPILER

CLIENT_NAME = CLIENT_NAME  # don't change!
CLIENT_VERSION = CLIENT_VERSION
PYTHON_CONNECTOR_USER_AGENT = f"{CLIENT_NAME}/{SNOWFLAKE_CONNECTOR_VERSION} ({PLATFORM}) {IMPLEMENTATION}/{PYTHON_VERSION}"

NO_TOKEN = "no-token"

STATUS_TO_EXCEPTION: dict[int, type[Error]] = {
    INTERNAL_SERVER_ERROR: InternalServerError,
    FORBIDDEN: ForbiddenError,
    SERVICE_UNAVAILABLE: ServiceUnavailableError,
    GATEWAY_TIMEOUT: GatewayTimeoutError,
    BAD_REQUEST: BadRequest,
    BAD_GATEWAY: BadGatewayError,
    METHOD_NOT_ALLOWED: MethodNotAllowed,
    TOO_MANY_REQUESTS: TooManyRequests,
}

DEFAULT_AUTHENTICATOR = "SNOWFLAKE"  # default authenticator name
EXTERNAL_BROWSER_AUTHENTICATOR = "EXTERNALBROWSER"
KEY_PAIR_AUTHENTICATOR = "SNOWFLAKE_JWT"
OAUTH_AUTHENTICATOR = "OAUTH"
ID_TOKEN_AUTHENTICATOR = "ID_TOKEN"
USR_PWD_MFA_AUTHENTICATOR = "USERNAME_PASSWORD_MFA"


def is_retryable_http_code(code: int) -> bool:
    """Decides whether code is a retryable HTTP issue."""
    return 500 <= code < 600 or code in (
        BAD_REQUEST,  # 400
        FORBIDDEN,  # 403
        METHOD_NOT_ALLOWED,  # 405
        REQUEST_TIMEOUT,  # 408
        TOO_MANY_REQUESTS,  # 429
    )


def get_http_retryable_error(status_code: int) -> Error:
    error_class: type[Error] = STATUS_TO_EXCEPTION.get(
        status_code, OtherHTTPRetryableError
    )
    return error_class(errno=status_code)


def raise_okta_unauthorized_error(
    connection: SnowflakeConnection | None, response: Response
) -> None:
    Error.errorhandler_wrapper(
        connection,
        None,
        DatabaseError,
        {
            "msg": f"Failed to get authentication by OKTA: {response.status_code}: {response.reason}",
            "errno": ER_FAILED_TO_CONNECT_TO_DB,
            "sqlstate": SQLSTATE_CONNECTION_REJECTED,
        },
    )


def raise_failed_request_error(
    connection: SnowflakeConnection | None,
    url: str,
    method: str,
    response: Response,
) -> None:
    Error.errorhandler_wrapper(
        connection,
        None,
        InterfaceError,
        {
            "msg": f"{response.status_code} {response.reason}: {method} {url}",
            "errno": ER_FAILED_TO_REQUEST,
            "sqlstate": SQLSTATE_CONNECTION_WAS_NOT_ESTABLISHED,
        },
    )


def is_login_request(url: str) -> bool:
    return "login-request" in parse_url(url).path


class ProxySupportAdapter(HTTPAdapter):
    """This Adapter creates proper headers for Proxy CONNECT messages."""

    def get_connection(
        self, url: str, proxies: OrderedDict | None = None
    ) -> HTTPConnectionPool | HTTPSConnectionPool:
        proxy = select_proxy(url, proxies)
        parsed_url = urlparse(url)

        if proxy:
            proxy = prepend_scheme_if_needed(proxy, "http")
            proxy_url = parse_url(proxy)
            if not proxy_url.host:
                raise InvalidProxyURL(
                    "Please check proxy URL. It is malformed"
                    " and could be missing the host."
                )
            proxy_manager = self.proxy_manager_for(proxy)

            if isinstance(proxy_manager, ProxyManager):
                # Add Host to proxy header SNOW-232777
                proxy_manager.proxy_headers["Host"] = parsed_url.hostname
            else:
                logger.debug(
                    f"Unable to set 'Host' to proxy manager of type {type(proxy_manager)} as"
                    f" it does not have attribute 'proxy_headers'."
                )
            conn = proxy_manager.connection_from_url(url)
        else:
            # Only scheme should be lower case
            url = parsed_url.geturl()
            conn = self.poolmanager.connection_from_url(url)

        return conn


class RetryRequest(Exception):
    """Signal to retry request."""

    pass


class ReauthenticationRequest(Exception):
    """Signal to reauthenticate."""

    def __init__(self, cause) -> None:
        self.cause = cause


class SnowflakeAuth(AuthBase):
    """Attaches HTTP Authorization header for Snowflake."""

    def __init__(self, token) -> None:
        # setup any auth-related data here
        self.token = token

    def __call__(self, r: PreparedRequest) -> PreparedRequest:
        """Modifies and returns the request."""
        if HEADER_AUTHORIZATION_KEY in r.headers:
            del r.headers[HEADER_AUTHORIZATION_KEY]
        if self.token != NO_TOKEN:
            r.headers[HEADER_AUTHORIZATION_KEY] = HEADER_SNOWFLAKE_TOKEN.format(
                token=self.token
            )
        return r


class SessionPool:
    def __init__(self, rest: SnowflakeRestful) -> None:
        # A stack of the idle sessions
        self._idle_sessions: list[Session] = []
        self._active_sessions: set[Session] = set()
        self._rest: SnowflakeRestful = rest

    def get_session(self) -> Session:
        """Returns a session from the session pool or creates a new one."""
        try:
            session = self._idle_sessions.pop()
        except IndexError:
            session = self._rest.make_requests_session()
        self._active_sessions.add(session)
        return session

    def return_session(self, session: Session) -> None:
        """Places an active session back into the idle session stack."""
        try:
            self._active_sessions.remove(session)
        except KeyError:
            logger.debug("session doesn't exist in the active session pool. Ignored...")
        self._idle_sessions.append(session)

    def __str__(self) -> str:
        total_sessions = len(self._active_sessions) + len(self._idle_sessions)
        return (
            f"SessionPool {len(self._active_sessions)}/{total_sessions} active sessions"
        )

    def close(self) -> None:
        """Closes all active and idle sessions in this session pool."""
        if self._active_sessions:
            logger.debug(f"Closing {len(self._active_sessions)} active sessions")
        for s in itertools.chain(self._active_sessions, self._idle_sessions):
            try:
                s.close()
            except Exception as e:
                logger.info(f"Session cleanup failed: {e}")
        self._active_sessions.clear()
        self._idle_sessions.clear()


class SnowflakeRestful:
    """Snowflake Restful class."""

    def __init__(
        self,
        host: str = "127.0.0.1",
        port: int = 8080,
        protocol: str = "http",
        inject_client_pause: int = 0,
        connection: SnowflakeConnection | None = None,
    ) -> None:
        self._host = host
        self._port = port
        self._protocol = protocol
        self._inject_client_pause = inject_client_pause
        self._connection = connection
        self._lock_token = Lock()
        self._sessions_map: dict[str | None, SessionPool] = collections.defaultdict(
            lambda: SessionPool(self)
        )

        # OCSP mode (OCSPMode.FAIL_OPEN by default)
        ssl_wrap_socket.FEATURE_OCSP_MODE = (
            self._connection._ocsp_mode()
            if self._connection
            else ssl_wrap_socket.DEFAULT_OCSP_MODE
        )
        # cache file name (enabled by default)
        ssl_wrap_socket.FEATURE_OCSP_RESPONSE_CACHE_FILE_NAME = (
            self._connection._ocsp_response_cache_filename if self._connection else None
        )

        # This is to address the issue where requests hangs
        _ = "dummy".encode("idna").decode("utf-8")

    @property
    def token(self) -> str | None:
        return self._token if hasattr(self, "_token") else None

    @property
    def master_token(self) -> str | None:
        return self._master_token if hasattr(self, "_master_token") else None

    @property
    def master_validity_in_seconds(self) -> int:
        return (
            self._master_validity_in_seconds
            if hasattr(self, "_master_validity_in_seconds")
            and self._master_validity_in_seconds
            else DEFAULT_MASTER_VALIDITY_IN_SECONDS
        )

    @master_validity_in_seconds.setter
    def master_validity_in_seconds(self, value) -> None:
        self._master_validity_in_seconds = (
            value if value else DEFAULT_MASTER_VALIDITY_IN_SECONDS
        )

    @property
    def id_token(self):
        return getattr(self, "_id_token", None)

    @id_token.setter
    def id_token(self, value) -> None:
        self._id_token = value

    @property
    def mfa_token(self) -> str | None:
        return getattr(self, "_mfa_token", None)

    @mfa_token.setter
    def mfa_token(self, value: str) -> None:
        self._mfa_token = value

    @property
    def server_url(self) -> str:
        return f"{self._protocol}://{self._host}:{self._port}"

    def close(self) -> None:
        if hasattr(self, "_token"):
            del self._token
        if hasattr(self, "_master_token"):
            del self._master_token
        if hasattr(self, "_id_token"):
            del self._id_token
        if hasattr(self, "_mfa_token"):
            del self._mfa_token

        for session_pool in self._sessions_map.values():
            session_pool.close()

    def request(
        self,
        url,
        body=None,
        method: str = "post",
        client: str = "sfsql",
        timeout: int | None = None,
        _no_results: bool = False,
        _include_retry_params: bool = False,
        _no_retry: bool = False,
    ):
        if body is None:
            body = {}
        if self.master_token is None and self.token is None:
            Error.errorhandler_wrapper(
                self._connection,
                None,
                DatabaseError,
                {
                    "msg": "Connection is closed",
                    "errno": ER_CONNECTION_IS_CLOSED,
                    "sqlstate": SQLSTATE_CONNECTION_NOT_EXISTS,
                },
            )

        if client == "sfsql":
            accept_type = ACCEPT_TYPE_APPLICATION_SNOWFLAKE
        else:
            accept_type = CONTENT_TYPE_APPLICATION_JSON

        headers = {
            HTTP_HEADER_CONTENT_TYPE: CONTENT_TYPE_APPLICATION_JSON,
            HTTP_HEADER_ACCEPT: accept_type,
            HTTP_HEADER_USER_AGENT: PYTHON_CONNECTOR_USER_AGENT,
        }
        try:
            from opentelemetry.propagate import inject

            inject(headers)
        except ModuleNotFoundError as e:
            logger.debug(f"Opentelemtry otel injection failed because of: {e}")
        if self._connection.service_name:
            headers[HTTP_HEADER_SERVICE_NAME] = self._connection.service_name
        if method == "post":
            return self._post_request(
                url,
                headers,
                json.dumps(body),
                token=self.token,
                _no_results=_no_results,
                timeout=timeout,
                _include_retry_params=_include_retry_params,
                no_retry=_no_retry,
            )
        else:
            return self._get_request(
                url,
                headers,
                token=self.token,
                timeout=timeout,
            )

    def update_tokens(
        self,
        session_token,
        master_token,
        master_validity_in_seconds=None,
        id_token=None,
        mfa_token=None,
    ) -> None:
        """Updates session and master tokens and optionally temporary credential."""
        with self._lock_token:
            self._token = session_token
            self._master_token = master_token
            self._id_token = id_token
            self._mfa_token = mfa_token
            self._master_validity_in_seconds = master_validity_in_seconds

    def _renew_session(self):
        """Renew a session and master token."""
        return self._token_request(REQUEST_TYPE_RENEW)

    def _token_request(self, request_type):
        logger.debug(
            "updating session. master_token: {}".format(
                "****" if self.master_token else None
            )
        )
        headers = {
            HTTP_HEADER_CONTENT_TYPE: CONTENT_TYPE_APPLICATION_JSON,
            HTTP_HEADER_ACCEPT: CONTENT_TYPE_APPLICATION_JSON,
            HTTP_HEADER_USER_AGENT: PYTHON_CONNECTOR_USER_AGENT,
        }
        if self._connection.service_name:
            headers[HTTP_HEADER_SERVICE_NAME] = self._connection.service_name
        request_id = str(uuid.uuid4())
        logger.debug("request_id: %s", request_id)
        url = "/session/token-request?" + urlencode({REQUEST_ID: request_id})

        # NOTE: ensure an empty key if master token is not set.
        # This avoids HTTP 400.
        header_token = self.master_token or ""
        body = {
            "oldSessionToken": self.token,
            "requestType": request_type,
        }
        ret = self._post_request(
            url,
            headers,
            json.dumps(body),
            token=header_token,
        )
        if ret.get("success") and ret.get("data", {}).get("sessionToken"):
            logger.debug("success: %s", SecretDetector.mask_secrets(str(ret)))
            self.update_tokens(
                ret["data"]["sessionToken"],
                ret["data"].get("masterToken"),
                master_validity_in_seconds=ret["data"].get("masterValidityInSeconds"),
            )
            logger.debug("updating session completed")
            return ret
        else:
            logger.debug("failed: %s", SecretDetector.mask_secrets(str(ret)))
            err = ret.get("message")
            if err is not None and ret.get("data"):
                err += ret["data"].get("errorMessage", "")
            errno = ret.get("code") or ER_FAILED_TO_RENEW_SESSION
            if errno in (
                ID_TOKEN_EXPIRED_GS_CODE,
                SESSION_EXPIRED_GS_CODE,
                MASTER_TOKEN_NOTFOUND_GS_CODE,
                MASTER_TOKEN_EXPIRED_GS_CODE,
                MASTER_TOKEN_INVALD_GS_CODE,
                BAD_REQUEST_GS_CODE,
            ):
                raise ReauthenticationRequest(
                    ProgrammingError(
                        msg=err,
                        errno=int(errno),
                        sqlstate=SQLSTATE_CONNECTION_WAS_NOT_ESTABLISHED,
                    )
                )
            Error.errorhandler_wrapper(
                self._connection,
                None,
                ProgrammingError,
                {
                    "msg": err,
                    "errno": int(errno),
                    "sqlstate": SQLSTATE_CONNECTION_WAS_NOT_ESTABLISHED,
                },
            )

    def _heartbeat(self) -> Any | dict[Any, Any] | None:
        headers = {
            HTTP_HEADER_CONTENT_TYPE: CONTENT_TYPE_APPLICATION_JSON,
            HTTP_HEADER_ACCEPT: CONTENT_TYPE_APPLICATION_JSON,
            HTTP_HEADER_USER_AGENT: PYTHON_CONNECTOR_USER_AGENT,
        }
        if self._connection.service_name:
            headers[HTTP_HEADER_SERVICE_NAME] = self._connection.service_name
        request_id = str(uuid.uuid4())
        logger.debug("request_id: %s", request_id)
        url = "/session/heartbeat?" + urlencode({REQUEST_ID: request_id})
        ret = self._post_request(
            url,
            headers,
            None,
            token=self.token,
        )
        if not ret.get("success"):
            logger.error("Failed to heartbeat. code: %s, url: %s", ret.get("code"), url)
        return ret

    def delete_session(self, retry: bool = False) -> None:
        """Deletes the session."""
        if self.master_token is None:
            Error.errorhandler_wrapper(
                self._connection,
                None,
                DatabaseError,
                {
                    "msg": "Connection is closed",
                    "errno": ER_CONNECTION_IS_CLOSED,
                    "sqlstate": SQLSTATE_CONNECTION_NOT_EXISTS,
                },
            )

        url = "/session?" + urlencode({"delete": "true"})
        headers = {
            HTTP_HEADER_CONTENT_TYPE: CONTENT_TYPE_APPLICATION_JSON,
            HTTP_HEADER_ACCEPT: CONTENT_TYPE_APPLICATION_JSON,
            HTTP_HEADER_USER_AGENT: PYTHON_CONNECTOR_USER_AGENT,
        }
        if self._connection.service_name:
            headers[HTTP_HEADER_SERVICE_NAME] = self._connection.service_name

        body = {}
        retry_limit = 3 if retry else 1
        num_retries = 0
        should_retry = True
        while should_retry and (num_retries < retry_limit):
            try:
                should_retry = False
                ret = self._post_request(
                    url,
                    headers,
                    json.dumps(body),
                    token=self.token,
                    timeout=5,
                    no_retry=True,
                )
                if not ret:
                    if retry:
                        should_retry = True
                    else:
                        return
                elif ret.get("success"):
                    return
                err = ret.get("message")
                if err is not None and ret.get("data"):
                    err += ret["data"].get("errorMessage", "")
                    # no exception is raised
                logger.debug("error in deleting session. ignoring...: %s", err)
            except Exception as e:
                logger.debug("error in deleting session. ignoring...: %s", e)
            finally:
                num_retries += 1

    def _get_request(
        self,
        url: str,
        headers: dict[str, str],
        token: str = None,
        timeout: int | None = None,
        is_fetch_query_status: bool = False,
    ) -> dict[str, Any]:
        if "Content-Encoding" in headers:
            del headers["Content-Encoding"]
        if "Content-Length" in headers:
            del headers["Content-Length"]

        full_url = f"{self.server_url}{url}"
        ret = self.fetch(
            "get",
            full_url,
            headers,
            timeout=timeout,
            token=token,
            is_fetch_query_status=is_fetch_query_status,
        )
        if ret.get("code") == SESSION_EXPIRED_GS_CODE:
            try:
                ret = self._renew_session()
            except ReauthenticationRequest as ex:
                if self._connection._authenticator != EXTERNAL_BROWSER_AUTHENTICATOR:
                    raise ex.cause
                ret = self._connection._reauthenticate()
            logger.debug(
                "ret[code] = {code} after renew_session".format(
                    code=(ret.get("code", "N/A"))
                )
            )
            if ret.get("success"):
                return self._get_request(
                    url,
                    headers,
                    token=self.token,
                    is_fetch_query_status=is_fetch_query_status,
                )

        return ret

    def _post_request(
        self,
        url,
        headers,
        body,
        token=None,
        timeout: int | None = None,
        socket_timeout: int | None = None,
        _no_results: bool = False,
        no_retry: bool = False,
        _include_retry_params: bool = False,
    ):
        full_url = f"{self.server_url}{url}"
        if self._connection._probe_connection:
            from pprint import pprint

            ret = probe_connection(full_url)
            pprint(ret)

        ret = self.fetch(
            "post",
            full_url,
            headers,
            data=body,
            timeout=timeout,
            token=token,
            no_retry=no_retry,
            _include_retry_params=_include_retry_params,
            socket_timeout=socket_timeout,
        )
        logger.debug(
            "ret[code] = {code}, after post request".format(
                code=(ret.get("code", "N/A"))
            )
        )

        if ret.get("code") == MASTER_TOKEN_EXPIRED_GS_CODE:
            self._connection.expired = True
        elif ret.get("code") == SESSION_EXPIRED_GS_CODE:
            try:
                ret = self._renew_session()
            except ReauthenticationRequest as ex:
                if self._connection._authenticator != EXTERNAL_BROWSER_AUTHENTICATOR:
                    raise ex.cause
                ret = self._connection._reauthenticate()
            logger.debug(
                "ret[code] = {code} after renew_session".format(
                    code=(ret.get("code", "N/A"))
                )
            )
            if ret.get("success"):
                return self._post_request(
                    url, headers, body, token=self.token, timeout=timeout
                )

        if isinstance(ret.get("data"), dict) and ret["data"].get("queryId"):
            logger.debug("Query id: {}".format(ret["data"]["queryId"]))

        if ret.get("code") == QUERY_IN_PROGRESS_ASYNC_CODE and _no_results:
            return ret

        while ret.get("code") in (QUERY_IN_PROGRESS_CODE, QUERY_IN_PROGRESS_ASYNC_CODE):
            if self._inject_client_pause > 0:
                logger.debug("waiting for %s...", self._inject_client_pause)
                time.sleep(self._inject_client_pause)
            # ping pong
            result_url = ret["data"]["getResultUrl"]
            logger.debug("ping pong starting...")
            ret = self._get_request(
                result_url,
                headers,
                token=self.token,
                timeout=timeout,
                is_fetch_query_status=bool(
                    re.match(r"^/queries/.+/result$", result_url)
                ),
            )
            logger.debug("ret[code] = %s", ret.get("code", "N/A"))
            logger.debug("ping pong done")

        return ret

    def fetch(
        self,
        method: str,
        full_url: str,
        headers: dict[str, Any],
        data: dict[str, Any] | None = None,
        timeout: int | None = None,
        **kwargs,
    ) -> dict[Any, Any]:
        """Carry out API request with session management."""

        class RetryCtx(TimeoutBackoffCtx):
            def __init__(
                self,
                _include_retry_params: bool = False,
                _include_retry_reason: bool = False,
                **kwargs,
            ) -> None:
                super().__init__(**kwargs)
                self.retry_reason = 0
                self._include_retry_params = _include_retry_params
                self._include_retry_reason = _include_retry_reason

            def add_retry_params(self, full_url: str) -> str:
                if self._include_retry_params and self.current_retry_count > 0:
                    retry_params = {
                        "clientStartTime": self._start_time_millis,
                        "retryCount": self.current_retry_count,
                    }
                    if self._include_retry_reason:
                        retry_params.update({"retryReason": self.retry_reason})
                    suffix = urlencode(retry_params)
                    sep = "&" if urlparse(full_url).query else "?"
                    return full_url + sep + suffix
                else:
                    return full_url

        include_retry_reason = self._connection._enable_retry_reason_in_query_response
        include_retry_params = kwargs.pop("_include_retry_params", False)

        with self._use_requests_session(full_url) as session:
            retry_ctx = RetryCtx(
                _include_retry_params=include_retry_params,
                _include_retry_reason=include_retry_reason,
                timeout=(
                    timeout if timeout is not None else self._connection.network_timeout
                ),
                backoff_generator=self._connection._backoff_generator,
            )

            retry_ctx.set_start_time()
            while True:
                ret = self._request_exec_wrapper(
                    session, method, full_url, headers, data, retry_ctx, **kwargs
                )
                if ret is not None:
                    return ret

    @staticmethod
    def add_request_guid(full_url: str) -> str:
        """Adds request_guid parameter for HTTP request tracing."""
        parsed_url = urlparse(full_url)
        if not re.search(_SNOWFLAKE_HOST_SUFFIX_REGEX, parsed_url.hostname):
            return full_url
        request_guid = str(uuid.uuid4())
        suffix = urlencode({REQUEST_GUID: request_guid})
        logger.debug(f"Request guid: {request_guid}")
        sep = "&" if parsed_url.query else "?"
        # url has query string already, just add fields
        return full_url + sep + suffix

    def _request_exec_wrapper(
        self,
        session,
        method,
        full_url,
        headers,
        data,
        retry_ctx,
        no_retry: bool = False,
        token=NO_TOKEN,
        **kwargs,
    ):
        conn = self._connection
        logger.debug(
            "remaining request timeout: %s ms, retry cnt: %s",
            retry_ctx.remaining_time_millis if retry_ctx.timeout is not None else "N/A",
            retry_ctx.current_retry_count + 1,
        )

        full_url = retry_ctx.add_retry_params(full_url)
        full_url = SnowflakeRestful.add_request_guid(full_url)
        is_fetch_query_status = kwargs.pop("is_fetch_query_status", False)
        # raise_raw_http_failure is not a public parameter and may change in the future
        # it enables raising raw http errors that are not handled by
        # connector, connector handles the following http error:
        #  1. FORBIDDEN error when trying to login
        #  2. retryable http code defined in method is_retryable_http_code
        #  3. UNAUTHORIZED error when using okta authentication
        # raise_raw_http_failure doesn't work for the 3 mentioned cases.
        raise_raw_http_failure = kwargs.pop("raise_raw_http_failure", False)
        try:
            return_object = self._request_exec(
                session=session,
                method=method,
                full_url=full_url,
                headers=headers,
                data=data,
                token=token,
                raise_raw_http_failure=raise_raw_http_failure,
                **kwargs,
            )
            if return_object is not None:
                return return_object
            if is_fetch_query_status:
                err_msg = "fetch query status failed and http request returned None, this is usually caused by transient network failures, retrying..."
                logger.info(err_msg)
                raise RetryRequest(err_msg)
            self._handle_unknown_error(method, full_url, headers, data, conn)
            return {}
        except RetryRequest as e:
            cause = e.args[0]
            if no_retry:
                self.log_and_handle_http_error_with_cause(
                    e,
                    full_url,
                    method,
                    retry_ctx.timeout,
                    retry_ctx.current_retry_count,
                    conn,
                    timed_out=False,
                )
                return {}  # required for tests
            if not retry_ctx.should_retry:
                self.log_and_handle_http_error_with_cause(
                    e,
                    full_url,
                    method,
                    retry_ctx.timeout,
                    retry_ctx.current_retry_count,
                    conn,
                )
                return {}  # required for tests

            logger.debug(
                "retrying: errorclass=%s, "
                "error=%s, "
                "counter=%s, "
                "sleeping=%s(s)",
                type(cause),
                cause,
                retry_ctx.current_retry_count + 1,
                retry_ctx.current_sleep_time,
            )
            time.sleep(float(retry_ctx.current_sleep_time))
            retry_ctx.increment()

            reason = getattr(cause, "errno", 0)
            retry_ctx.retry_reason = reason

            if "Connection aborted" in repr(e) and "ECONNRESET" in repr(e):
                # connection is reset by the server, the underlying connection is broken and can not be reused
                # we need a new urllib3 http(s) connection in this case.
                # We need to first close the old one so that urllib3 pool manager can create a new connection
                # for new requests
                try:
                    logger.debug(
                        "shutting down requests session adapter due to connection aborted"
                    )
                    session.get_adapter(full_url).close()
                except Exception as close_adapter_exc:
                    logger.debug(
                        "Ignored error caused by closing https connection failure: %s",
                        close_adapter_exc,
                    )
            return None  # retry
        except Exception as e:
            if (
                raise_raw_http_failure and isinstance(e, requests.exceptions.HTTPError)
            ) or not no_retry:
                raise e
            logger.debug("Ignored error", exc_info=True)
            return {}

    def log_and_handle_http_error_with_cause(
        self,
        e: Exception,
        full_url: str,
        method: str,
        retry_timeout: int,
        retry_count: int,
        conn: SnowflakeConnection,
        timed_out: bool = True,
    ) -> None:
        cause = e.args[0]
        logger.error(cause, exc_info=True)
        if isinstance(cause, Error):
            Error.errorhandler_wrapper_from_cause(conn, cause)
        else:
            self.handle_invalid_certificate_error(conn, full_url, cause)

    def handle_invalid_certificate_error(self, conn, full_url, cause) -> None:
        # all other errors raise exception
        Error.errorhandler_wrapper(
            conn,
            None,
            OperationalError,
            {
                "msg": f"Failed to execute request: {cause}",
                "errno": ER_FAILED_TO_REQUEST,
            },
        )

    def _handle_unknown_error(self, method, full_url, headers, data, conn) -> None:
        """Handles unknown errors."""
        if data:
            _, masked_data, err_str = SecretDetector.mask_secrets(data)
            if err_str is None:
                data = masked_data
        logger.error(
            f"Failed to get the response. Hanging? "
            f"method: {method}, url: {full_url}, headers:{headers}, "
            f"data: {data}"
        )
        Error.errorhandler_wrapper(
            conn,
            None,
            OperationalError,
            {
                "msg": f"Failed to get the response. Hanging? method: {method}, url: {full_url}",
                "errno": ER_FAILED_TO_REQUEST,
            },
        )

    def _request_exec(
        self,
        session,
        method,
        full_url,
        headers,
        data,
        token,
        catch_okta_unauthorized_error: bool = False,
        is_raw_text: bool = False,
        is_raw_binary: bool = False,
        binary_data_handler=None,
        socket_timeout: int | None = None,
        is_okta_authentication: bool = False,
        raise_raw_http_failure: bool = False,
    ):
        if socket_timeout is None:
            if self._connection.socket_timeout is not None:
                logger.debug("socket_timeout specified in connection")
                socket_timeout = self._connection.socket_timeout
            else:
                socket_timeout = DEFAULT_SOCKET_CONNECT_TIMEOUT
        logger.debug("socket timeout: %s", socket_timeout)

        try:
            if not catch_okta_unauthorized_error and data and len(data) > 0:
                headers["Content-Encoding"] = "gzip"
                input_data = gzip.compress(data.encode("utf-8"))
            else:
                input_data = data

            download_start_time = get_time_millis()
            # socket timeout is constant. You should be able to receive
            # the response within the time. If not, ConnectReadTimeout or
            # ReadTimeout is raised.
            raw_ret = session.request(
                method=method,
                url=full_url,
                headers=headers,
                data=input_data,
                timeout=socket_timeout,
                verify=True,
                stream=is_raw_binary,
                auth=SnowflakeAuth(token),
            )
            download_end_time = get_time_millis()

            try:
                if raw_ret.status_code == OK:
                    logger.debug("SUCCESS")
                    if is_raw_text:
                        ret = raw_ret.text
                    elif is_raw_binary:
                        ret = binary_data_handler.to_iterator(
                            raw_ret.raw, download_end_time - download_start_time
                        )
                    else:
                        ret = raw_ret.json()
                    return ret

                if is_login_request(full_url) and raw_ret.status_code == FORBIDDEN:
                    raise ForbiddenError

                elif is_retryable_http_code(raw_ret.status_code):
                    err = get_http_retryable_error(raw_ret.status_code)
                    # retryable server exceptions
                    if is_okta_authentication:
                        raise RefreshTokenError(
                            msg="OKTA authentication requires token refresh."
                        )
                    if is_login_request(full_url):
                        logger.debug(
                            "Received retryable response code while logging in. Will be handled by "
                            f"authenticator. Ignore the following. Error stack: {err}",
                            exc_info=True,
                        )
                        raise OperationalError(
                            msg="Login request is retryable. Will be handled by authenticator",
                            errno=ER_RETRYABLE_CODE,
                        )
                    else:
                        logger.debug(f"{err}. Retrying...")
                        raise RetryRequest(err)

                elif (
                    raw_ret.status_code == UNAUTHORIZED
                    and catch_okta_unauthorized_error
                ):
                    # OKTA Unauthorized errors
                    raise_okta_unauthorized_error(self._connection, raw_ret)
                    return None  # required for tests
                else:
                    if raise_raw_http_failure:
                        raw_ret.raise_for_status()
                    raise_failed_request_error(
                        self._connection, full_url, method, raw_ret
                    )
                    return None  # required for tests
            finally:
                raw_ret.close()  # ensure response is closed
        except SSLError as se:
            msg = f"Hit non-retryable SSL error, {str(se)}.\n{_CONNECTIVITY_ERR_MSG}"
            logger.debug(msg)
            # the following code is for backward compatibility with old versions of python connector which calls
            # self._handle_unknown_error to process SSLError
            Error.errorhandler_wrapper(
                self._connection,
                None,
                OperationalError,
                {
                    "msg": msg,
                    "errno": ER_FAILED_TO_REQUEST,
                },
            )
        except (
            BadStatusLine,
            ConnectionError,
            ConnectTimeout,
            IncompleteRead,
            ProtocolError,  # from urllib3  # from urllib3
            OpenSSL.SSL.SysCallError,
            KeyError,  # SNOW-39175: asn1crypto.keys.PublicKeyInfo
            ValueError,
            ReadTimeout,
            RuntimeError,
            AttributeError,  # json decoding error
        ) as err:
            if is_login_request(full_url):
                logger.debug(
                    "Hit a timeout error while logging in. Will be handled by "
                    f"authenticator. Ignore the following. Error stack: {err}",
                    exc_info=True,
                )
                raise OperationalError(
                    msg="ConnectionTimeout occurred during login. Will be handled by authenticator",
                    errno=ER_CONNECTION_TIMEOUT,
                )
            else:
                logger.debug(
                    "Hit retryable client error. Retrying... Ignore the following "
                    f"error stack: {err}",
                    exc_info=True,
                )
                raise RetryRequest(err)
        except Exception as err:
            raise err

    def make_requests_session(self) -> Session:
        s = requests.Session()
        s.mount("http://", ProxySupportAdapter(max_retries=REQUESTS_RETRY))
        s.mount("https://", ProxySupportAdapter(max_retries=REQUESTS_RETRY))
        s._reuse_count = itertools.count()
        return s

    @contextlib.contextmanager
    def _use_requests_session(self, url: str | None = None):
        """Session caching context manager.

        Notes:
            The session is not closed until close() is called so each session may be used multiple times.
        """
        # short-lived session, not added to the _sessions_map
        if self._connection.disable_request_pooling:
            session = self.make_requests_session()
            try:
                yield session
            finally:
                session.close()
        else:
            try:
                hostname = urlparse(url).hostname
            except Exception:
                hostname = None

            session_pool: SessionPool = self._sessions_map[hostname]
            session = session_pool.get_session()
            logger.debug(f"Session status for SessionPool '{hostname}', {session_pool}")
            try:
                yield session
            finally:
                session_pool.return_session(session)
                logger.debug(
                    f"Session status for SessionPool '{hostname}', {session_pool}"
                )
