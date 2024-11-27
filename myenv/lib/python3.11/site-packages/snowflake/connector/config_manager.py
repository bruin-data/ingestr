#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import itertools
import logging
import os
import stat
import warnings
from collections.abc import Iterable
from operator import methodcaller
from pathlib import Path
from typing import Any, Callable, Literal, NamedTuple, TypeVar
from warnings import warn

import tomlkit
from tomlkit.items import Table

from snowflake.connector.compat import IS_WINDOWS
from snowflake.connector.constants import CONFIG_FILE, CONNECTIONS_FILE
from snowflake.connector.errors import (
    ConfigManagerError,
    ConfigSourceError,
    Error,
    MissingConfigOptionError,
)

_T = TypeVar("_T")

LOGGER = logging.getLogger(__name__)
READABLE_BY_OTHERS = stat.S_IRGRP | stat.S_IROTH


class ConfigSliceOptions(NamedTuple):
    """Class that defines settings individual configuration files."""

    check_permissions: bool = True
    only_in_slice: bool = False


class ConfigSlice(NamedTuple):
    path: Path
    options: ConfigSliceOptions
    section: str


class ConfigOption:
    """ConfigOption represents a flag/setting.

    This class knows how to read the value out of all different sources and implements
    order of precedence between them.

    It also provides value parsing and verification.

    Attributes:
        name: Name of this ConfigOption.
        parse_str: A function that can turn str to the desired type, useful
          for reading value from environmental variable.
        choices: An iterable of all possible values that are allowed for
          this option.
        env_name: Environmental variable value should be read from, if not
          supplied, we'll construct this. False disables reading from
          environmental variables, None uses the auto generated variable name
          and explicitly provided string overwrites the default one.
        default: The value we should resolve to when the option is not defined
          in any of the sources. When it's None we treat that as there's no
          default value.
        _root_manager: Reference to the root manager. Used to efficiently
          refer to cached config file. Is supplied by the parent
          ConfigManager.
        _nest_path: The names of the ConfigManagers that this option is
          nested in. Used to be able to efficiently resolve where to retrieve
          value out of the configuration file and construct environment
          variable name. This is supplied by the parent ConfigManager.
    """

    def __init__(
        self,
        *,
        name: str,
        parse_str: Callable[[str], _T] | None = None,
        choices: Iterable[Any] | None = None,
        env_name: str | None | Literal[False] = None,
        default: Any | None = None,
        _root_manager: ConfigManager | None = None,
        _nest_path: list[str] | None,
    ) -> None:
        """Create a config option that can read values from different sources.

        Args:
            name: Name to assign to this ConfigOption.
            parse_str: String parser function for this instance.
            choices: List of possible values for this instance.
            env_name: Environmental variable name value should be read from.
              Providing a string will use that environment variable, False disables
              reading value from environmental variables and the default None generates
              an environmental variable name for it using the _nest_path and name.
            default: Default value for the option. Used in case the value is
              is not defined in any of the sources.
            _root_manager: Reference to the root manager. Should be supplied by
              the parent ConfigManager.
            _nest_path: The names of the ConfigManagers that this option is
              nested in. This is supplied by the parent ConfigManager.
        """
        if _root_manager is None:
            raise TypeError("_root_manager cannot be None")
        if _nest_path is None:
            raise TypeError("_nest_path cannot be None")
        self.name = name
        self.parse_str = parse_str
        self.choices = choices
        self._nest_path = _nest_path + [name]
        self._root_manager: ConfigManager = _root_manager
        self.env_name = env_name
        self.default = default

    def value(self) -> Any:
        """Retrieve a value of option.

        This function implements order of precedence between different sources.
        """
        source = "environment variable"
        loaded_env, value = self._get_env()
        if not loaded_env:
            try:
                value = self._get_config()
                source = "configuration file"
            except MissingConfigOptionError:
                if self.default is not None:
                    source = "default_value"
                    value = self.default
                else:
                    raise
        if self.choices and value not in self.choices:
            raise ConfigSourceError(
                f"The value of {self.option_name} read from "
                f"{source} is not part of {self.choices}"
            )
        return value

    @property
    def option_name(self) -> str:
        """User-friendly name of the config option. Includes self._nest_path."""
        return ".".join(self._nest_path[1:])

    @property
    def default_env_name(self) -> str:
        """The default environmental variable name for this option."""
        pieces = map(methodcaller("upper"), self._nest_path[1:])
        return f"SNOWFLAKE_{'_'.join(pieces)}"

    def _get_env(self) -> tuple[bool, str | _T | None]:
        """Get value from environment variable if possible.

        Returns whether it was able to load the data and the loaded value
        itself.
        """
        if self.env_name is False:
            return False, None
        if self.env_name is not None:
            env_name = self.env_name
        else:
            # Generate environment name if it was not explicitly supplied,
            #  and isn't disabled
            env_name = self.default_env_name
        env_var = os.environ.get(env_name)
        if env_var is None:
            return False, None
        loaded_var: str | _T = env_var
        if self.parse_str is not None:
            loaded_var = self.parse_str(env_var)
        if isinstance(loaded_var, (Table, tomlkit.TOMLDocument)):
            # If we got a TOML table we probably want it in dictionary form
            return True, loaded_var.value
        return True, loaded_var

    def _get_config(self) -> Any:
        """Get value from the cached config file if possible.

        Since this is the last resource for retrieving the value it raises
        a MissingConfigOptionError if it's unable to find this option.
        """
        if (
            self._root_manager.conf_file_cache is None
            and self._root_manager.file_path is not None
        ):
            self._root_manager.read_config()
        e = self._root_manager.conf_file_cache
        if e is None:
            raise ConfigManagerError(
                f"Root manager '{self._root_manager.name}' is missing file_path",
            )
        for k in self._nest_path[1:]:
            try:
                e = e[k]
            except tomlkit.exceptions.NonExistentKey:
                raise MissingConfigOptionError(  # TOOO: maybe a child Exception for missing option?
                    f"Configuration option '{self.option_name}' is not defined anywhere, "
                    "have you forgotten to set it in a configuration file, "
                    "or environmental variable?"
                )

        if isinstance(e, (Table, tomlkit.TOMLDocument)):
            # If we got a TOML table we probably want it in dictionary form
            return e.value
        return e


class ConfigManager:
    """Read a TOML configuration file with managed multi-source precedence.

    Note that multi-source precedence is actually implemented by ConfigOption.
    This is done to make sure that special handling can be done for special options.
    As an example, think of not allowing to provide passwords by command line arguments.

    This class is updatable at run-time, allowing other libraries to add their
    own configuration options and sub-managers before resolution.

    This class can simply be thought of as nestable containers for ConfigOptions.
    It holds extra information necessary for efficient nesting purposes.

    Sub-managers allow option groups to exist, e.g. the group "snowflake.cli.output"
    could have 2 options in it: debug (boolean flag) and format (a string like "json",
    or "csv").

    When a ConfigManager tries to retrieve ConfigOptions' value the _root_manager
    will read and cache the TOML file from the file it's pointing at, afterwards
    updating the read cache can be forced by calling read_config.

    Attributes:
        name: The name of the ConfigManager. Used for nesting and emitting
          useful error messages.
        file_path: Path to the file where this and all child ConfigManagers
          should read their values out of. Can be omitted for all child
          managers. Root manager could also miss this value, but this will
          result in an exception when a value is read that isn't available from
          a preceding config source.
        conf_file_cache: Cache to store what we read from the TOML file.
        _sub_managers: List of ConfigManagers that are nested under the current manager.
        _sub_parsers: Alias for the old name of _sub_managers in the first release, please use
          the new name now, as this might get deprecated in the future.
        _options: List of ConfigOptions that are under the current manager.
        _root_manager: Reference to the root manager. Used to efficiently propagate to
          child options.
        _nest_path: The names of the ConfigManagers that this manager is nested
          under. Used to efficiently propagate to child options.
        _slices: List of config slices, where optional sections could be read from.
          Note that this feature might become deprecated soon.
    """

    def __init__(
        self,
        *,
        name: str,
        file_path: Path | None = None,
        _slices: list[ConfigSlice] | None = None,
    ):
        """Creates a new ConfigManager.

        Args:
            name: Name of this ConfigManager.
            file_path: File this manager should read values from. Can be omitted
              for all child managers.
            _slices: List of ConfigSlices to consider. A configuration file's slice is a
              section that can optionally reside in a different file. Note that this
              feature might get deprecated soon.
        """
        if _slices is None:
            _slices = list()
        self.name = name
        self.file_path = file_path
        self._slices = _slices
        # Objects holding sub-managers and options
        self._options: dict[str, ConfigOption] = dict()
        self._sub_managers: dict[str, ConfigManager] = dict()
        # Dictionary to cache read in config file
        self.conf_file_cache: tomlkit.TOMLDocument | None = None
        # Information necessary to be able to nest elements
        #  and add options in O(1)
        self._root_manager: ConfigManager = self
        self._nest_path = [name]

    @property
    def _sub_parsers(self) -> dict[str, ConfigManager]:
        """
        Alias for the old name of ``_sub_managers``.

        This used to be the original name  in the first release, please use the
        new name, as this might get deprecated in the future.
        """
        warnings.warn(
            "_sub_parsers has been deprecated, use _sub_managers instead",
            DeprecationWarning,
            stacklevel=2,
        )
        return self._sub_managers

    def read_config(
        self,
    ) -> None:
        """Read and cache config file contents.

        This function should be explicitly called if the ConfigManager's cache is
        outdated. Most likely when someone's doing development and are interactively
        adding new options to their configuration file.
        """
        if self.file_path is None:
            raise ConfigManagerError(
                "ConfigManager is trying to read config file, but it doesn't "
                "have one"
            )
        read_config_file = tomlkit.TOMLDocument()

        # Read in all of the config slices
        for filep, sliceoptions, section in itertools.chain(
            ((self.file_path, ConfigSliceOptions(), None),),
            self._slices,
        ):
            if sliceoptions.only_in_slice:
                del read_config_file[section]
            try:
                if not filep.exists():
                    continue
            except PermissionError:
                LOGGER.debug(
                    f"Fail to read configuration file from {str(filep)} due to no permission on its parent directory"
                )
                continue

            if (
                sliceoptions.check_permissions  # Skip checking if this file couldn't hold sensitive information
                # Same check as openssh does for permissions
                # https://github.com/openssh/openssh-portable/blob/2709809fd616a0991dc18e3a58dea10fb383c3f0/readconf.c#LL2264C1-L2264C1
                and filep.stat().st_mode & READABLE_BY_OTHERS != 0
                or (
                    # Windows doesn't have getuid, skip checking
                    hasattr(os, "getuid")
                    and filep.stat().st_uid != 0
                    and filep.stat().st_uid != os.getuid()
                )
            ):
                # for non-Windows, suggest change to 0600 permissions.
                chmod_message = (
                    f'.\n * To change owner, run `chown $USER "{str(filep)}"`.\n * To restrict permissions, run `chmod 0600 "{str(filep)}"`.\n'
                    if not IS_WINDOWS
                    else ""
                )

                warn(f"Bad owner or permissions on {str(filep)}{chmod_message}")
            LOGGER.debug(f"reading configuration file from {str(filep)}")
            try:
                read_config_piece = tomlkit.parse(filep.read_text())
            except Exception as e:
                raise ConfigSourceError(
                    "An unknown error happened while loading " f"'{str(filep)}'"
                ) from e
            if section is None:
                read_config_file = read_config_piece
            else:
                read_config_file[section] = read_config_piece
        self.conf_file_cache = read_config_file

    def add_option(
        self,
        *,
        option_cls: type[ConfigOption] = ConfigOption,
        **kwargs,
    ) -> None:
        """Add a ConfigOption to this ConfigManager.

        Args:
            option_cls: The class that should be instantiated. This class
              should be a child class of ConfigOption. Mainly useful for cases
              where the default ConfigOption needs to be extended, for example
              if a new configuration option source needs to be supported.
        """
        kwargs["_root_manager"] = self._root_manager
        kwargs["_nest_path"] = self._nest_path
        new_option = option_cls(
            **kwargs,
        )
        self._check_child_conflict(new_option.name)
        self._options[new_option.name] = new_option

    def _check_child_conflict(self, name: str) -> None:
        """Check if a sub-manager, or ConfigOption conflicts with given name.

        Args:
            name: Name to check against children.
        """
        if name in (self._options.keys() | self._sub_managers.keys()):
            raise ConfigManagerError(
                f"'{name}' sub-manager, or option conflicts with a child element of '{self.name}'"
            )

    def add_submanager(self, new_child: ConfigManager) -> None:
        """Nest another ConfigManager under this one.

        This function recursively updates _nest_path and _root_manager of all
        children under new_child.

        Args:
            new_child: The ConfigManager to be nested under the current one.
        Notes:
            We currently don't support re-nesting a ConfigManager. Only nest a
            manager under another one once.
        """
        self._check_child_conflict(new_child.name)
        self._sub_managers[new_child.name] = new_child

        def _root_setter_helper(node: ConfigManager):
            # Deal with ConfigManagers
            node._root_manager = self._root_manager
            node._nest_path = self._nest_path + node._nest_path
            for sub_manager in node._sub_managers.values():
                _root_setter_helper(sub_manager)
            # Deal with ConfigOptions
            for option in node._options.values():
                option._root_manager = self._root_manager
                option._nest_path = self._nest_path + option._nest_path

        _root_setter_helper(new_child)

    def add_subparser(self, *args, **kwargs) -> None:
        warnings.warn(
            "add_subparser has been deprecated, use add_submanager instead",
            DeprecationWarning,
            stacklevel=2,
        )
        return self.add_submanager(*args, **kwargs)

    def __getitem__(self, name: str) -> ConfigOption | ConfigManager:
        """Get either sub-manager, or option in this manager with name.

        If an option is retrieved, we call get() on it to return its value instead.

        Args:
            name: Name to retrieve.
        """
        if name in self._options:
            return self._options[name].value()
        if name not in self._sub_managers:
            raise ConfigSourceError(
                "No ConfigManager, or ConfigOption can be found"
                f" with the name '{name}'"
            )
        return self._sub_managers[name]


CONFIG_MANAGER = ConfigManager(
    name="CONFIG_MANAGER",
    file_path=CONFIG_FILE,
    _slices=[
        ConfigSlice(  # Optional connections file to read in connections from
            CONNECTIONS_FILE,
            ConfigSliceOptions(
                check_permissions=True,  # connections could live here, check permissions
            ),
            "connections",
        ),
    ],
)
CONFIG_MANAGER.add_option(
    name="connections",
    parse_str=tomlkit.parse,
    default=dict(),
)
CONFIG_MANAGER.add_option(
    name="default_connection_name",
    default="default",
)


def _get_default_connection_params() -> dict[str, Any]:
    def_connection_name = CONFIG_MANAGER["default_connection_name"]
    connections = CONFIG_MANAGER["connections"]
    if def_connection_name not in connections:
        raise Error(
            f"Default connection with name '{def_connection_name}' "
            "cannot be found, known ones are "
            f"{list(connections.keys())}"
        )
    return {**connections[def_connection_name]}


def __getattr__(name):
    if name == "CONFIG_PARSER":
        warnings.warn(
            "CONFIG_PARSER has been deprecated, use CONFIG_MANAGER instead",
            DeprecationWarning,
            stacklevel=2,
        )
        return CONFIG_MANAGER
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")


__all__ = [  # noqa: F822
    "ConfigOption",
    "ConfigManager",
    "CONFIG_MANAGER",
    "CONFIG_PARSER",
]
