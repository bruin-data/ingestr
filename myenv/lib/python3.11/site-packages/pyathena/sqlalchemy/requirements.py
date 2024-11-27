# -*- coding: utf-8 -*-
from sqlalchemy.testing import exclusions
from sqlalchemy.testing.requirements import SuiteRequirements

supported = exclusions.open
unsupported = exclusions.closed


class Requirements(SuiteRequirements):
    @property
    def array_type(self):
        return unsupported()

    @property
    def uuid_data_type(self):
        return unsupported()

    @property
    def foreign_keys(self):
        return unsupported()

    @property
    def on_update_cascade(self):
        return unsupported()

    @property
    def self_referential_foreign_keys(self):
        return unsupported()

    @property
    def foreign_key_ddl(self):
        return unsupported()

    @property
    def autoincrement_insert(self):
        return unsupported()

    @property
    def primary_key_constraint_reflection(self):
        return unsupported()

    @property
    def foreign_key_constraint_reflection(self):
        return unsupported()

    @property
    def temp_table_reflection(self):
        return unsupported()

    @property
    def temporary_tables(self):
        return unsupported()

    @property
    def index_reflection(self):
        return unsupported()

    @property
    def indexes_with_ascdesc(self):
        return unsupported()

    @property
    def reflect_indexes_with_ascdesc(self):
        return unsupported()

    @property
    def unique_constraint_reflection(self):
        return unsupported()

    @property
    def duplicate_key_raises_integrity_error(self):
        return unsupported()

    @property
    def update_where_target_in_subquery(self):
        return unsupported()

    @property
    def recursive_fk_cascade(self):
        return unsupported()

    @property
    def datetime_literals(self):
        return unsupported()

    @property
    def timestamp_microseconds(self):
        return unsupported()

    @property
    def precision_generic_float_type(self):
        # TODO: AssertionError:
        #  {Decimal('15.7563820'), Decimal('15.7563830')} != {Decimal('15.7563827')}
        return unsupported()

    @property
    def precision_numerics_many_significant_digits(self):
        return supported()

    @property
    def window_functions(self):
        return supported()

    @property
    def ctes(self):
        return supported()

    @property
    def views(self):
        return supported()

    @property
    def schemas(self):
        return supported()

    @property
    def implicit_default_schema(self):
        return supported()

    @property
    def datetime_historic(self):
        return supported()

    @property
    def date_historic(self):
        return supported()

    @property
    def precision_numerics_enotation_small(self):
        return supported()

    @property
    def order_by_label_with_expression(self):
        return supported()
