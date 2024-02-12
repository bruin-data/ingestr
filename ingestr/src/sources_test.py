import unittest

import dlt
import pytest

from ingestr.src.sources import SqlSource


class SqlSourceTest(unittest.TestCase):
    def test_sql_source_requires_two_fields_in_table(self):
        source = SqlSource()
        with pytest.raises(ValueError):
            uri = "bigquery://my-project"
            source.dlt_source(uri, "onetable")

        with pytest.raises(ValueError):
            uri = "bigquery://my-project"
            source.dlt_source(uri, "onetable.with.too.many.fields")

    def test_table_instance_is_created(self):
        uri = "bigquery://my-project"
        table = "schema.table"

        # monkey patch the sql_table function
        def sql_table(credentials, schema, table, incremental):
            self.assertEqual(credentials, uri)
            self.assertEqual(schema, "schema")
            self.assertEqual(table, "table")
            self.assertIsNone(incremental)
            return dlt.resource()

        source = SqlSource(table_builder=sql_table)
        res = source.dlt_source(uri, table)
        self.assertIsNotNone(res)

    def test_table_instance_is_created_with_incremental(self):
        uri = "bigquery://my-project"
        table = "schema.table"
        incremental_key = "id"

        # monkey patch the sql_table function
        def sql_table(credentials, schema, table, incremental):
            self.assertEqual(credentials, uri)
            self.assertEqual(schema, "schema")
            self.assertEqual(table, "table")
            self.assertIsInstance(incremental, dlt.sources.incremental)
            self.assertEqual(incremental.cursor_path, incremental_key)
            return dlt.resource()

        source = SqlSource(table_builder=sql_table)
        res = source.dlt_source(uri, table, incremental_key=incremental_key)
        self.assertIsNotNone(res)
