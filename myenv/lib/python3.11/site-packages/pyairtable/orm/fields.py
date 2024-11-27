"""
Field are used to define the Airtable column type for your pyAirtable models.

Internally these are implemented as `descriptors <https://docs.python.org/3/howto/descriptor.html>`_,
which allows us to define methods and type annotations for getting and setting attribute values.

>>> from pyairtable.orm import Model, fields
>>> class Contact(Model):
...     class Meta:
...         ...
...     name = fields.TextField("Name")
...     is_registered = fields.CheckboxField("Registered")
...
>>> contact = Contact(name="George", is_registered=True)
>>> assert contact.name == "George"
>>> reveal_type(contact.name)  # -> str
>>> contact.to_record()
{
    "id": recS6qSLw0OCA6Xul",
    "createdTime": "2021-07-14T06:42:37.000Z",
    "fields": {
        "Name": "George",
        "Registered": True,
    }
}
"""

import abc
import importlib
from datetime import date, datetime, timedelta
from enum import Enum
from typing import (
    TYPE_CHECKING,
    Any,
    ClassVar,
    Generic,
    List,
    Literal,
    Optional,
    Tuple,
    Type,
    TypeVar,
    Union,
    cast,
    overload,
)

from typing_extensions import Self as SelfType
from typing_extensions import TypeAlias

from pyairtable import utils
from pyairtable.api.types import (
    AITextDict,
    AttachmentDict,
    BarcodeDict,
    ButtonDict,
    CollaboratorDict,
    RecordId,
)

if TYPE_CHECKING:
    from pyairtable.orm import Model  # noqa


_ClassInfo: TypeAlias = Union[type, Tuple["_ClassInfo", ...]]
T = TypeVar("T")
T_Linked = TypeVar("T_Linked", bound="Model")
T_API = TypeVar("T_API")  # type used to exchange values w/ Airtable API
T_ORM = TypeVar("T_ORM")  # type used to store values internally


class Field(Generic[T_API, T_ORM], metaclass=abc.ABCMeta):
    """
    A generic class for an Airtable field descriptor that will be
    included in an ORM model.

    Type-checked subclasses should provide two type parameters,
    ``T_API`` and ``T_ORM``, which indicate the type returned
    by the API and the type used to store values internally.

    Subclasses should also define ``valid_types`` as a type
    or tuple of types, which will be used to validate the type
    of field values being set via this descriptor.
    """

    #: Types that are allowed to be passed to this field.
    valid_types: ClassVar[_ClassInfo] = ()

    #: Whether to allow modification of the value in this field.
    readonly: bool = False

    # Contains a reference to the Model class (if possible)
    _model: Optional[Type["Model"]] = None

    # The name of the attribute on the Model class (if possible)
    _attribute_name: Optional[str] = None

    def __init__(
        self,
        field_name: str,
        validate_type: bool = True,
        readonly: Optional[bool] = None,
    ) -> None:
        """
        Args:
            field_name: The name of the field in Airtable.
            validate_type: Whether to raise a TypeError if anything attempts to write
                an object of an unsupported type as a field value. If ``False``, you
                may encounter unpredictable behavior from the Airtable API.
            readonly: If ``True``, any attempt to write a value to this field will
                raise an ``AttributeError``. Each field implements appropriate default
                values, but you may find it useful to mark fields as readonly if you
                know that the access token your code uses does not have permission
                to modify specific fields.
        """
        self.field_name = field_name
        self.validate_type = validate_type

        # Each class will define its own default, but implementers can override it.
        # Overriding this to be `readonly=False` is probably always wrong, though.
        if readonly is not None:
            self.readonly = readonly

    def __set_name__(self, owner: Any, name: str) -> None:
        """
        Called when an instance of Field is created within a class.
        """
        self._model = owner
        self._attribute_name = name

    @property
    def _description(self) -> str:
        """
        Describes the field for the purpose of logging an error message.
        Handles an edge case where a field is created directly onto a class
        that already exists; in those cases, __set_name__ is not called.
        """
        if self._model and self._attribute_name:
            return f"{self._model.__name__}.{self._attribute_name}"
        return f"{self.field_name!r} field"

    # __get__ and __set__ are called when accessing an instance of Field on an object.
    # Model.field should return the Field instance itself, whereas
    # obj.field should return the field's value from the Model instance obj.

    # Model.field will call __get__(instance=None, owner=Model)
    @overload
    def __get__(self, instance: None, owner: Type[Any]) -> SelfType: ...

    # obj.field will call __get__(instance=obj, owner=Model)
    @overload
    def __get__(self, instance: "Model", owner: Type[Any]) -> Optional[T_ORM]: ...

    def __get__(
        self, instance: Optional["Model"], owner: Type[Any]
    ) -> Union[SelfType, Optional[T_ORM]]:
        # allow calling Model.field to get the field object instead of a value
        if not instance:
            return self
        try:
            return cast(T_ORM, instance._fields[self.field_name])
        except (KeyError, AttributeError):
            return self._missing_value()

    def __set__(self, instance: "Model", value: Optional[T_ORM]) -> None:
        self._raise_if_readonly()
        if not hasattr(instance, "_fields"):
            instance._fields = {}
        if self.validate_type and value is not None:
            self.valid_or_raise(value)
        instance._fields[self.field_name] = value

    def __delete__(self, instance: "Model") -> None:
        raise AttributeError(f"cannot delete {self._description}")

    def _missing_value(self) -> Optional[T_ORM]:
        return None

    def to_record_value(self, value: Any) -> Any:
        """
        Calculate the value which should be persisted to the API.
        """
        return value

    def to_internal_value(self, value: Any) -> Any:
        """
        Convert a value from the API into the value's internal representation.
        """
        return value

    def valid_or_raise(self, value: Any) -> None:
        """
        Validate the type of the given value and raise TypeError if invalid.
        """
        if self.valid_types and not isinstance(value, self.valid_types):
            raise TypeError(
                f"{self.__class__.__name__} value must be {self.valid_types}; got {type(value)}"
            )

    def _raise_if_readonly(self) -> None:
        if self.readonly:
            raise AttributeError(f"{self._description} is read-only")

    def __repr__(self) -> str:
        args = [repr(self.field_name)]
        args += [f"{key}={val!r}" for (key, val) in self._repr_fields()]
        return self.__class__.__name__ + "(" + ", ".join(args) + ")"

    def _repr_fields(self) -> List[Tuple[str, Any]]:
        return [
            ("readonly", self.readonly),
            ("validate_type", self.validate_type),
        ]


#: A generic Field whose internal and API representations are the same type.
_BasicField: TypeAlias = Field[T, T]


#: An alias for any type of Field.
AnyField: TypeAlias = _BasicField[Any]


class TextField(_BasicField[str]):
    """
    Used for all Airtable text fields. Accepts ``str``.

    See `Single line text <https://airtable.com/developers/web/api/field-model#simpletext>`__
    and `Long text <https://airtable.com/developers/web/api/field-model#multilinetext>`__.
    """

    valid_types = str


class _NumericField(Generic[T], _BasicField[T]):
    """
    Base class for Number, Float, and Integer. Shares a common validation rule.
    """

    def valid_or_raise(self, value: Any) -> None:
        # Because `bool` is a subclass of `int`, we have to explicitly check for it here.
        if isinstance(value, bool):
            raise TypeError(
                f"{self.__class__.__name__} value must be {self.valid_types}; got {type(value)}"
            )
        return super().valid_or_raise(value)


class NumberField(_NumericField[Union[int, float]]):
    """
    Number field with unspecified precision. Accepts either ``int`` or ``float``.

    See `Number <https://airtable.com/developers/web/api/field-model#decimalorintegernumber>`__.
    """

    valid_types = (int, float)


# This cannot inherit from NumberField because valid_types would be more restrictive
# in the subclass than what is defined in the parent class.
class IntegerField(_NumericField[int]):
    """
    Number field with integer precision. Accepts only ``int`` values.

    See `Number <https://airtable.com/developers/web/api/field-model#decimalorintegernumber>`__.
    """

    valid_types = int


# This cannot inherit from NumberField because valid_types would be more restrictive
# in the subclass than what is defined in the parent class.
class FloatField(_NumericField[float]):
    """
    Number field with decimal precision. Accepts only ``float`` values.

    See `Number <https://airtable.com/developers/web/api/field-model#decimalorintegernumber>`__.
    """

    valid_types = float


class RatingField(IntegerField):
    """
    Accepts ``int`` values that are greater than zero.

    See `Rating <https://airtable.com/developers/web/api/field-model#rating>`__.
    """

    def valid_or_raise(self, value: int) -> None:
        super().valid_or_raise(value)
        if value < 1:
            raise ValueError("rating cannot be below 1")


class CheckboxField(_BasicField[bool]):
    """
    Returns ``False`` instead of ``None`` if the field is empty on the Airtable base.

    See `Checkbox <https://airtable.com/developers/web/api/field-model#checkbox>`__.
    """

    valid_types = bool

    def _missing_value(self) -> bool:
        return False


class DatetimeField(Field[str, datetime]):
    """
    DateTime field. Accepts only `datetime <https://docs.python.org/3/library/datetime.html#datetime-objects>`_ values.

    See `Date and time <https://airtable.com/developers/web/api/field-model#dateandtime>`__.
    """

    valid_types = datetime

    def to_record_value(self, value: datetime) -> str:
        """
        Convert a ``datetime`` into an ISO 8601 string, e.g. "2014-09-05T12:34:56.000Z".
        """
        return utils.datetime_to_iso_str(value)

    def to_internal_value(self, value: str) -> datetime:
        """
        Convert an ISO 8601 string, e.g. "2014-09-05T07:00:00.000Z" into a ``datetime``.
        """
        return utils.datetime_from_iso_str(value)


class DateField(Field[str, date]):
    """
    Date field. Accepts only `date <https://docs.python.org/3/library/datetime.html#date-objects>`_ values.

    See `Date <https://airtable.com/developers/web/api/field-model#dateonly>`__.
    """

    valid_types = date

    def to_record_value(self, value: date) -> str:
        """
        Convert a ``date`` into an ISO 8601 string, e.g. "2014-09-05".
        """
        return utils.date_to_iso_str(value)

    def to_internal_value(self, value: str) -> date:
        """
        Convert an ISO 8601 string, e.g. "2014-09-05" into a ``date``.
        """
        return utils.date_from_iso_str(value)


class DurationField(Field[int, timedelta]):
    """
    Duration field. Accepts only `timedelta <https://docs.python.org/3/library/datetime.html#timedelta-objects>`_ values.

    See `Duration <https://airtable.com/developers/web/api/field-model#durationnumber>`__.
    Airtable's API returns this as a number of seconds.
    """

    valid_types = timedelta

    def to_record_value(self, value: timedelta) -> float:
        """
        Convert a ``timedelta`` into a number of seconds.
        """
        return value.total_seconds()

    def to_internal_value(self, value: Union[int, float]) -> timedelta:
        """
        Convert a number of seconds into a ``timedelta``.
        """
        return timedelta(seconds=value)


class _DictField(Generic[T], _BasicField[T]):
    """
    Generic field type that stores a single dict. Not for use via API;
    should be subclassed by concrete field types (below).
    """

    valid_types = dict


class _ListField(Generic[T_API, T_ORM], Field[List[T_API], List[T_ORM]]):
    """
    Generic type for a field that stores a list of values. Can be used
    to refer to a lookup field that might return more than one value.
    Not for direct use; should be subclassed by concrete field types (below).
    """

    valid_types = list

    # List fields will always return a list, never ``None``, so we
    # have to overload the type annotations for __get__

    @overload
    def __get__(self, instance: None, owner: Type[Any]) -> SelfType: ...

    @overload
    def __get__(self, instance: "Model", owner: Type[Any]) -> List[T_ORM]: ...

    def __get__(
        self, instance: Optional["Model"], owner: Type[Any]
    ) -> Union[SelfType, List[T_ORM]]:
        if not instance:
            return self
        return self._get_list_value(instance)

    def _get_list_value(self, instance: "Model") -> List[T_ORM]:
        value = cast(List[T_ORM], instance._fields.get(self.field_name))
        # If Airtable returns no value, substitute an empty list.
        if value is None:
            value = []
            # For implementers to be able to modify this list in place
            # and persist it later when they call .save(), we need to
            # set this empty list as the field's value.
            if not self.readonly:
                instance._fields[self.field_name] = value
        return value

    def to_internal_value(self, value: Optional[List[T_ORM]]) -> List[T_ORM]:
        if value is None:
            value = []
        return value


class _ValidatingListField(Generic[T], _ListField[T, T]):
    contains_type: Type[T]

    def valid_or_raise(self, value: Any) -> None:
        super().valid_or_raise(value)
        for obj in value:
            if not isinstance(obj, self.contains_type):
                raise TypeError(f"expected {self.contains_type}; got {type(obj)}")


class _LinkFieldOptions(Enum):
    LinkSelf = object()


#: Sentinel option for the `model=` param to :class:`~LinkField`
LinkSelf = _LinkFieldOptions.LinkSelf


class LinkField(_ListField[RecordId, T_Linked]):
    """
    Represents a MultipleRecordLinks field. Returns and accepts lists of Models.

    Can also be used with a lookup field that pulls from a MultipleRecordLinks field,
    provided the field is created with ``readonly=True``.

    See `Link to another record <https://airtable.com/developers/web/api/field-model#foreignkey>`__.
    """

    _linked_model: Union[str, Literal[_LinkFieldOptions.LinkSelf], Type[T_Linked]]

    def __init__(
        self,
        field_name: str,
        model: Union[str, Literal[_LinkFieldOptions.LinkSelf], Type[T_Linked]],
        validate_type: bool = True,
        readonly: Optional[bool] = None,
        lazy: bool = False,
    ):
        """
        Args:
            field_name: Name of the Airtable field.
            model:
                Model class representing the linked table. There are a few options:

                1. You can provide a ``str`` that is the fully qualified module and class name.
                   For example, ``"your.module.Model"`` will import ``Model`` from ``your.module``.
                2. You can provide a ``str`` that is *just* the class name, and it will be imported
                   from the same module as the model class.
                3. You can provide the sentinel value :data:`~LinkSelf`, and the link field
                   will point to the same model where the link field is created.

            validate_type: Whether to raise a TypeError if attempting to write
                an object of an unsupported type as a field value. If ``False``, you
                may encounter unpredictable behavior from the Airtable API.
            readonly: If ``True``, any attempt to write a value to this field will
                raise an ``AttributeError``. This will not, however, prevent any
                modification of the list object returned by this field.
            lazy: If ``True``, this field will return empty objects with oly IDs;
                call :meth:`~pyairtable.orm.Model.fetch` to retrieve values.
        """
        from pyairtable.orm import Model  # noqa, avoid circular import

        if not (
            model is _LinkFieldOptions.LinkSelf
            or isinstance(model, str)
            or (isinstance(model, type) and issubclass(model, Model))
        ):
            raise TypeError(f"expected str, Model, or LinkSelf; got {type(model)}")

        super().__init__(field_name, validate_type=validate_type, readonly=readonly)
        self._linked_model = model
        self._lazy = lazy

    @property
    def linked_model(self) -> Type[T_Linked]:
        """
        Resolve a :class:`~pyairtable.orm.Model` class based on
        the ``model=`` constructor parameter to this field instance.
        """
        if isinstance(self._linked_model, str):
            modpath, _, clsname = self._linked_model.rpartition(".")
            if not modpath:
                if self._model is None:
                    raise RuntimeError(f"{self._description} not created on a Model")
                modpath = self._model.__module__
            mod = importlib.import_module(modpath)
            cls = getattr(mod, clsname)
            self._linked_model = cast(Type[T_Linked], cls)

        elif self._linked_model is _LinkFieldOptions.LinkSelf:
            if self._model is None:
                raise RuntimeError(f"{self._description} not created on a Model")
            self._linked_model = cast(Type[T_Linked], self._model)

        return self._linked_model

    def _repr_fields(self) -> List[Tuple[str, Any]]:
        return [
            ("model", self._linked_model),
            ("validate_type", self.validate_type),
            ("readonly", self.readonly),
            ("lazy", self._lazy),
        ]

    def _get_list_value(self, instance: "Model") -> List[T_Linked]:
        """
        Unlike most other field classes, LinkField does not store its internal
        representation (T_ORM) in instance._fields after Model.from_record().
        Instead, we defer creating objects until they're requested for the first
        time, so we can avoid infinite recursion during to_internal_value().
        """
        if not (records := super()._get_list_value(instance)):
            return records
        # If there are any values which are IDs rather than instances,
        # retrieve their values in bulk, and store them keyed by ID
        # so we can maintain the order we received from the API.
        new_records = {}
        if new_record_ids := [v for v in records if isinstance(v, RecordId)]:
            new_records = {
                record.id: record
                for record in self.linked_model.from_ids(
                    cast(List[RecordId], new_record_ids),
                    fetch=(not self._lazy),
                )
            }
        # If the list contains record IDs, replace the contents with instances.
        # Other code may already have references to this specific list, so
        # we replace the existing list's values.
        records[:] = [
            new_records[cast(RecordId, value)] if isinstance(value, RecordId) else value
            for value in records
        ]
        return records

    def to_record_value(self, value: Union[List[str], List[T_Linked]]) -> List[str]:
        """
        Build the list of record IDs which should be persisted to the API.
        """
        # If the _fields value contains str, it means we loaded it from the API
        # but we never actually accessed the value (see _get_list_value).
        # When persisting this model back to the API, we can just write those IDs.
        if all(isinstance(v, str) for v in value):
            return cast(List[str], value)
        # From here on, we assume we're dealing with models, not record IDs.
        records = cast(List[T_Linked], value)
        self.valid_or_raise(records)
        # We could *try* to recursively save models that don't have an ID yet,
        # but that requires us to second-guess the implementers' intentions.
        # Better to just raise an exception.
        if not all(record.exists() for record in records):
            raise ValueError(f"{self._description} contains an unsaved record")
        return [record.id for record in records]

    def valid_or_raise(self, value: Any) -> None:
        super().valid_or_raise(value)
        for obj in value:
            if not isinstance(obj, self.linked_model):
                raise TypeError(f"expected {self.linked_model}; got {type(obj)}")


# Many of these are "passthrough" subclasses for now. E.g. there is no real
# difference between `field = TextField()` and `field = PhoneNumberField()`.
#
# But we might choose to add more type-specific functionality later, so
# we'll allow implementers to get as specific as they care to and they might
# get some extra functionality for free in the future.


class AITextField(_DictField[AITextDict]):
    """
    Read-only field that returns a `dict`. For more information, read the
    `AI Text <https://airtable.com/developers/web/api/field-model#aitext>`_
    documentation.
    """

    readonly = True


class AttachmentsField(_ValidatingListField[AttachmentDict]):
    """
    Accepts a list of dicts in the format detailed in
    `Attachments <https://airtable.com/developers/web/api/field-model#multipleattachment>`_.
    """

    contains_type = cast(Type[AttachmentDict], dict)


class AutoNumberField(IntegerField):
    """
    Equivalent to :class:`IntegerField(readonly=True) <IntegerField>`.

    See `Auto number <https://airtable.com/developers/web/api/field-model#autonumber>`__.
    """

    readonly = True


class BarcodeField(_DictField[BarcodeDict]):
    """
    Accepts a `dict` that should conform to the format detailed in the
    `Barcode <https://airtable.com/developers/web/api/field-model#barcode>`_
    documentation.
    """


class ButtonField(_DictField[ButtonDict]):
    """
    Read-only field that returns a `dict`. For more information, read the
    `Button <https://airtable.com/developers/web/api/field-model#button>`_
    documentation.
    """

    readonly = True


class CollaboratorField(_DictField[CollaboratorDict]):
    """
    Accepts a `dict` that should conform to the format detailed in the
    `Collaborator <https://airtable.com/developers/web/api/field-model#collaborator>`_
    documentation.
    """


class CountField(IntegerField):
    """
    Equivalent to :class:`IntegerField(readonly=True) <IntegerField>`.

    See `Count <https://airtable.com/developers/web/api/field-model#count>`__.
    """

    readonly = True


class CreatedByField(CollaboratorField):
    """
    Equivalent to :class:`CollaboratorField(readonly=True) <CollaboratorField>`.

    See `Created by <https://airtable.com/developers/web/api/field-model#createdby>`__.
    """

    readonly = True


class CreatedTimeField(DatetimeField):
    """
    Equivalent to :class:`DatetimeField(readonly=True) <DatetimeField>`.

    See `Created time <https://airtable.com/developers/web/api/field-model#createdtime>`__.
    """

    readonly = True


class CurrencyField(NumberField):
    """
    Equivalent to :class:`~NumberField`.

    See `Currency <https://airtable.com/developers/web/api/field-model#currencynumber>`__.
    """


class EmailField(TextField):
    """
    Equivalent to :class:`~TextField`.

    See `Email <https://airtable.com/developers/web/api/field-model#email>`__.
    """


class ExternalSyncSourceField(TextField):
    """
    Equivalent to :class:`TextField(readonly=True) <TextField>`.

    See `Sync source <https://airtable.com/developers/web/api/field-model#syncsource>`__.
    """

    readonly = True


class LastModifiedByField(CollaboratorField):
    """
    Equivalent to :class:`CollaboratorField(readonly=True) <CollaboratorField>`.

    See `Last modified by <https://airtable.com/developers/web/api/field-model#lastmodifiedby>`__.
    """

    readonly = True


class LastModifiedTimeField(DatetimeField):
    """
    Equivalent to :class:`DatetimeField(readonly=True) <DatetimeField>`.

    See `Last modified time <https://airtable.com/developers/web/api/field-model#lastmodifiedtime>`__.
    """

    readonly = True


class LookupField(Generic[T], _ListField[T, T]):
    """
    Generic field class for a lookup, which returns a list of values.

    pyAirtable does not inspect field configuration at runtime or during type checking.
    If you use mypy, you can declare which type(s) the lookup returns:

    >>> from pyairtable.orm import fields as F
    >>> class MyTable(Model):
    ...     Meta = fake_meta()
    ...     lookup = F.LookupField[str]("My Lookup")
    ...
    >>> rec = MyTable.first()
    >>> rec.lookup
    ["First value", "Second value", ...]

    See `Lookup <https://airtable.com/developers/web/api/field-model#lookup>`__.
    """

    readonly = True


class MultipleCollaboratorsField(_ValidatingListField[CollaboratorDict]):
    """
    Accepts a list of dicts in the format detailed in
    `Multiple Collaborators <https://airtable.com/developers/web/api/field-model#multicollaborator>`_.
    """

    contains_type = cast(Type[CollaboratorDict], dict)


class MultipleSelectField(_ValidatingListField[str]):
    """
    Accepts a list of ``str``.

    See `Multiple select <https://airtable.com/developers/web/api/field-model#multiselect>`__.
    """

    contains_type = str


class PercentField(NumberField):
    """
    Equivalent to :class:`~NumberField`.

    See `Percent <https://airtable.com/developers/web/api/field-model#percentnumber>`__.
    """


class PhoneNumberField(TextField):
    """
    Equivalent to :class:`~TextField`.

    See `Phone <https://airtable.com/developers/web/api/field-model#phone>`__.
    """


class RichTextField(TextField):
    """
    Equivalent to :class:`~TextField`.

    See `Rich text <https://airtable.com/developers/web/api/field-model#rich-text>`__.
    """


class SelectField(TextField):
    """
    Equivalent to :class:`~TextField`.

    See `Single select <https://airtable.com/developers/web/api/field-model#select>`__.
    """


class UrlField(TextField):
    """
    Equivalent to :class:`~TextField`.

    See `Url <https://airtable.com/developers/web/api/field-model#urltext>`__.
    """


#: Set of all Field subclasses exposed by the library.
#:
#: :meta hide-value:
ALL_FIELDS = {
    field_class
    for name, field_class in vars().items()
    if isinstance(field_class, type)
    and issubclass(field_class, Field)
    and field_class is not Field
    and not name.startswith("_")
}


#: Set of all read-only Field subclasses exposed by the library.
#:
#: :meta hide-value:
READONLY_FIELDS = {cls for cls in ALL_FIELDS if cls.readonly}


#: Mapping of Airtable field type names to their ORM classes.
#: See https://airtable.com/developers/web/api/field-model
#: and :ref:`Formula, Rollup, and Lookup Fields`.
#:
#: The data type of "formula" and "rollup" fields will depend
#: on the underlying fields they reference, so it is not practical
#: for the ORM to know or detect those fields' types. These two
#: field type names are mapped to the constant ``NotImplemented``.
#:
#: :meta hide-value:
FIELD_TYPES_TO_CLASSES = {
    "aiText": AITextField,
    "autoNumber": AutoNumberField,
    "barcode": BarcodeField,
    "button": ButtonField,
    "checkbox": CheckboxField,
    "count": CountField,
    "createdBy": CreatedByField,
    "createdTime": CreatedTimeField,
    "currency": CurrencyField,
    "date": DateField,
    "dateTime": DatetimeField,
    "duration": DurationField,
    "email": EmailField,
    "externalSyncSource": ExternalSyncSourceField,
    "formula": NotImplemented,
    "lastModifiedBy": LastModifiedByField,
    "lastModifiedTime": LastModifiedTimeField,
    "lookup": LookupField,
    "multilineText": TextField,
    "multipleAttachments": AttachmentsField,
    "multipleCollaborators": MultipleCollaboratorsField,
    "multipleRecordLinks": LinkField,
    "multipleSelects": MultipleSelectField,
    "number": NumberField,
    "percent": PercentField,
    "phoneNumber": PhoneNumberField,
    "rating": RatingField,
    "richText": RichTextField,
    "rollup": NotImplemented,
    "singleCollaborator": CollaboratorField,
    "singleLineText": TextField,
    "singleSelect": SelectField,
    "url": UrlField,
}


#: Mapping of field classes to the set of supported Airtable field types.
#:
#: :meta hide-value:
FIELD_CLASSES_TO_TYPES = {
    cls: {key for (key, val) in FIELD_TYPES_TO_CLASSES.items() if val == cls}
    for cls in ALL_FIELDS
}


# Auto-generate __all__ to explicitly exclude any imported values
#
# [[[cog]]]
# import re
#
# with open(cog.inFile) as fp:
#     src = fp.read()
#
# classes = re.findall(r"class ((?:[A-Z]\w+)?Field)", src)
# constants = re.findall(r"^(?!T_)([A-Z][A-Z_]+) = ", src, re.MULTILINE)
# extras = ["LinkSelf"]
# names = sorted(classes) + constants + extras
#
# cog.outl("\n\n__all__ = [")
# for name in ["Field", *names]:
#     cog.outl(f'    "{name}",')
# cog.outl("]")
# [[[out]]]


__all__ = [
    "Field",
    "AITextField",
    "AttachmentsField",
    "AutoNumberField",
    "BarcodeField",
    "ButtonField",
    "CheckboxField",
    "CollaboratorField",
    "CountField",
    "CreatedByField",
    "CreatedTimeField",
    "CurrencyField",
    "DateField",
    "DatetimeField",
    "DurationField",
    "EmailField",
    "ExternalSyncSourceField",
    "Field",
    "FloatField",
    "IntegerField",
    "LastModifiedByField",
    "LastModifiedTimeField",
    "LinkField",
    "LookupField",
    "MultipleCollaboratorsField",
    "MultipleSelectField",
    "NumberField",
    "PercentField",
    "PhoneNumberField",
    "RatingField",
    "RichTextField",
    "SelectField",
    "TextField",
    "UrlField",
    "ALL_FIELDS",
    "READONLY_FIELDS",
    "FIELD_TYPES_TO_CLASSES",
    "FIELD_CLASSES_TO_TYPES",
    "LinkSelf",
]
# [[[end]]] (checksum: 2aa36f4e76db73f3d0b741b6be6c9e9e)
