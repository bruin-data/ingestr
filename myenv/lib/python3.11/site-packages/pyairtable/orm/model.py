from functools import lru_cache
from typing import Any, Dict, Iterable, List, Optional

from typing_extensions import Self as SelfType

from pyairtable.api.api import Api
from pyairtable.api.base import Base
from pyairtable.api.table import Table
from pyairtable.api.types import (
    FieldName,
    RecordDict,
    RecordId,
    UpdateRecordDict,
    WritableFields,
)
from pyairtable.formulas import OR, STR_VALUE
from pyairtable.models import Comment
from pyairtable.orm.fields import AnyField, Field


class Model:
    """
    Supports creating ORM-style classes representing Airtable tables.
    For more details, see :ref:`orm`.

    A nested class called ``Meta`` is required and can specify
    the following attributes:

        * ``api_key`` (required) - API key or personal access token.
        * ``base_id`` (required) - Base ID (not name).
        * ``table_name`` (required) - Table ID or name.
        * ``timeout`` - A tuple indicating a connect and read timeout. Defaults to no timeout.
        * ``typecast`` - |kwarg_typecast| Defaults to ``True``.

    .. code-block:: python

        from pyairtable.orm import Model, fields

        class Contact(Model):
            first_name = fields.TextField("First Name")
            age = fields.IntegerField("Age")

            class Meta:
                base_id = "appaPqizdsNHDvlEm"
                table_name = "Contact"
                api_key = "keyapikey"
                timeout = (5, 5)
                typecast = True

    You can implement meta attributes as callables if certain values
    need to be dynamically provided or are unavailable at import time:

    .. code-block:: python

        from pyairtable.orm import Model, fields
        from your_app.config import get_secret

        class Contact(Model):
            first_name = fields.TextField("First Name")
            age = fields.IntegerField("Age")

            class Meta:
                base_id = "appaPqizdsNHDvlEm"
                table_name = "Contact"

                @staticmethod
                def api_key():
                    return get_secret("AIRTABLE_API_KEY")
    """

    id: str = ""
    created_time: str = ""
    _deleted: bool = False
    _fields: Dict[FieldName, Any]

    def __init_subclass__(cls, **kwargs: Any):
        cls._validate_class()
        super().__init_subclass__(**kwargs)

    def __repr__(self) -> str:
        if not self.id:
            return f"<unsaved {self.__class__.__name__}>"
        return f"<{self.__class__.__name__} id={self.id!r}>"

    @classmethod
    def _attribute_descriptor_map(cls) -> Dict[str, AnyField]:
        """
        Build a mapping of the model's attribute names to field descriptor instances.

        >>> class Test(Model):
        ...     first_name = TextField("First Name")
        ...     age = NumberField("Age")
        ...
        >>> Test._attribute_descriptor_map()
        >>> {
        ...     "field_name": <TextField field_name="First Name">,
        ...     "another_Field": <NumberField field_name="Age">,
        ... }
        """
        return {k: v for k, v in cls.__dict__.items() if isinstance(v, Field)}

    @classmethod
    def _field_name_descriptor_map(cls) -> Dict[FieldName, AnyField]:
        """
        Build a mapping of the model's field names to field descriptor instances.

        >>> class Test(Model):
        ...     first_name = TextField("First Name")
        ...     age = NumberField("Age")
        ...
        >>> Test._field_name_descriptor_map()
        >>> {
        ...     "First Name": <TextField field_name="First Name">,
        ...     "Age": <NumberField field_name="Age">,
        ... }
        """
        return {f.field_name: f for f in cls._attribute_descriptor_map().values()}

    def __init__(self, **fields: Any):
        """
        Construct a model instance with field values based on the given keyword args.

        >>> Contact(name="Alice", birthday=date(1980, 1, 1))
        <unsaved Contact>

        The keyword argument ``id=`` special-cased and sets the record ID, not a field value.

        >>> Contact(id="recWPqD9izdsNvlE", name="Bob")
        <Contact id='recWPqD9izdsNvlE'>
        """

        if "id" in fields:
            self.id = fields.pop("id")

        # Field values in internal (not API) representation
        self._fields = {}

        # Call __set__ on each field to set field values
        for key, value in fields.items():
            if key not in self._attribute_descriptor_map():
                raise AttributeError(key)
            setattr(self, key, value)

    @classmethod
    def _get_meta(
        cls, name: str, default: Any = None, required: bool = False, call: bool = True
    ) -> Any:
        """
        Retrieves the value of a Meta attribute.

        Args:
            default: The default value to return if the attribute is not set.
            required: Raise an exception if the attribute is not set.
            call: If the value is callable, call it before returning a result.
        """
        if not hasattr(cls, "Meta"):
            raise AttributeError(f"{cls.__name__}.Meta must be defined")
        if not hasattr(cls.Meta, name):
            if required:
                raise ValueError(f"{cls.__name__}.Meta.{name} must be defined")
            return default
        value = getattr(cls.Meta, name)
        if call and callable(value):
            value = value()
        if required and value is None:
            raise ValueError(f"{cls.__name__}.Meta.{name} cannot be None")
        return value

    @classmethod
    def _validate_class(cls) -> None:
        # Verify required Meta attributes were set (but don't call any callables)
        assert cls._get_meta("api_key", required=True, call=False)
        assert cls._get_meta("base_id", required=True, call=False)
        assert cls._get_meta("table_name", required=True, call=False)

        model_attributes = [a for a in cls.__dict__.keys() if not a.startswith("__")]
        overridden = set(model_attributes).intersection(Model.__dict__.keys())
        if overridden:
            raise ValueError(
                "Class {cls} fields clash with existing method: {name}".format(
                    cls=cls.__name__, name=overridden
                )
            )

    @classmethod
    @lru_cache
    def get_api(cls) -> Api:
        return Api(
            api_key=cls._get_meta("api_key", required=True),
            timeout=cls._get_meta("timeout"),
        )

    @classmethod
    def get_base(cls) -> Base:
        return cls.get_api().base(cls._get_meta("base_id", required=True))

    @classmethod
    def get_table(cls) -> Table:
        return cls.get_base().table(cls._get_meta("table_name", required=True))

    @classmethod
    def _typecast(cls) -> bool:
        return bool(cls._get_meta("typecast", default=True))

    def exists(self) -> bool:
        """
        Whether the instance has been saved to Airtable already.
        """
        return bool(self.id)

    def save(self) -> bool:
        """
        Save the model to the API.

        If the instance does not exist already, it will be created;
        otherwise, the existing record will be updated.

        Returns:
            ``True`` if a record was created, ``False`` if it was updated.
        """
        if self._deleted:
            raise RuntimeError(f"{self.id} was deleted")
        table = self.get_table()
        fields = self.to_record(only_writable=True)["fields"]

        if not self.id:
            record = table.create(fields, typecast=self._typecast())
            did_create = True
        else:
            record = table.update(self.id, fields, typecast=self._typecast())
            did_create = False

        self.id = record["id"]
        self.created_time = record["createdTime"]
        return did_create

    def delete(self) -> bool:
        """
        Delete the record.

        Raises:
            ValueError: if the record does not exist.
        """
        if not self.id:
            raise ValueError("cannot be deleted because it does not have id")
        table = self.get_table()
        result = table.delete(self.id)
        self._deleted = True
        # Is it even possible to get "deleted" False?
        return bool(result["deleted"])

    @classmethod
    def all(cls, **kwargs: Any) -> List[SelfType]:
        """
        Retrieve all records for this model. For all supported
        keyword arguments, see :meth:`Table.all <pyairtable.Table.all>`.
        """
        table = cls.get_table()
        return [cls.from_record(record) for record in table.all(**kwargs)]

    @classmethod
    def first(cls, **kwargs: Any) -> Optional[SelfType]:
        """
        Retrieve the first record for this model. For all supported
        keyword arguments, see :meth:`Table.first <pyairtable.Table.first>`.
        """
        table = cls.get_table()
        if record := table.first(**kwargs):
            return cls.from_record(record)
        return None

    def to_record(self, only_writable: bool = False) -> RecordDict:
        """
        Build a :class:`~pyairtable.api.types.RecordDict` to represent this instance.

        This method converts internal field values into values expected by Airtable.
        For example, a ``datetime`` value from :class:`~pyairtable.orm.fields.DatetimeField`
        is converted into an ISO 8601 string.

        Args:
            only_writable: If ``True``, the result will exclude any
                values which are associated with readonly fields.
        """
        map_ = self._field_name_descriptor_map()
        fields = {
            field: None if value is None else map_[field].to_record_value(value)
            for field, value in self._fields.items()
            if not (map_[field].readonly and only_writable)
        }
        return {"id": self.id, "createdTime": self.created_time, "fields": fields}

    @classmethod
    def from_record(cls, record: RecordDict) -> SelfType:
        """
        Create an instance from a record dict.
        """
        name_field_map = cls._field_name_descriptor_map()
        # Convert Column Names into model field names
        field_values = {
            # Use field's to_internal_value to cast into model fields
            field: name_field_map[field].to_internal_value(value)
            for (field, value) in record["fields"].items()
            # Silently proceed if Airtable returns fields we don't recognize
            if field in name_field_map and value is not None
        }
        # Since instance(**field_values) will perform validation and fail on
        # any readonly fields, instead we directly set instance._fields.
        instance = cls(id=record["id"])
        instance._fields = field_values
        instance.created_time = record["createdTime"]
        return instance

    @classmethod
    def from_id(
        cls,
        record_id: RecordId,
        fetch: bool = True,
    ) -> SelfType:
        """
        Create an instance from a record ID.

        Args:
            record_id: |arg_record_id|
            fetch: If ``True``, record will be fetched and field values will be
                updated. If ``False``, a new instance is created with the provided ID,
                but field values are unset.
        """
        instance = cls(id=record_id)
        if fetch:
            instance.fetch()
        return instance

    def fetch(self) -> None:
        """
        Fetch field values from the API and resets instance field values.
        """
        if not self.id:
            raise ValueError("cannot be fetched because instance does not have an id")

        record = self.get_table().get(self.id)
        unused = self.from_record(record)
        self._fields = unused._fields
        self.created_time = unused.created_time

    @classmethod
    def from_ids(
        cls,
        record_ids: Iterable[RecordId],
        fetch: bool = True,
    ) -> List[SelfType]:
        """
        Create a list of instances from record IDs. If any record IDs returned
        are invalid this will raise a KeyError, but only *after* retrieving all
        other valid records from the API.

        Args:
            record_ids: |arg_record_id|
            fetch: If ``True``, records will be fetched and field values will be
                updated. If ``False``, new instances are created with the provided IDs,
                but field values are unset.
        """
        record_ids = list(record_ids)
        if not fetch:
            return [cls.from_id(record_id, fetch=False) for record_id in record_ids]
        formula = OR(
            *[f"RECORD_ID()={STR_VALUE(record_id)}" for record_id in record_ids]
        )
        records = [
            cls.from_record(record) for record in cls.get_table().all(formula=formula)
        ]
        records_by_id = {record.id: record for record in records}
        # Ensure we return records in the same order, and raise KeyError if any are missing
        return [records_by_id[record_id] for record_id in record_ids]

    @classmethod
    def batch_save(cls, models: List[SelfType]) -> None:
        """
        Save a list of model instances to the Airtable API with as few
        network requests as possible. Can accept a mixture of new records
        (which have not been saved yet) and existing records that have IDs.
        """
        if not all(isinstance(model, cls) for model in models):
            raise TypeError(set(type(model) for model in models))

        create_models = [model for model in models if not model.id]
        update_models = [model for model in models if model.id]
        create_records: List[WritableFields] = [
            record["fields"]
            for model in create_models
            if (record := model.to_record(only_writable=True))
        ]
        update_records: List[UpdateRecordDict] = [
            {"id": record["id"], "fields": record["fields"]}
            for model in update_models
            if (record := model.to_record(only_writable=True))
        ]

        table = cls.get_table()
        table.batch_update(update_records, typecast=cls._typecast())
        created_records = table.batch_create(create_records, typecast=cls._typecast())
        for model, created_record in zip(create_models, created_records):
            model.id = created_record["id"]
            model.created_time = created_record["createdTime"]

    @classmethod
    def batch_delete(cls, models: List[SelfType]) -> None:
        """
        Delete a list of model instances from Airtable.

        Raises:
            ValueError: if the model has not been saved to Airtable.
        """
        if not all(model.id for model in models):
            raise ValueError("cannot delete an unsaved model")
        if not all(isinstance(model, cls) for model in models):
            raise TypeError(set(type(model) for model in models))
        cls.get_table().batch_delete([model.id for model in models])

    def comments(self) -> List[Comment]:
        """
        Return a list of comments on this record.
        See :meth:`Table.comments <pyairtable.Table.comments>`.
        """
        return self.get_table().comments(self.id)

    def add_comment(self, text: str) -> Comment:
        """
        Add a comment to this record.
        See :meth:`Table.add_comment <pyairtable.Table.add_comment>`.
        """
        return self.get_table().add_comment(self.id, text)
