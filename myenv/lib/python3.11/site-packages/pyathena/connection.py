# -*- coding: utf-8 -*-
from __future__ import annotations

import logging
import os
import time
from typing import (
    TYPE_CHECKING,
    Any,
    Dict,
    Generic,
    List,
    Optional,
    Type,
    TypeVar,
    Union,
    cast,
    overload,
)

from boto3.session import Session
from botocore.config import Config

import pyathena
from pyathena.common import BaseCursor, CursorIterator
from pyathena.converter import Converter
from pyathena.cursor import Cursor
from pyathena.error import NotSupportedError
from pyathena.formatter import DefaultParameterFormatter, Formatter
from pyathena.util import RetryConfig

if TYPE_CHECKING:
    from botocore.client import BaseClient

_logger = logging.getLogger(__name__)  # type: ignore


ConnectionCursor = TypeVar("ConnectionCursor", bound=BaseCursor)
FunctionalCursor = TypeVar("FunctionalCursor", bound=BaseCursor)


class Connection(Generic[ConnectionCursor]):
    _ENV_S3_STAGING_DIR: str = "AWS_ATHENA_S3_STAGING_DIR"
    _ENV_WORK_GROUP: str = "AWS_ATHENA_WORK_GROUP"
    _SESSION_PASSING_ARGS: List[str] = [
        "aws_access_key_id",
        "aws_secret_access_key",
        "aws_session_token",
        "region_name",
        "botocore_session",
        "profile_name",
    ]
    _CLIENT_PASSING_ARGS: List[str] = [
        "aws_access_key_id",
        "aws_secret_access_key",
        "aws_session_token",
        "api_version",
        "use_ssl",
        "verify",
        "endpoint_url",
        "region_name",
        "config",
    ]

    @overload
    def __init__(
        self: Connection[Cursor],
        s3_staging_dir: Optional[str] = ...,
        region_name: Optional[str] = ...,
        schema_name: Optional[str] = ...,
        catalog_name: Optional[str] = ...,
        work_group: Optional[str] = ...,
        poll_interval: float = ...,
        encryption_option: Optional[str] = ...,
        kms_key: Optional[str] = ...,
        profile_name: Optional[str] = ...,
        role_arn: Optional[str] = ...,
        role_session_name: str = ...,
        external_id: Optional[str] = ...,
        serial_number: Optional[str] = ...,
        duration_seconds: int = ...,
        converter: Optional[Converter] = ...,
        formatter: Optional[Formatter] = ...,
        retry_config: Optional[RetryConfig] = ...,
        cursor_class: None = ...,
        cursor_kwargs: Optional[Dict[str, Any]] = ...,
        kill_on_interrupt: bool = ...,
        session: Optional[Session] = ...,
        config: Optional[Config] = ...,
        result_reuse_enable: bool = ...,
        result_reuse_minutes: int = ...,
        **kwargs,
    ) -> None:
        ...

    @overload
    def __init__(
        self: Connection[ConnectionCursor],
        s3_staging_dir: Optional[str] = ...,
        region_name: Optional[str] = ...,
        schema_name: Optional[str] = ...,
        catalog_name: Optional[str] = ...,
        work_group: Optional[str] = ...,
        poll_interval: float = ...,
        encryption_option: Optional[str] = ...,
        kms_key: Optional[str] = ...,
        profile_name: Optional[str] = ...,
        role_arn: Optional[str] = ...,
        role_session_name: str = ...,
        external_id: Optional[str] = ...,
        serial_number: Optional[str] = ...,
        duration_seconds: int = ...,
        converter: Optional[Converter] = ...,
        formatter: Optional[Formatter] = ...,
        retry_config: Optional[RetryConfig] = ...,
        cursor_class: Type[ConnectionCursor] = ...,
        cursor_kwargs: Optional[Dict[str, Any]] = ...,
        kill_on_interrupt: bool = ...,
        session: Optional[Session] = ...,
        config: Optional[Config] = ...,
        result_reuse_enable: bool = ...,
        result_reuse_minutes: int = ...,
        **kwargs,
    ) -> None:
        ...

    def __init__(
        self,
        s3_staging_dir: Optional[str] = None,
        region_name: Optional[str] = None,
        schema_name: Optional[str] = "default",
        catalog_name: Optional[str] = "awsdatacatalog",
        work_group: Optional[str] = None,
        poll_interval: float = 1,
        encryption_option: Optional[str] = None,
        kms_key: Optional[str] = None,
        profile_name: Optional[str] = None,
        role_arn: Optional[str] = None,
        role_session_name: str = f"PyAthena-session-{int(time.time())}",
        external_id: Optional[str] = None,
        serial_number: Optional[str] = None,
        duration_seconds: int = 3600,
        converter: Optional[Converter] = None,
        formatter: Optional[Formatter] = None,
        retry_config: Optional[RetryConfig] = None,
        cursor_class: Optional[Type[ConnectionCursor]] = cast(Type[ConnectionCursor], Cursor),
        cursor_kwargs: Optional[Dict[str, Any]] = None,
        kill_on_interrupt: bool = True,
        session: Optional[Session] = None,
        config: Optional[Config] = None,
        result_reuse_enable: bool = False,
        result_reuse_minutes: int = CursorIterator.DEFAULT_RESULT_REUSE_MINUTES,
        **kwargs,
    ) -> None:
        self._kwargs = {
            **kwargs,
            "role_arn": role_arn,
            "role_session_name": role_session_name,
            "external_id": external_id,
            "serial_number": serial_number,
            "duration_seconds": duration_seconds,
        }
        if s3_staging_dir:
            self.s3_staging_dir: Optional[str] = s3_staging_dir
        else:
            self.s3_staging_dir = os.getenv(self._ENV_S3_STAGING_DIR)
        self.region_name = region_name
        self.schema_name = schema_name
        self.catalog_name = catalog_name
        if work_group:
            self.work_group: Optional[str] = work_group
        else:
            self.work_group = os.getenv(self._ENV_WORK_GROUP)
        self.poll_interval = poll_interval
        self.encryption_option = encryption_option
        self.kms_key = kms_key
        self.profile_name = profile_name
        self.config: Optional[Config] = config if config else Config()

        assert (
            self.s3_staging_dir or self.work_group
        ), "Required argument `s3_staging_dir` or `work_group` not found."

        if session:
            self._session = session
        else:
            if role_arn:
                creds = self._assume_role(
                    profile_name=self.profile_name,
                    region_name=self.region_name,
                    role_arn=role_arn,
                    role_session_name=role_session_name,
                    external_id=external_id,
                    serial_number=serial_number,
                    duration_seconds=duration_seconds,
                )
                self.profile_name = None
                self._kwargs.update(
                    {
                        "aws_access_key_id": creds["AccessKeyId"],
                        "aws_secret_access_key": creds["SecretAccessKey"],
                        "aws_session_token": creds["SessionToken"],
                    }
                )
            elif serial_number:
                creds = self._get_session_token(
                    profile_name=self.profile_name,
                    region_name=self.region_name,
                    serial_number=serial_number,
                    duration_seconds=duration_seconds,
                )
                self.profile_name = None
                self._kwargs.update(
                    {
                        "aws_access_key_id": creds["AccessKeyId"],
                        "aws_secret_access_key": creds["SecretAccessKey"],
                        "aws_session_token": creds["SessionToken"],
                    }
                )
            self._session = Session(
                region_name=self.region_name,
                profile_name=self.profile_name,
                **self._session_kwargs,
            )

        if not self.config.user_agent_extra or (
            pyathena.user_agent_extra not in self.config.user_agent_extra
        ):
            self.config.user_agent_extra = (
                f"{pyathena.user_agent_extra}"
                f"{' ' + self.config.user_agent_extra if self.config.user_agent_extra else ''}"
            )
        self._client = self._session.client(
            "athena", region_name=self.region_name, config=self.config, **self._client_kwargs
        )
        self._converter = converter
        self._formatter = formatter if formatter else DefaultParameterFormatter()
        self._retry_config = retry_config if retry_config else RetryConfig()
        self.cursor_class = cast(Type[ConnectionCursor], cursor_class)
        self.cursor_kwargs = cursor_kwargs if cursor_kwargs else dict()
        self.kill_on_interrupt = kill_on_interrupt
        self.result_reuse_enable = result_reuse_enable
        self.result_reuse_minutes = result_reuse_minutes

    def _assume_role(
        self,
        profile_name: Optional[str],
        region_name: Optional[str],
        role_arn: str,
        role_session_name: str,
        external_id: Optional[str],
        serial_number: Optional[str],
        duration_seconds: int,
    ) -> Dict[str, Any]:
        session = Session(
            region_name=region_name, profile_name=profile_name, **self._session_kwargs
        )
        client = session.client(
            "sts", region_name=region_name, config=self.config, **self._client_kwargs
        )
        request = {
            "RoleArn": role_arn,
            "RoleSessionName": role_session_name,
            "DurationSeconds": duration_seconds,
        }
        if external_id:
            request.update(
                {
                    "ExternalId": external_id,
                }
            )
        if serial_number:
            token_code = input("Enter the MFA code: ")
            request.update(
                {
                    "SerialNumber": serial_number,
                    "TokenCode": token_code,
                }
            )
        response = client.assume_role(**request)
        creds: Dict[str, Any] = response["Credentials"]
        return creds

    def _get_session_token(
        self,
        profile_name: Optional[str],
        region_name: Optional[str],
        serial_number: Optional[str],
        duration_seconds: int,
    ) -> Dict[str, Any]:
        session = Session(profile_name=profile_name, **self._session_kwargs)
        client = session.client(
            "sts", region_name=region_name, config=self.config, **self._client_kwargs
        )
        token_code = input("Enter the MFA code: ")
        request = {
            "DurationSeconds": duration_seconds,
            "SerialNumber": serial_number,
            "TokenCode": token_code,
        }
        response = client.get_session_token(**request)
        creds: Dict[str, Any] = response["Credentials"]
        return creds

    @property
    def _session_kwargs(self) -> Dict[str, Any]:
        return {k: v for k, v in self._kwargs.items() if k in self._SESSION_PASSING_ARGS}

    @property
    def _client_kwargs(self) -> Dict[str, Any]:
        return {k: v for k, v in self._kwargs.items() if k in self._CLIENT_PASSING_ARGS}

    @property
    def session(self) -> Session:
        return self._session

    @property
    def client(self) -> "BaseClient":
        return self._client

    @property
    def retry_config(self) -> RetryConfig:
        return self._retry_config

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()

    @overload
    def cursor(self, cursor: None = ..., **kwargs) -> ConnectionCursor:
        ...

    @overload
    def cursor(self, cursor: Type[FunctionalCursor], **kwargs) -> FunctionalCursor:
        ...

    def cursor(
        self, cursor: Optional[Type[FunctionalCursor]] = None, **kwargs
    ) -> Union[FunctionalCursor, ConnectionCursor]:
        kwargs.update(self.cursor_kwargs)
        _cursor = cursor or self.cursor_class
        converter = kwargs.pop("converter", self._converter)
        if not converter:
            converter = _cursor.get_default_converter(kwargs.get("unload", False))
        return _cursor(
            connection=self,
            converter=converter,
            formatter=kwargs.pop("formatter", self._formatter),
            retry_config=kwargs.pop("retry_config", self._retry_config),
            s3_staging_dir=kwargs.pop("s3_staging_dir", self.s3_staging_dir),
            schema_name=kwargs.pop("schema_name", self.schema_name),
            catalog_name=kwargs.pop("catalog_name", self.catalog_name),
            work_group=kwargs.pop("work_group", self.work_group),
            poll_interval=kwargs.pop("poll_interval", self.poll_interval),
            encryption_option=kwargs.pop("encryption_option", self.encryption_option),
            kms_key=kwargs.pop("kms_key", self.kms_key),
            kill_on_interrupt=kwargs.pop("kill_on_interrupt", self.kill_on_interrupt),
            result_reuse_enable=kwargs.pop("result_reuse_enable", self.result_reuse_enable),
            result_reuse_minutes=kwargs.pop("result_reuse_minutes", self.result_reuse_minutes),
            **kwargs,
        )

    def close(self) -> None:
        pass

    def commit(self) -> None:
        pass

    def rollback(self) -> None:
        raise NotSupportedError
