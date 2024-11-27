# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.nested_resource_class_methods package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.nested_resource_class_methods import nested_resource_class_methods
    To:
      from stripe import nested_resource_class_methods
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe._nested_resource_class_methods import (  # noqa
        nested_resource_class_methods,
    )
