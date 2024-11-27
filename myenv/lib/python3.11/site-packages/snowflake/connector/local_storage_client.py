#!/usr/bin/env python
#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import os
import shutil
from logging import getLogger
from typing import TYPE_CHECKING, Any

from .constants import FileHeader, ResultStatus
from .storage_client import SnowflakeStorageClient
from .vendored import requests

if TYPE_CHECKING:  # pragma: no cover
    from .file_transfer_agent import SnowflakeFileMeta

logger = getLogger(__name__)


class SnowflakeLocalStorageClient(SnowflakeStorageClient):
    def __init__(
        self,
        meta: SnowflakeFileMeta,
        stage_info: dict[str, Any],
        chunk_size: int,
    ) -> None:
        super().__init__(meta, stage_info, chunk_size)
        self.data_file = meta.src_file_name
        self.full_dst_file_name: str = os.path.join(
            stage_info["location"], os.path.basename(meta.dst_file_name)
        )
        if meta.local_location:
            src_file_name = self.data_file
            if src_file_name.startswith("/"):
                src_file_name = src_file_name[1:]
            self.stage_file_name: str = os.path.join(
                stage_info["location"], src_file_name
            )
            self.full_dst_file_name = os.path.join(
                meta.local_location, os.path.basename(meta.dst_file_name)
            )

    def get_file_header(self, filename: str) -> FileHeader | None:
        """
        Notes:
            Checks whether the file exits in specified directory, does not return FileHeader
        """
        if os.path.isfile(filename):
            return FileHeader(None, os.stat(filename).st_size, None)
        return None

    def download_chunk(self, chunk_id: int) -> None:
        with open(self.stage_file_name, "rb") as sfd:
            with open(
                os.path.join(
                    self.meta.local_location,
                    os.path.basename(self.intermediate_dst_path),
                ),
                "rb+",
            ) as tfd:
                if self.num_of_chunks == 1:
                    tfd.write(sfd.read())
                else:
                    tfd.seek(chunk_id * self.chunk_size)
                    sfd.seek(chunk_id * self.chunk_size)
                    tfd.write(sfd.read(self.chunk_size))

    def finish_download(self) -> None:
        shutil.move(self.intermediate_dst_path, self.full_dst_file_name)
        self.meta.dst_file_size = os.stat(self.full_dst_file_name).st_size
        self.meta.result_status = ResultStatus.DOWNLOADED

    def _has_expired_token(self, response: requests.Response) -> bool:
        return False

    def prepare_upload(self) -> None:
        super().prepare_upload()
        with open(self.full_dst_file_name, "wb+") as fd:
            fd.truncate(self.meta.upload_size)

    def _upload_chunk(self, chunk_id: int, chunk: bytes) -> None:
        with open(self.full_dst_file_name, "rb+") as tfd:
            tfd.seek(chunk_id * self.chunk_size)
            tfd.write(chunk)

    def finish_upload(self) -> None:
        self.meta.result_status = ResultStatus.UPLOADED
        self.meta.dst_file_size = self.meta.upload_size
