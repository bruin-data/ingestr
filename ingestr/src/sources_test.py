import unittest

import dlt
import pendulum
import pytest
from dlt.sources.credentials import ConnectionStringCredentials

from ingestr.src import sources
from ingestr.src.sources import AdjustSource, MongoDbSource, SqlSource


class SqlSourceTest(unittest.TestCase):
    def test_sql_source_requires_two_fields_in_table(self):
        source = SqlSource()
        with pytest.raises(ValueError):
            uri = "bigquery://my-project"
            source.dlt_source(uri, "onetable")

    def test_table_instance_is_created(self):
        uri = "bigquery://my-project"
        table = "schema.table"

        # monkey patch the sql_table function
        def sql_table(
            credentials: ConnectionStringCredentials,
            schema,
            table,
            incremental,
            backend,
            chunk_size,
            **kwargs,
        ):
            self.assertEqual(str(credentials.to_url()), uri)
            self.assertEqual(schema, "schema")
            self.assertEqual(table, "table")
            self.assertEqual(backend, "sqlalchemy")
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
        def sql_table(
            credentials: ConnectionStringCredentials,
            schema,
            table,
            incremental,
            backend,
            chunk_size,
            **kwargs,
        ):
            self.assertEqual(str(credentials.to_url()), uri)
            self.assertEqual(schema, "schema")
            self.assertEqual(table, "table")
            self.assertEqual(backend, "sqlalchemy")
            self.assertIsInstance(incremental, dlt.sources.incremental)
            self.assertEqual(incremental.cursor_path, incremental_key)
            return dlt.resource()

        source = SqlSource(table_builder=sql_table)
        res = source.dlt_source(uri, table, incremental_key=incremental_key)
        self.assertIsNotNone(res)


class MongoDbSourceTest(unittest.TestCase):
    def test_sql_source_requires_two_fields_in_table(self):
        source = MongoDbSource()
        with pytest.raises(ValueError):
            uri = "mongodb://my-project"
            source.dlt_source(uri, "onetable")

    def test_table_instance_is_created(self):
        uri = "mongodb://my-project"
        table = "schema.table"

        # monkey patch the mongo function
        def mongo(connection_url, database, collection, incremental, parallel):
            self.assertEqual(connection_url, uri)
            self.assertEqual(database, "schema")
            self.assertEqual(collection, "table")
            self.assertIsNone(incremental)
            return dlt.resource()

        source = MongoDbSource(table_builder=mongo)
        res = source.dlt_source(uri, table)
        self.assertIsNotNone(res)

    def test_table_instance_is_created_with_incremental(self):
        uri = "mongodb://my-project"
        table = "schema.table"
        incremental_key = "id"

        # monkey patch the mongo function
        def mongo(connection_url, database, collection, incremental, parallel):
            self.assertEqual(connection_url, uri)
            self.assertEqual(database, "schema")
            self.assertEqual(collection, "table")
            self.assertIsInstance(incremental, dlt.sources.incremental)
            self.assertEqual(incremental.cursor_path, incremental_key)
            return dlt.resource()

        source = MongoDbSource(table_builder=mongo)
        res = source.dlt_source(uri, table, incremental_key=incremental_key)
        self.assertIsNotNone(res)


class AdjustSourceTest(unittest.TestCase):
    def test_table_instance_is_created(self):
        uri = "adjust://?api_key=my-api-key"
        table = "creatives"

        # monkey patch the adjust function
        @dlt.source(max_table_nesting=0)
        def adjust(
            start_date, end_date, api_key, dimensions, metrics, merge_key, filters
        ):
            self.assertEqual(api_key, "my-api-key")
            self.assertIsNone(dimensions)
            self.assertIsNone(metrics)
            self.assertIsNone(merge_key)
            self.assertEqual(filters, [])

            # ensure the lookback days is 30
            self.assertEqual(
                start_date,
                pendulum.datetime(2024, 10, 6).replace(
                    hour=0, minute=0, second=0, microsecond=0
                ),
            )
            self.assertEqual(end_date, pendulum.datetime(2024, 11, 12))

            def creatives():
                return dlt.resource()

            return creatives

        sources.adjust_source = adjust
        source = AdjustSource()
        res = source.dlt_source(
            uri, table, interval_start="2024-11-05", interval_end="2024-11-12"
        )
        self.assertIsNotNone(res)

    def test_custom_table_with_dimensions_and_metrics(self):
        uri = "adjust://?api_key=my-api-key"
        table = "custom:hour,day:impressions,cost"

        # monkey patch the adjust function
        @dlt.source(max_table_nesting=0)
        def adjust(
            start_date, end_date, api_key, dimensions, metrics, merge_key, filters
        ):
            self.assertEqual(api_key, "my-api-key")
            self.assertEqual(dimensions, ["hour", "day"])
            self.assertEqual(metrics, ["impressions", "cost"])
            self.assertIsNone(merge_key)
            self.assertEqual(filters, [])

            def custom():
                return dlt.resource()

            return custom

        sources.adjust_source = adjust
        source = AdjustSource()
        res = source.dlt_source(uri, table)
        self.assertIsNotNone(res)

    def test_custom_table_with_dimensions_and_metrics_and_filters(self):
        uri = "adjust://?api_key=my-api-key"
        table = "custom:hour,day:impressions,cost:campaign=campaign1,campaign2,key1=value1,key2=value2"

        # monkey patch the adjust function
        @dlt.source(max_table_nesting=0)
        def adjust(
            start_date, end_date, api_key, dimensions, metrics, merge_key, filters
        ):
            self.assertEqual(api_key, "my-api-key")
            self.assertEqual(dimensions, ["hour", "day"])
            self.assertEqual(metrics, ["impressions", "cost"])
            self.assertIsNone(merge_key)
            self.assertEqual(
                filters,
                {
                    "campaign": ["campaign1", "campaign2"],
                    "key1": "value1",
                    "key2": "value2",
                },
            )

            def custom():
                return dlt.resource()

            return custom

        sources.adjust_source = adjust
        source = AdjustSource()
        res = source.dlt_source(uri, table)
        self.assertIsNotNone(res)
