import csv
import gzip
import json
import subprocess
from contextlib import contextmanager
from typing import Generator

from pyarrow.parquet import ParquetFile  # type: ignore

PARQUET_BATCH_SIZE = 64


class UnsupportedLoaderFileFormat(Exception):
    pass


def load_dlt_file(filepath: str) -> Generator:
    """
    load_dlt_file reads dlt loader files. It handles different loader file formats
    automatically. It returns a generator that yield data items as a python dict
    """
    result = subprocess.run(
        ["file", "-b", filepath],
        check=True,
        capture_output=True,
        text=True,
    )

    filetype = result.stdout.strip()
    with factory(filetype, filepath) as reader:
        yield from reader


def factory(filetype: str, filepath: str):
    # ???(turtledev): can dlt produce non-gizpped jsonl files?
    if filetype.startswith("gzip"):
        return jsonlfile(filepath)
    elif filetype.startswith("CSV"):
        return csvfile(filepath)
    elif filetype.startswith("Apache Parquet"):
        return parquetfile(filepath)
    else:
        raise UnsupportedLoaderFileFormat(filetype)


@contextmanager
def jsonlfile(filepath: str):
    def reader(fd):
        for line in fd:
            yield json.loads(line.decode().strip())

    with gzip.open(filepath) as fd:
        yield reader(fd)


@contextmanager
def csvfile(filepath: str):
    with open(filepath, "r") as fd:
        yield csv.DictReader(fd)


@contextmanager
def parquetfile(filepath: str):
    def reader(pf: ParquetFile):
        for batch in pf.iter_batches(PARQUET_BATCH_SIZE):
            yield from batch.to_pylist()

    with open(filepath, "rb") as fd:
        yield reader(ParquetFile(fd))
