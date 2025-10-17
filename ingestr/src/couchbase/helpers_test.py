"""Tests for Couchbase helpers module"""

import pytest
from datetime import datetime, timedelta
from unittest.mock import Mock, MagicMock, patch

from ingestr.src.couchbase.helpers import (
    CouchbaseLoader,
    CouchbaseKVLoader,
    convert_couchbase_objs,
    parse_couchbase_uri,
    cluster_from_credentials,
)


class TestParseCouchbaseUri:
    """Tests for parse_couchbase_uri function"""

    def test_parse_basic_uri(self):
        uri = "couchbase://user:pass@localhost:11210/mybucket?scope=myscope&collection=mycoll"
        conn_str, username, password, bucket, scope, collection = parse_couchbase_uri(uri)

        assert conn_str == "couchbase://localhost:11210"
        assert username == "user"
        assert password == "pass"
        assert bucket == "mybucket"
        assert scope == "myscope"
        assert collection == "mycoll"

    def test_parse_secure_uri(self):
        uri = "couchbases://admin:secret@cluster.example.com:11207/testbucket"
        conn_str, username, password, bucket, scope, collection = parse_couchbase_uri(uri)

        assert conn_str == "couchbases://cluster.example.com:11207"
        assert username == "admin"
        assert password == "secret"
        assert bucket == "testbucket"
        assert scope is None
        assert collection is None

    def test_parse_uri_with_default_scope(self):
        uri = "couchbase://user:pass@localhost/mybucket?collection=mycoll"
        conn_str, username, password, bucket, scope, collection = parse_couchbase_uri(uri)

        assert bucket == "mybucket"
        assert scope is None
        assert collection == "mycoll"

    def test_parse_uri_without_credentials(self):
        uri = "couchbase://localhost:11210/mybucket"
        conn_str, username, password, bucket, scope, collection = parse_couchbase_uri(uri)

        assert username == ""
        assert password == ""
        assert bucket == "mybucket"


class TestConvertCouchbaseObjs:
    """Tests for convert_couchbase_objs function"""

    def test_convert_datetime(self):
        dt = datetime(2023, 1, 15, 10, 30, 0)
        result = convert_couchbase_objs(dt)
        # Should be converted to pendulum datetime
        assert result is not None

    def test_convert_timedelta(self):
        td = timedelta(hours=2, minutes=30)
        result = convert_couchbase_objs(td)
        assert result == 9000.0  # 2.5 hours in seconds

    def test_convert_regular_value(self):
        result = convert_couchbase_objs("test_string")
        assert result == "test_string"

        result = convert_couchbase_objs(123)
        assert result == 123

        result = convert_couchbase_objs({"key": "value"})
        assert result == {"key": "value"}


class TestCouchbaseLoader:
    """Tests for CouchbaseLoader class"""

    @pytest.fixture
    def mock_cluster(self):
        cluster = Mock()
        bucket = Mock()
        scope = Mock()
        collection = Mock()

        cluster.bucket.return_value = bucket
        bucket.scope.return_value = scope
        scope.collection.return_value = collection

        return cluster

    def test_loader_initialization(self, mock_cluster):
        loader = CouchbaseLoader(
            cluster=mock_cluster,
            bucket_name="test_bucket",
            scope_name="test_scope",
            collection_name="test_collection",
            chunk_size=1000,
        )

        assert loader.bucket_name == "test_bucket"
        assert loader.scope_name == "test_scope"
        assert loader.collection_name == "test_collection"
        assert loader.chunk_size == 1000
        assert loader.cursor_field is None
        assert loader.last_value is None

    def test_build_where_clause_no_filter(self, mock_cluster):
        loader = CouchbaseLoader(
            cluster=mock_cluster,
            bucket_name="test_bucket",
            scope_name="test_scope",
            collection_name="test_collection",
            chunk_size=1000,
        )

        where_clause, params = loader._build_where_clause()
        assert where_clause == ""
        assert params == {}

    def test_build_where_clause_with_filter(self, mock_cluster):
        loader = CouchbaseLoader(
            cluster=mock_cluster,
            bucket_name="test_bucket",
            scope_name="test_scope",
            collection_name="test_collection",
            chunk_size=1000,
        )

        filter_ = {"status": "active", "type": "user"}
        where_clause, params = loader._build_where_clause(filter_)

        assert "`status` = $filter_0" in where_clause
        assert "`type` = $filter_1" in where_clause
        assert params["filter_0"] == "active"
        assert params["filter_1"] == "user"

    def test_build_order_clause_no_incremental(self, mock_cluster):
        loader = CouchbaseLoader(
            cluster=mock_cluster,
            bucket_name="test_bucket",
            scope_name="test_scope",
            collection_name="test_collection",
            chunk_size=1000,
        )

        order_clause = loader._build_order_clause()
        assert order_clause == ""

    @patch("ingestr.src.couchbase.helpers.dlt")
    def test_build_order_clause_with_incremental(self, mock_dlt, mock_cluster):
        mock_incremental = Mock()
        mock_incremental.cursor_path = "updated_at"
        mock_incremental.last_value = "2023-01-01"
        mock_incremental.row_order = "asc"
        mock_incremental.last_value_func = max

        loader = CouchbaseLoader(
            cluster=mock_cluster,
            bucket_name="test_bucket",
            scope_name="test_scope",
            collection_name="test_collection",
            chunk_size=1000,
            incremental=mock_incremental,
        )

        order_clause = loader._build_order_clause()
        assert "ORDER BY `updated_at` ASC" in order_clause


class TestCouchbaseKVLoader:
    """Tests for CouchbaseKVLoader class"""

    @pytest.fixture
    def mock_cluster(self):
        cluster = Mock()
        bucket = Mock()
        scope = Mock()
        collection = Mock()

        cluster.bucket.return_value = bucket
        bucket.scope.return_value = scope
        scope.collection.return_value = collection

        return cluster

    def test_kv_loader_initialization(self, mock_cluster):
        loader = CouchbaseKVLoader(
            cluster=mock_cluster,
            bucket_name="test_bucket",
            scope_name="test_scope",
            collection_name="test_collection",
            chunk_size=100,
        )

        assert loader.bucket_name == "test_bucket"
        assert loader.scope_name == "test_scope"
        assert loader.collection_name == "test_collection"
        assert loader.chunk_size == 100

    def test_load_by_keys_basic(self, mock_cluster):
        # Mock document results
        mock_result1 = Mock()
        mock_result1.content_as = {dict: {"name": "doc1", "value": 100}}

        mock_result2 = Mock()
        mock_result2.content_as = {dict: {"name": "doc2", "value": 200}}

        mock_cluster.bucket().scope().collection().get.side_effect = [
            mock_result1,
            mock_result2,
        ]

        loader = CouchbaseKVLoader(
            cluster=mock_cluster,
            bucket_name="test_bucket",
            scope_name="test_scope",
            collection_name="test_collection",
            chunk_size=10,
        )

        keys = ["key1", "key2"]
        results = list(loader.load_by_keys(keys))

        assert len(results) > 0

    def test_load_by_keys_with_projection(self, mock_cluster):
        mock_result = Mock()
        mock_result.content_as = {dict: {"name": "doc1", "value": 100, "extra": "data"}}

        mock_cluster.bucket().scope().collection().get.return_value = mock_result

        loader = CouchbaseKVLoader(
            cluster=mock_cluster,
            bucket_name="test_bucket",
            scope_name="test_scope",
            collection_name="test_collection",
            chunk_size=10,
        )

        keys = ["key1"]
        projection = ["name", "value"]

        results = list(loader.load_by_keys(keys, projection=projection))
        assert len(results) > 0


class TestClusterFromCredentials:
    """Tests for cluster_from_credentials function"""

    @patch("ingestr.src.couchbase.helpers.Cluster")
    @patch("ingestr.src.couchbase.helpers.PasswordAuthenticator")
    @patch("ingestr.src.couchbase.helpers.ClusterOptions")
    def test_create_cluster_basic(self, mock_options, mock_auth, mock_cluster_class):
        mock_cluster = Mock()
        mock_cluster_class.return_value = mock_cluster

        cluster = cluster_from_credentials(
            connection_string="couchbase://localhost",
            username="admin",
            password="password",
        )

        mock_auth.assert_called_once_with("admin", "password")
        assert cluster == mock_cluster
        mock_cluster.wait_until_ready.assert_called_once()

    @patch("ingestr.src.couchbase.helpers.Cluster")
    @patch("ingestr.src.couchbase.helpers.PasswordAuthenticator")
    @patch("ingestr.src.couchbase.helpers.ClusterOptions")
    def test_create_cluster_with_options(self, mock_options, mock_auth, mock_cluster_class):
        mock_cluster = Mock()
        mock_cluster_class.return_value = mock_cluster

        cluster = cluster_from_credentials(
            connection_string="couchbase://localhost",
            username="admin",
            password="password",
            timeout_option=10,
        )

        assert cluster == mock_cluster
        mock_cluster.wait_until_ready.assert_called_once()
