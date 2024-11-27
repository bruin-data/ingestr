import typing

from redshift_connector.error import ArrayDimensionsNotConsistentError


def walk_array(arr: typing.List) -> typing.Generator:
    for i, v in enumerate(arr):
        if isinstance(v, list):
            for a, i2, v2 in walk_array(v):
                yield a, i2, v2
        else:
            yield arr, i, v


def array_find_first_element(arr: typing.List) -> typing.Any:
    for v in array_flatten(arr):
        if v is not None:
            return v
    return None


def array_flatten(arr: typing.List) -> typing.Generator:
    for v in arr:
        if isinstance(v, list):
            for v2 in array_flatten(v):
                yield v2
        else:
            yield v


def array_check_dimensions(arr: typing.List) -> typing.List:
    if len(arr) > 0:
        v0 = arr[0]
        if isinstance(v0, list):
            req_len = len(v0)
            req_inner_lengths = array_check_dimensions(v0)
            for v in arr:
                inner_lengths = array_check_dimensions(v)
                if len(v) != req_len or inner_lengths != req_inner_lengths:
                    raise ArrayDimensionsNotConsistentError("array dimensions not consistent")
            retval = [req_len]
            retval.extend(req_inner_lengths)
            return retval
        else:
            # make sure nothing else at this level is a list
            for v in arr:
                if isinstance(v, list):
                    raise ArrayDimensionsNotConsistentError("array dimensions not consistent")
    return []


def array_has_null(arr: typing.List) -> bool:
    for v in array_flatten(arr):
        if v is None:
            return True
    return False


def array_dim_lengths(arr: typing.List) -> typing.List:
    len_arr = len(arr)
    retval = [len_arr]
    if len_arr > 0:
        v0 = arr[0]
        if isinstance(v0, list):
            retval.extend(array_dim_lengths(v0))
    return retval
