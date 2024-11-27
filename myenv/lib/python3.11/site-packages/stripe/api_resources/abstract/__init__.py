# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from typing_extensions import TYPE_CHECKING
from warnings import warn

warn(
    """
    The stripe.api_resources.abstract package is deprecated, please change your
    imports to import from stripe directly.
    From:
      from stripe.api_resources.abstract import ...
    To:
      from stripe import ...
    """,
    DeprecationWarning,
    stacklevel=2,
)
if not TYPE_CHECKING:
    from stripe.api_resources.abstract.api_resource import APIResource
    from stripe.api_resources.abstract.createable_api_resource import (
        CreateableAPIResource,
    )
    from stripe.api_resources.abstract.custom_method import custom_method
    from stripe.api_resources.abstract.deletable_api_resource import (
        DeletableAPIResource,
    )
    from stripe.api_resources.abstract.listable_api_resource import (
        ListableAPIResource,
    )
    from stripe.api_resources.abstract.nested_resource_class_methods import (
        nested_resource_class_methods,
    )
    from stripe.api_resources.abstract.searchable_api_resource import (
        SearchableAPIResource,
    )
    from stripe.api_resources.abstract.singleton_api_resource import (
        SingletonAPIResource,
    )
    from stripe.api_resources.abstract.test_helpers import (
        APIResourceTestHelpers,
    )
    from stripe.api_resources.abstract.updateable_api_resource import (
        UpdateableAPIResource,
    )
    from stripe.api_resources.abstract.verify_mixin import VerifyMixin
