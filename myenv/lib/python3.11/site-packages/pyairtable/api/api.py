import posixpath
from functools import partialmethod
from typing import Any, Dict, Iterator, List, Optional, Sequence, Tuple, TypeVar, Union

import requests
from requests.sessions import Session
from typing_extensions import TypeAlias

from pyairtable.api import retrying
from pyairtable.api.enterprise import Enterprise
from pyairtable.api.params import options_to_json_and_params, options_to_params
from pyairtable.api.types import UserAndScopesDict, assert_typed_dict
from pyairtable.api.workspace import Workspace
from pyairtable.models.schema import Bases
from pyairtable.utils import cache_unless_forced, chunked, enterprise_only

T = TypeVar("T")
TimeoutTuple: TypeAlias = Tuple[int, int]


class Api:
    """
    Represents an Airtable API. Implements basic URL construction,
    session and request management, and retrying logic.

    Usage:
        >>> api = Api('auth_token')
        >>> table = api.table('base_id', 'table_name')
        >>> records = table.all()
    """

    VERSION = "v0"
    API_LIMIT = 1.0 / 5  # 5 per second

    #: Airtable-imposed limit on number of records per batch create/update operation.
    MAX_RECORDS_PER_REQUEST = 10

    #: Airtable-imposed limit on the length of a URL (including query parameters).
    MAX_URL_LENGTH = 16000

    # Cached metadata to reduce API calls
    _bases: Optional[Dict[str, "pyairtable.api.base.Base"]] = None

    def __init__(
        self,
        api_key: str,
        *,
        timeout: Optional[TimeoutTuple] = None,
        retry_strategy: Optional[Union[bool, retrying.Retry]] = True,
        endpoint_url: str = "https://api.airtable.com",
    ):
        """
        Args:
            api_key: An Airtable API key or personal access token.
            timeout: A tuple indicating a connect and read timeout.
                e.g. ``timeout=(2,5)`` would configure a 2 second timeout for
                the connection to be established  and 5 seconds for a
                server read timeout. Default is ``None`` (no timeout).
            retry_strategy: An instance of
                `urllib3.util.Retry <https://urllib3.readthedocs.io/en/stable/reference/urllib3.util.html#urllib3.util.Retry>`_.
                If ``None`` or ``False``, requests will not be retried.
                If ``True``, the default strategy will be applied
                (see :func:`~pyairtable.retry_strategy` for details).
            endpoint_url: The API endpoint to use. Override this if you are using
                a debugging or caching proxy.
        """
        if retry_strategy is True:
            retry_strategy = retrying.retry_strategy()
        if not retry_strategy:
            self.session = Session()
        else:
            self.session = retrying._RetryingSession(retry_strategy)

        self.endpoint_url = endpoint_url
        self.timeout = timeout
        self.api_key = api_key

    @property
    def api_key(self) -> str:
        """
        Airtable API key or access token to use on all connections.
        """
        return self._api_key

    @api_key.setter
    def api_key(self, value: str) -> None:
        self.session.headers.update({"Authorization": "Bearer {}".format(value)})
        self._api_key = value

    def __repr__(self) -> str:
        return "<pyairtable.Api>"

    def whoami(self) -> UserAndScopesDict:
        """
        Return the current user ID and (if connected via OAuth) the list of scopes.
        See `Get user ID & scopes <https://airtable.com/developers/web/api/get-user-id-scopes>`_ for more information.
        """
        data = self.request("GET", self.build_url("meta/whoami"))
        return assert_typed_dict(UserAndScopesDict, data)

    def workspace(self, workspace_id: str) -> Workspace:
        return Workspace(self, workspace_id)

    def base(
        self,
        base_id: str,
        *,
        validate: bool = False,
        force: bool = False,
    ) -> "pyairtable.api.base.Base":
        """
        Return a new :class:`Base` instance that uses this instance of :class:`Api`.

        Args:
            base_id: |arg_base_id|
            validate: |kwarg_validate_metadata|
            force: |kwarg_force_metadata|

        Raises:
            KeyError: if ``validate=True`` and the given base ID does not exist.
        """
        if validate:
            info = self._base_info(force=force).base(base_id)
            return self._base_from_info(info)
        return pyairtable.api.base.Base(self, base_id)

    @cache_unless_forced
    def _base_info(self) -> Bases:
        """
        Return a schema object that represents all bases available via the API.
        """
        url = self.build_url("meta/bases")
        data = {
            "bases": [
                base_info
                for page in self.iterate_requests("GET", url)
                for base_info in page["bases"]
            ]
        }
        return Bases.from_api(data, self)

    def _base_from_info(self, base_info: Bases.Info) -> "pyairtable.api.base.Base":
        return pyairtable.api.base.Base(
            self,
            base_info.id,
            name=base_info.name,
            permission_level=base_info.permission_level,
        )

    def bases(self, *, force: bool = False) -> List["pyairtable.api.base.Base"]:
        """
        Retrieve the base's schema and return a list of :class:`Base` instances.

        Args:
            force: |kwarg_force_metadata|

        Usage:
            >>> api.bases()
            [
                <pyairtable.Base base_id='appSW9...'>,
                <pyairtable.Base base_id='appLkN...'>
            ]
        """
        return [
            self._base_from_info(info) for info in self._base_info(force=force).bases
        ]

    def create_base(
        self,
        workspace_id: str,
        name: str,
        tables: Sequence[Dict[str, Any]],
    ) -> "pyairtable.api.base.Base":
        """
        Create a base in the given workspace.

        See https://airtable.com/developers/web/api/create-base

        Args:
            workspace_id: The ID of the workspace where the new base will live.
            name: The name to give to the new base. Does not need to be unique.
            tables: A list of ``dict`` objects that conform to Airtable's
                `Table model <https://airtable.com/developers/web/api/model/table-model>`__.
        """
        return self.workspace(workspace_id).create_base(name, tables)

    def table(
        self,
        base_id: str,
        table_name: str,
        *,
        validate: bool = False,
        force: bool = False,
    ) -> "pyairtable.api.table.Table":
        """
        Build a new :class:`Table` instance that uses this instance of :class:`Api`.

        Args:
            base_id: |arg_base_id|
            table_name: The Airtable table's ID or name.
            validate: |kwarg_validate_metadata|
            force: |kwarg_force_metadata|
        """
        base = self.base(base_id, validate=validate, force=force)
        return base.table(table_name, validate=validate, force=force)

    def build_url(self, *components: str) -> str:
        """
        Build a URL to the Airtable API endpoint with the given URL components,
        including the API version number.
        """
        return posixpath.join(self.endpoint_url, self.VERSION, *components)

    def request(
        self,
        method: str,
        url: str,
        fallback: Optional[Tuple[str, str]] = None,
        options: Optional[Dict[str, Any]] = None,
        params: Optional[Dict[str, Any]] = None,
        json: Optional[Dict[str, Any]] = None,
    ) -> Any:
        """
        Make a request to the Airtable API, optionally converting a GET to a POST if the URL exceeds the
        `maximum URL length <https://support.airtable.com/docs/enforcement-of-url-length-limit-for-web-api-requests>`__.

        Args:
            method: HTTP method to use.
            url: The URL we're attempting to call.
            fallback: The method and URL to use if we have to convert a GET to a POST.
            options: Airtable-specific query params to use while fetching records.
                See :ref:`Parameters` for valid options.
            params: Additional query params to append to the URL as-is.
            json: The JSON payload for a POST/PUT/PATCH/DELETE request.
        """
        # Convert Airtable-specific options to query params, but give priority to query params
        # that are explicitly passed via `params=`. This is to preserve backwards-compatibility for
        # any library users who might be calling `self._request` directly.
        request_params = {
            **options_to_params(options or {}),
            **(params or {}),
        }

        # Build a requests.PreparedRequest so we can examine how long the URL is.
        prepared = self.session.prepare_request(
            requests.Request(
                method,
                url=url,
                params=request_params,
                json=json,
            )
        )

        # If our URL is too long, move *most* (not all) query params into a POST body.
        if (
            fallback
            and method.upper() == "GET"
            and len(str(prepared.url)) >= self.MAX_URL_LENGTH
        ):
            json, spare_params = options_to_json_and_params(options or {})
            return self.request(
                method=fallback[0],
                url=fallback[1],
                params={**spare_params, **(params or {})},
                json=json,
            )

        response = self.session.send(prepared, timeout=self.timeout)
        return self._process_response(response)

    get = partialmethod(request, "GET")
    post = partialmethod(request, "POST")
    patch = partialmethod(request, "PATCH")
    delete = partialmethod(request, "DELETE")

    def _process_response(self, response: requests.Response) -> Any:
        try:
            response.raise_for_status()
        except requests.exceptions.HTTPError as exc:
            # Attempt to get Error message from response, Issue #16
            try:
                error_dict = response.json()
            except ValueError:
                pass
            else:
                if "error" in error_dict:
                    exc.args = (*exc.args, repr(error_dict["error"]))
            raise exc

        # Some Airtable endpoints will respond with an empty body and a 200.
        if not response.text:
            return None
        return response.json()

    def iterate_requests(
        self,
        method: str,
        url: str,
        fallback: Optional[Tuple[str, str]] = None,
        options: Optional[Dict[str, Any]] = None,
        params: Optional[Dict[str, Any]] = None,
        offset_field: str = "offset",
    ) -> Iterator[Any]:
        """
        Make one or more requests and iterates through each result.

        If the response payload contains an 'offset' value, this method will perform
        another request with that offset value as a parameter (query params for GET,
        body payload for POST/PATCH/etc).

        If the response payload is not a 'dict', it will be yielded as normal
        and the method will return.

        Args:
            method: HTTP method to use.
            url: The URL we're attempting to call.
            fallback: The method and URL to use if we have to convert a GET to a POST.
            options: Airtable-specific query params to use while fetching records.
                See :ref:`Parameters` for valid options.
            params: Additional query params to append to the URL as-is.
            offset_field: The key to use in the API response to determine whether
                there are additional pages to retrieve.
        """
        options = options or {}
        params = params or {}

        def _get_offset_field(response: Dict[str, Any]) -> Optional[str]:
            value = response.get("pagination") or response  # see Enterprise.audit_log
            field_names = offset_field.split(".")
            while field_names:
                if not (value := value.get(field_names.pop(0))):
                    return None
            return str(value)

        while True:
            response = self.request(
                method=method,
                url=url,
                fallback=fallback,
                options=options,
                params=params,
            )
            yield response
            if not isinstance(response, dict):
                return
            if not (offset := _get_offset_field(response)):
                return
            params = {**params, offset_field: offset}

    def chunked(self, iterable: Sequence[T]) -> Iterator[Sequence[T]]:
        """
        Iterate through chunks of the given sequence that are equal in size
        to the maximum number of records per request allowed by the API.
        """
        return chunked(iterable, self.MAX_RECORDS_PER_REQUEST)

    @enterprise_only
    def enterprise(self, enterprise_account_id: str) -> Enterprise:
        """
        Build an object representing an enterprise account.
        """
        return Enterprise(self, enterprise_account_id)


import pyairtable.api.base  # noqa
import pyairtable.api.table  # noqa
