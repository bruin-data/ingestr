#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

from __future__ import annotations

import logging
import warnings
from collections.abc import Mapping
from typing import Any


def getSnowLogger(
    name: str,
    extra: Mapping[str, object] | None = None,
) -> SnowLogger:
    logger = logging.getLogger(name)
    return SnowLogger(logger, extra)  # type:ignore[arg-type]


class SnowLogger(logging.LoggerAdapter):
    """Snowflake Python logger wrapper of the built-in Python logger.

    This logger wrapper supports user-provided logging info about
    file name, function name and line number. This wrapper can be
    used in Cython code (.pyx).
    """

    def debug(  # type: ignore[override]
        self,
        msg: str,
        path_name: str | None = None,
        func_name: str | None = None,
        *args: Any,
        **kwargs: Any,
    ) -> None:
        self.log(logging.DEBUG, msg, path_name, func_name, *args, **kwargs)

    def info(  # type: ignore[override]
        self,
        msg: str,
        path_name: str | None = None,
        func_name: str | None = None,
        *args: Any,
        **kwargs: Any,
    ) -> None:
        self.log(logging.INFO, msg, path_name, func_name, *args, **kwargs)

    def warning(  # type: ignore[override]
        self,
        msg: str,
        path_name: str | None = None,
        func_name: str | None = None,
        *args: Any,
        **kwargs: Any,
    ) -> None:
        self.log(logging.WARNING, msg, path_name, func_name, *args, **kwargs)

    def warn(  # type: ignore[override]
        self,
        msg: str,
        path_name: str | None = None,
        func_name: str | None = None,
        *args: Any,
        **kwargs: Any,
    ) -> None:
        warnings.warn(
            "The 'warn' method is deprecated, " "use 'warning' instead",
            DeprecationWarning,
            stacklevel=2,
        )
        self.warning(msg, path_name, func_name, *args, **kwargs)

    def error(  # type: ignore[override]
        self,
        msg: str,
        path_name: str | None = None,
        func_name: str | None = None,
        *args: Any,
        **kwargs: Any,
    ) -> None:
        self.log(logging.ERROR, msg, path_name, func_name, *args, **kwargs)

    def exception(  # type: ignore[override]
        self,
        msg: str,
        path_name: str | None = None,
        func_name: str | None = None,
        *args: Any,
        exc_info: bool = True,
        **kwargs: Any,
    ) -> None:
        """Convenience method for logging an ERROR with exception information."""
        self.error(msg, path_name, func_name, *args, exc_info=exc_info, **kwargs)

    def critical(  # type: ignore[override]
        self,
        msg: str,
        path_name: str | None = None,
        func_name: str | None = None,
        *args: Any,
        **kwargs: Any,
    ) -> None:
        self.log(logging.CRITICAL, msg, path_name, func_name, *args, **kwargs)

    fatal = critical

    def log(  # type: ignore[override]
        self,
        level: int,
        msg: str,
        path_name: str | None = None,
        func_name: str | None = None,
        line_num: int = 0,
        *args: Any,
        **kwargs: Any,
    ) -> None:
        """Generalized log method of SnowLogger wrapper.

        Args:
            level: Logging level.
            msg: Logging message.
            path_name: Absolute or relative path of the file where the logger gets called.
            func_name: Function inside which the logger gets called.
            line_num: Line number at which the logger gets called.
        """
        if not path_name:
            path_name = "path_name not provided"
        if not func_name:
            func_name = "func_name not provided"
        if not isinstance(level, int):
            if logging.raiseExceptions:
                raise TypeError("level must be an integer")
            else:
                return
        if self.logger.isEnabledFor(level):
            record = self.logger.makeRecord(
                self.logger.name,
                level,
                path_name,
                line_num,
                msg,
                args,
                None,
                func_name,
                **kwargs,
            )
            self.logger.handle(record)
