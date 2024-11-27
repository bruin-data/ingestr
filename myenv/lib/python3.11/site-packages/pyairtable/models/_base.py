from functools import partial
from typing import Any, ClassVar, Dict, Iterable, Mapping, Optional, Set, Type, Union

import inflection
from typing_extensions import Self as SelfType

from pyairtable._compat import pydantic
from pyairtable.utils import _append_docstring_text


class AirtableModel(pydantic.BaseModel):
    """
    Base model for any data structures that will be loaded from the Airtable API.
    """

    class Config:
        # Ignore field names we don't recognize, so applications don't crash
        # if Airtable decides to add new attributes.
        extra = "ignore"

        # Convert e.g. "base_invite_links" to "baseInviteLinks" for (de)serialization
        alias_generator = partial(inflection.camelize, uppercase_first_letter=False)

        # Allow both base_invite_links= and baseInviteLinks= in constructor
        allow_population_by_field_name = True

        # We'll assume this in a couple different places
        underscore_attrs_are_private = True

    _raw: Any = pydantic.PrivateAttr()

    def __init__(self, **data: Any) -> None:
        super().__init__(**data)
        self._raw = data

    @classmethod
    def from_api(
        cls,
        obj: Dict[str, Any],
        api: "pyairtable.api.api.Api",
        *,
        context: Optional[Any] = None,
    ) -> SelfType:
        """
        Construct an instance which is able to update itself using an
        :class:`~pyairtable.Api`.

        Args:
            obj: The JSON data structure used to construct the instance.
                 Will be passed to `parse_obj <https://docs.pydantic.dev/latest/usage/models/#helper-functions>`_.
            api: The connection to use for saving updates.
            context: An object, sequence of objects, or mapping of names to objects
                which will be used as arguments to ``str.format()`` when constructing
                the URL for a :class:`~pyairtable.models._base.RestfulModel`.
        """
        instance = cls(**obj)
        cascade_api(instance, api, context=context)
        return instance


def _context_name(obj: Any) -> str:
    return inflection.underscore(type(obj).__name__)


def cascade_api(
    obj: Any,
    api: "pyairtable.api.api.Api",
    *,
    context: Optional[Any] = None,
) -> None:
    """
    Ensure all nested objects have access to the given Api instance,
    and trigger them to configure their URLs accordingly.

    Args:
        api: The instance of the API to set.
        context: A mapping of class names to instances of that class.
    """
    if context is None:
        context = {}
    # context=Foo() is short for context={"foo": Foo()}
    if context and not isinstance(context, dict):
        context = {_context_name(context): context}

    # Ensure we don't get stuck in infinite loops
    visited: Set[int] = context.setdefault("__visited__", set())
    if id(obj) in visited:
        return
    visited.add(id(obj))

    # Iterate over containers and cascade API context down to contained models.
    if isinstance(obj, (list, tuple, set)):
        for value in obj:
            cascade_api(value, api, context=context)
    if isinstance(obj, dict):
        for key, value in obj.items():
            cascade_api(value, api, context={**context, "key": key})
    if not isinstance(obj, AirtableModel):
        return

    # If we get this far, we're dealing with a model, so add it to the context.
    # If it's a ModelNamedThis, the key will be model_named_this.
    context = {**context, _context_name(obj): obj}

    if isinstance(obj, RestfulModel):
        # This is what we came here for; set the API and URL on the RESTful model.
        obj._set_api(api, context=context)

    # Find and apply API/context to nested models in every Pydantic field.
    for field_name in type(obj).__fields__:
        if field_value := getattr(obj, field_name, None):
            cascade_api(field_value, api, context=context)


class RestfulModel(AirtableModel):
    """
    Base model for any data structures that wrap around a REST API endpoint.

    Subclasses can pass a number of keyword arguments to control serialization behavior:

        * ``url=``: format string for building the URL to be used when saving changes to this model.
    """

    __url_pattern: ClassVar[str] = ""

    _api: "pyairtable.api.api.Api" = pydantic.PrivateAttr()
    _url: str = pydantic.PrivateAttr(default="")
    _url_context: Any = None

    def __init_subclass__(cls, **kwargs: Any) -> None:
        cls.__url_pattern = kwargs.pop("url", cls.__url_pattern)
        super().__init_subclass__()

    def _set_api(self, api: "pyairtable.api.api.Api", context: Dict[str, Any]) -> None:
        """
        Set a link to the API and build the REST URL used for this resource.
        """
        self._api = api
        self._url_context = context
        try:
            self._url = self.__url_pattern.format(**context, self=self)
        except (KeyError, AttributeError) as exc:
            exc.args = (
                *exc.args,
                {k: v for (k, v) in context.items() if k != "__visited__"},
            )
            raise
        if self._url and not self._url.startswith("http"):
            self._url = api.build_url(self._url)

    def _reload(self, obj: Optional[Dict[str, Any]] = None) -> None:
        """
        Reload the model's contents from the given object, or by making a GET request to the API.
        """
        if obj is None:
            obj = self._api.get(self._url)
        copyable = type(self).from_api(obj, self._api, context=self._url_context)
        self.__dict__.update(
            {key: copyable.__dict__.get(key) for key in type(self).__fields__}
        )


class CanDeleteModel(RestfulModel):
    """
    Mix-in for RestfulModel that allows a model to be deleted.
    """

    _deleted: bool = pydantic.PrivateAttr(default=False)

    @property
    def deleted(self) -> bool:
        """
        Indicates whether the record has been deleted since being returned from the API.
        """
        return self._deleted

    def delete(self) -> None:
        """
        Delete the record on the server and mark this instance as deleted.
        """
        if not self._url:
            raise RuntimeError("delete() called with no URL specified")
        self._api.request("DELETE", self._url)
        self._deleted = True


class CanUpdateModel(RestfulModel):
    """
    Mix-in for RestfulModel that allows a model to be modified and saved.

    Subclasses can pass a number of keyword arguments to control serialization behavior:

        * ``writable=``: field names that should be written to API on ``save()``.
        * ``readonly=``: field names that should not be written to API on ``save()``.
        * ``save_null_values=``: boolean indicating whether ``save()`` should write nulls (default: true)
    """

    __writable: ClassVar[Optional[Iterable[str]]] = None
    __readonly: ClassVar[Optional[Iterable[str]]] = None
    __save_none: ClassVar[bool] = True
    __save_http_method: ClassVar[str] = "PATCH"
    __reload_after_save: ClassVar[bool] = True

    def __init_subclass__(cls, **kwargs: Any) -> None:
        if "writable" in kwargs and "readonly" in kwargs:
            raise ValueError("incompatible kwargs 'writable' and 'readonly'")
        cls.__writable = kwargs.pop("writable", cls.__writable)
        cls.__readonly = kwargs.pop("readonly", cls.__readonly)
        cls.__save_none = bool(kwargs.pop("save_null_values", cls.__save_none))
        cls.__save_http_method = kwargs.pop("save_method", cls.__save_http_method)
        cls.__reload_after_save = bool(
            kwargs.pop("reload_after_save", cls.__reload_after_save)
        )
        if cls.__writable:
            _append_docstring_text(
                cls,
                "The following fields can be modified and saved: "
                + ", ".join(f"``{field}``" for field in cls.__writable),
            )
        if cls.__readonly:
            _append_docstring_text(
                cls,
                "The following fields are read-only and cannot be modified:\n"
                + ", ".join(f"``{field}``" for field in cls.__readonly),
            )
        super().__init_subclass__(**kwargs)

    def save(self) -> None:
        """
        Save any changes made to the instance's writable fields and update the
        instance with any refreshed values returned from the API.

        Will raise ``RuntimeError`` if the record has been deleted.
        """
        if getattr(self, "_deleted", None):
            raise RuntimeError("save() called after delete()")
        if not self._url:
            raise RuntimeError("save() called with no URL specified")
        include = set(self.__writable) if self.__writable else None
        exclude = set(self.__readonly) if self.__readonly else None
        data = self.dict(
            by_alias=True,
            include=include,
            exclude=exclude,
            exclude_none=(not self.__save_none),
        )
        response = self._api.request(self.__save_http_method, self._url, json=data)
        if self.__reload_after_save:
            self._reload(response)

    def __setattr__(self, name: str, value: Any) -> None:
        # Prevents implementers from changing values on readonly or non-writable fields.
        # Mypy can't tell that we are using pydantic v1.
        if name in self.__class__.__fields__:  # type: ignore[operator, unused-ignore]
            if self.__readonly and name in self.__readonly:
                raise AttributeError(name)
            if self.__writable is not None and name not in self.__writable:
                raise AttributeError(name)

        super().__setattr__(name, value)


def update_forward_refs(
    obj: Union[Type[AirtableModel], Mapping[str, Any]],
    memo: Optional[Set[int]] = None,
) -> None:
    """
    Convenience method to ensure we update forward references for all nested models.

    Any time a type annotation refers to a nested class that isn't present
    at the time the attribute is created, we need to tell pydantic to
    update forward references after all the referenced models exist.

    Only intended for use within pyAirtable, like:

        >>> from pyairtable.models._base import AirtableModel, update_forward_refs
        >>> class A(AirtableModel): ...
        >>> class B(AirtableModel): ...
        ...     class B_One(AirtableModel): ...
        ...     class B_Two(AirtableModel): ...
        >>> update_forward_refs(vars())
    """
    memo = set() if memo is None else memo
    # If it's a type, update its refs, then do the same for any nested classes.
    # This will raise AttributeError if given a non-AirtableModel type.
    if isinstance(obj, type):
        if id(obj) in memo:
            return
        memo.add(id(obj))
        obj.update_forward_refs()
        return update_forward_refs(vars(obj), memo=memo)
    # If it's a mapping, update refs for any AirtableModel instances.
    for value in obj.values():
        if isinstance(value, type) and issubclass(value, AirtableModel):
            update_forward_refs(value, memo=memo)


import pyairtable.api.api  # noqa
