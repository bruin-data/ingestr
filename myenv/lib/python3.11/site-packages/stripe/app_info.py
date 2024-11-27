# -*- coding: utf-8 -*-
"""
The stripe.app_info package is deprecated, please change your
imports to import from stripe directly.
From:
  from stripe.app_info import AppInfo
To:
  from stripe import AppInfo
"""

from typing_extensions import TYPE_CHECKING

# No deprecation warning is raised here, because it would happen
# on every import of `stripe/__init__.py` otherwise. Since that
# module declares its own `app_info` name, this module becomes
# practically impossible to import anyway.

if not TYPE_CHECKING:
    from stripe._app_info import (  # noqa
        AppInfo,
    )
