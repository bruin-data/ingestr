import filetype

from contextlib import contextmanager

@contextmanager
def load_dlt_file(path: str):
    """
    load_dlt_file reads dlt loader files. It handles different loader file formats
    automatically. It returns a generator that yield data items as a python dict
    """
    kind = filetype.guess(path)
    pass
