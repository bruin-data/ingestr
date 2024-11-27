import logging
from dataclasses import dataclass

import requests
import lz4.frame
import threading
import time

from databricks.sql.thrift_api.TCLIService.ttypes import TSparkArrowResultLink

logger = logging.getLogger(__name__)


@dataclass
class DownloadableResultSettings:
    """
    Class for settings common to each download handler.

    Attributes:
        is_lz4_compressed (bool): Whether file is expected to be lz4 compressed.
        link_expiry_buffer_secs (int): Time in seconds to prevent download of a link before it expires. Default 0 secs.
        download_timeout (int): Timeout for download requests. Default 60 secs.
        max_consecutive_file_download_retries (int): Number of consecutive download retries before shutting down.
    """

    is_lz4_compressed: bool
    link_expiry_buffer_secs: int = 0
    download_timeout: int = 60
    max_consecutive_file_download_retries: int = 0


class ResultSetDownloadHandler(threading.Thread):
    def __init__(
        self,
        downloadable_result_settings: DownloadableResultSettings,
        t_spark_arrow_result_link: TSparkArrowResultLink,
    ):
        super().__init__()
        self.settings = downloadable_result_settings
        self.result_link = t_spark_arrow_result_link
        self.is_download_scheduled = False
        self.is_download_finished = threading.Event()
        self.is_file_downloaded_successfully = False
        self.is_link_expired = False
        self.is_download_timedout = False
        self.result_file = None

    def is_file_download_successful(self) -> bool:
        """
        Check and report if cloud fetch file downloaded successfully.

        This function will block until a file download finishes or until a timeout.
        """
        timeout = (
            self.settings.download_timeout
            if self.settings.download_timeout > 0
            else None
        )
        try:
            if not self.is_download_finished.wait(timeout=timeout):
                self.is_download_timedout = True
                logger.debug(
                    "Cloud fetch download timed out after {} seconds for link representing rows {} to {}".format(
                        self.settings.download_timeout,
                        self.result_link.startRowOffset,
                        self.result_link.startRowOffset + self.result_link.rowCount,
                    )
                )
                return False
        except Exception as e:
            logger.error(e)
            return False
        return self.is_file_downloaded_successfully

    def run(self):
        """
        Download the file described in the cloud fetch link.

        This function checks if the link has or is expiring, gets the file via a requests session, decompresses the
        file, and signals to waiting threads that the download is finished and whether it was successful.
        """
        self._reset()

        # Check if link is already expired or is expiring
        if ResultSetDownloadHandler.check_link_expired(
            self.result_link, self.settings.link_expiry_buffer_secs
        ):
            self.is_link_expired = True
            return

        session = requests.Session()
        session.timeout = self.settings.download_timeout

        try:
            # Get the file via HTTP request
            response = session.get(self.result_link.fileLink)

            if not response.ok:
                self.is_file_downloaded_successfully = False
                return

            # Save (and decompress if needed) the downloaded file
            compressed_data = response.content
            decompressed_data = (
                ResultSetDownloadHandler.decompress_data(compressed_data)
                if self.settings.is_lz4_compressed
                else compressed_data
            )
            self.result_file = decompressed_data

            # The size of the downloaded file should match the size specified from TSparkArrowResultLink
            self.is_file_downloaded_successfully = (
                len(self.result_file) == self.result_link.bytesNum
            )
        except Exception as e:
            logger.error(e)
            self.is_file_downloaded_successfully = False

        finally:
            session and session.close()
            # Awaken threads waiting for this to be true which signals the run is complete
            self.is_download_finished.set()

    def _reset(self):
        """
        Reset download-related flags for every retry of run()
        """
        self.is_file_downloaded_successfully = False
        self.is_link_expired = False
        self.is_download_timedout = False
        self.is_download_finished = threading.Event()

    @staticmethod
    def check_link_expired(
        link: TSparkArrowResultLink, expiry_buffer_secs: int
    ) -> bool:
        """
        Check if a link has expired or will expire.

        Expiry buffer can be set to avoid downloading files that has not expired yet when the function is called,
        but may expire before the file has fully downloaded.
        """
        current_time = int(time.time())
        if (
            link.expiryTime < current_time
            or link.expiryTime - current_time < expiry_buffer_secs
        ):
            return True
        return False

    @staticmethod
    def decompress_data(compressed_data: bytes) -> bytes:
        """
        Decompress lz4 frame compressed data.

        Decompresses data that has been lz4 compressed, either via the whole frame or by series of chunks.
        """
        uncompressed_data, bytes_read = lz4.frame.decompress(
            compressed_data, return_bytes_read=True
        )
        # The last cloud fetch file of the entire result is commonly punctuated by frequent end-of-frame markers.
        # Full frame decompression above will short-circuit, so chunking is necessary
        if bytes_read < len(compressed_data):
            d_context = lz4.frame.create_decompression_context()
            start = 0
            uncompressed_data = bytearray()
            while start < len(compressed_data):
                data, num_bytes, is_end = lz4.frame.decompress_chunk(
                    d_context, compressed_data[start:]
                )
                uncompressed_data += data
                start += num_bytes
        return uncompressed_data
