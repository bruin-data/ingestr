"""Readers for HTTP file sources"""

import io
from typing import Any, Iterator, Optional
from urllib.parse import urlparse

import requests
from dlt.sources import TDataItems


class HttpReader:
    """Reader for HTTP-based file sources"""

    def __init__(self, url: str, file_format: Optional[str] = None):
        self.url = url
        self.file_format = file_format or self._infer_format(url)

        if self.file_format not in ["csv", "json", "parquet"]:
            raise ValueError(
                f"Unsupported file format: {self.file_format}. "
                "Supported formats: csv, json, parquet"
            )

    def _infer_format(self, url: str) -> str:
        """Infer file format from URL extension"""
        parsed = urlparse(url)
        path = parsed.path.lower()

        if path.endswith(".csv"):
            return "csv"
        elif path.endswith(".json") or path.endswith(".jsonl"):
            return "json"
        elif path.endswith(".parquet"):
            return "parquet"
        else:
            raise ValueError(
                f"Cannot infer file format from URL: {url}. "
                "Please specify file_format parameter."
            )

    def _download_file(self) -> bytes:
        """Download file from URL"""
        response = requests.get(self.url, stream=True, timeout=30)
        response.raise_for_status()
        return response.content

    def read_file(self, **kwargs: Any) -> Iterator[TDataItems]:
        """Read file and yield data in chunks"""
        content = self._download_file()

        if self.file_format == "csv":
            yield from self._read_csv(content, **kwargs)
        elif self.file_format == "json":
            yield from self._read_json(content, **kwargs)
        elif self.file_format == "parquet":
            yield from self._read_parquet(content, **kwargs)

    def _read_csv(
        self, content: bytes, chunksize: int = 10000, **pandas_kwargs: Any
    ) -> Iterator[TDataItems]:
        """Read CSV file with Pandas chunk by chunk"""
        import pandas as pd  # type: ignore

        kwargs = {**{"header": "infer", "chunksize": chunksize}, **pandas_kwargs}

        file_obj = io.BytesIO(content)
        for df in pd.read_csv(file_obj, **kwargs):
            yield df.to_dict(orient="records")

    def _read_json(
        self, content: bytes, chunksize: int = 1000, **kwargs: Any
    ) -> Iterator[TDataItems]:
        """Read JSON or JSONL file"""
        from dlt.common import json

        file_obj = io.BytesIO(content)
        text = file_obj.read().decode("utf-8")

        # Try to detect if it's JSONL format (one JSON object per line)
        lines = text.strip().split("\n")

        if len(lines) > 1:
            # Likely JSONL format
            lines_chunk = []
            for line in lines:
                if line.strip():
                    lines_chunk.append(json.loads(line))
                    if len(lines_chunk) >= chunksize:
                        yield lines_chunk
                        lines_chunk = []
            if lines_chunk:
                yield lines_chunk
        else:
            # Single JSON object or array
            data = json.loads(text)
            if isinstance(data, list):
                # Chunk the list
                for i in range(0, len(data), chunksize):
                    yield data[i : i + chunksize]
            else:
                # Single object
                yield [data]

    def _read_parquet(
        self, content: bytes, chunksize: int = 10000, **kwargs: Any
    ) -> Iterator[TDataItems]:
        """Read Parquet file"""
        from pyarrow import parquet as pq  # type: ignore

        file_obj = io.BytesIO(content)
        parquet_file = pq.ParquetFile(file_obj)

        for batch in parquet_file.iter_batches(batch_size=chunksize):
            yield batch.to_pylist()
