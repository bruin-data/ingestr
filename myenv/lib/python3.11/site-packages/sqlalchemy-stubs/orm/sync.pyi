from typing import Any

from . import attributes as attributes
from . import exc as exc
from .. import util as util

def populate(
    source: Any,
    source_mapper: Any,
    dest: Any,
    dest_mapper: Any,
    synchronize_pairs: Any,
    uowcommit: Any,
    flag_cascaded_pks: Any,
) -> None: ...
def bulk_populate_inherit_keys(
    source_dict: Any, source_mapper: Any, synchronize_pairs: Any
) -> None: ...
def clear(dest: Any, dest_mapper: Any, synchronize_pairs: Any) -> None: ...
def update(
    source: Any,
    source_mapper: Any,
    dest: Any,
    old_prefix: Any,
    synchronize_pairs: Any,
) -> None: ...
def populate_dict(
    source: Any, source_mapper: Any, dict_: Any, synchronize_pairs: Any
) -> None: ...
def source_modified(
    uowcommit: Any, source: Any, source_mapper: Any, synchronize_pairs: Any
): ...
