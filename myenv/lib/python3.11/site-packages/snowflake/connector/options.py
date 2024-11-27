#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import importlib
import os
import warnings
from importlib.metadata import distributions
from logging import getLogger
from types import ModuleType
from typing import Union

from packaging.requirements import Requirement

from . import errors

logger = getLogger(__name__)

"""This module helps to manage optional dependencies.

It implements MissingOptionalDependency as a base class. If a module is unavailable an instance of this will be
returned. These derived classes can be seen in this file pre-defined. The point of these classes is that if someone
tries to use pyarrow code then by importing pyarrow from this module if they did pyarrow.xxx then that would raise
a MissingDependencyError.
"""


class MissingOptionalDependency:
    """A class to replace missing dependencies.

    The only thing this class is supposed to do is raise a MissingDependencyError when __getattr__ is called.
    This will be triggered whenever module.member is going to be called.
    """

    _dep_name = "not set"

    def __getattr__(self, item):
        raise errors.MissingDependencyError(self._dep_name)


class MissingPandas(MissingOptionalDependency):
    """The class is specifically for pandas optional dependency."""

    _dep_name = "pandas"


class MissingKeyring(MissingOptionalDependency):
    """The class is specifically for sso optional dependency."""

    _dep_name = "keyring"


ModuleLikeObject = Union[ModuleType, MissingOptionalDependency]


def warn_incompatible_dep(
    dep_name: str, installed_ver: str, expected_ver: Requirement
) -> None:
    warnings.warn(
        "You have an incompatible version of '{}' installed ({}), please install a version that "
        "adheres to: '{}'".format(dep_name, installed_ver, expected_ver),
        stacklevel=2,
    )


def _import_or_missing_pandas_option() -> (
    tuple[ModuleLikeObject, ModuleLikeObject, bool]
):
    """This function tries importing the following packages: pandas, pyarrow.

    If available it returns pandas and pyarrow packages with a flag of whether they were imported.
    It also warns users if they have an unsupported pyarrow version installed if possible.
    """
    try:
        pandas = importlib.import_module("pandas")
        # since we enable relative imports without dots this import gives us an issues when ran from test directory
        from pandas import DataFrame  # NOQA

        pyarrow = importlib.import_module("pyarrow")

        # set default memory pool to system for pyarrow to_pandas conversion
        if "ARROW_DEFAULT_MEMORY_POOL" not in os.environ:
            os.environ["ARROW_DEFAULT_MEMORY_POOL"] = "system"

        # Check whether we have the currently supported pyarrow installed
        installed_packages = {
            package.metadata["Name"]: package for package in distributions()
        }
        if {"pyarrow", "snowflake-connector-python"} <= installed_packages.keys():
            dependencies = installed_packages[
                "snowflake-connector-python"
            ].metadata.get_all("Requires-Dist", [])
            pandas_pyarrow_extra = None
            for dependency in dependencies:
                dep = Requirement(dependency)
                if (
                    dep.marker is not None
                    and dep.marker.evaluate({"extra": "pandas"})
                    and dep.name == "pyarrow"
                ):
                    pandas_pyarrow_extra = dep
                    break

            installed_pyarrow_version = installed_packages["pyarrow"].version
            if not pandas_pyarrow_extra.specifier.contains(installed_pyarrow_version):
                warn_incompatible_dep(
                    "pyarrow", installed_pyarrow_version, pandas_pyarrow_extra
                )

        else:
            logger.info(
                "Cannot determine if compatible pyarrow is installed because of missing package(s) from "
                "{}".format(list(installed_packages.keys()))
            )
        return pandas, pyarrow, True
    except ImportError:
        return MissingPandas(), MissingPandas(), False


def _import_or_missing_keyring_option() -> tuple[ModuleLikeObject, bool]:
    """This function tries importing the following packages: keyring.

    If available it returns keyring package with a flag of whether it was imported.
    """
    try:
        keyring = importlib.import_module("keyring")
        return keyring, True
    except ImportError:
        return MissingKeyring(), False


# Create actual constants to be imported from this file
pandas, pyarrow, installed_pandas = _import_or_missing_pandas_option()
keyring, installed_keyring = _import_or_missing_keyring_option()
