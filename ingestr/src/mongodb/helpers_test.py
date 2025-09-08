"""
Unit tests for MongoDB shell syntax to Extended JSON conversion
"""

import json
import unittest
from datetime import datetime

from bson import json_util

from ingestr.src.mongodb.helpers import convert_mongo_shell_to_extended_json


class TestMongoShellToExtendedJSON(unittest.TestCase):
    """Test cases for MongoDB shell syntax to Extended JSON conversion"""

    def test_isodate_conversion(self):
        """Test ISODate conversion"""
        # Basic ISODate with Z timezone
        input_str = 'ISODate("2010-01-01T00:00:00.000Z")'
        expected = '{"$date": "2010-01-01T00:00:00.000Z"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # ISODate with timezone offset
        input_str = 'ISODate("2010-01-01T00:00:00.000+0000")'
        expected = '{"$date": "2010-01-01T00:00:00.000+0000"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Multiple ISODates in a query
        input_str = '{"$gte":ISODate("2010-01-01T00:00:00.000Z"),"$lte":ISODate("2015-12-31T23:59:59.000Z")}'
        expected = '{"$gte":{"$date": "2010-01-01T00:00:00.000Z"},"$lte":{"$date": "2015-12-31T23:59:59.000Z"}}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # ISODate in an array
        input_str = (
            '[ISODate("2020-01-01T00:00:00.000Z"), ISODate("2021-01-01T00:00:00.000Z")]'
        )
        expected = '[{"$date": "2020-01-01T00:00:00.000Z"}, {"$date": "2021-01-01T00:00:00.000Z"}]'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

    def test_objectid_conversion(self):
        """Test ObjectId conversion"""
        # Basic ObjectId
        input_str = 'ObjectId("507f1f77bcf86cd799439011")'
        expected = '{"$oid": "507f1f77bcf86cd799439011"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # ObjectId in a query
        input_str = '{"_id": ObjectId("507f1f77bcf86cd799439011")}'
        expected = '{"_id": {"$oid": "507f1f77bcf86cd799439011"}}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Multiple ObjectIds
        input_str = '{"_id": {"$in": [ObjectId("507f1f77bcf86cd799439011"), ObjectId("507f1f77bcf86cd799439012")]}}'
        expected = '{"_id": {"$in": [{"$oid": "507f1f77bcf86cd799439011"}, {"$oid": "507f1f77bcf86cd799439012"}]}}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

    def test_numberlong_conversion(self):
        """Test NumberLong conversion"""
        # Without quotes
        input_str = "NumberLong(123456789)"
        expected = '{"$numberLong": "123456789"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # With quotes
        input_str = 'NumberLong("123456789")'
        expected = '{"$numberLong": "123456789"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Negative number without quotes
        input_str = "NumberLong(-123456789)"
        expected = '{"$numberLong": "-123456789"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Negative number with quotes
        input_str = 'NumberLong("-123456789")'
        expected = '{"$numberLong": "-123456789"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Large number
        input_str = "NumberLong(9223372036854775807)"
        expected = '{"$numberLong": "9223372036854775807"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

    def test_numberint_conversion(self):
        """Test NumberInt conversion"""
        # Without quotes
        input_str = "NumberInt(42)"
        expected = '{"$numberInt": "42"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # With quotes
        input_str = 'NumberInt("42")'
        expected = '{"$numberInt": "42"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Negative number
        input_str = "NumberInt(-42)"
        expected = '{"$numberInt": "-42"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Zero
        input_str = "NumberInt(0)"
        expected = '{"$numberInt": "0"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

    def test_numberdecimal_conversion(self):
        """Test NumberDecimal conversion"""
        # Basic decimal
        input_str = 'NumberDecimal("123.456")'
        expected = '{"$numberDecimal": "123.456"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Scientific notation
        input_str = 'NumberDecimal("1.23E+3")'
        expected = '{"$numberDecimal": "1.23E+3"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Negative decimal
        input_str = 'NumberDecimal("-99.999")'
        expected = '{"$numberDecimal": "-99.999"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Very small decimal
        input_str = 'NumberDecimal("0.000001")'
        expected = '{"$numberDecimal": "0.000001"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

    def test_timestamp_conversion(self):
        """Test Timestamp conversion"""
        # Basic timestamp
        input_str = "Timestamp(1234567890, 1)"
        expected = '{"$timestamp": {"t": 1234567890, "i": 1}}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # With spaces
        input_str = "Timestamp(1234567890,  1)"
        expected = '{"$timestamp": {"t": 1234567890, "i": 1}}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Large values
        input_str = "Timestamp(1609459200, 999999)"
        expected = '{"$timestamp": {"t": 1609459200, "i": 999999}}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

    def test_bindata_conversion(self):
        """Test BinData conversion"""
        # Generic binary subtype
        input_str = 'BinData(0, "SGVsbG8gV29ybGQ=")'
        expected = '{"$binary": {"base64": "SGVsbG8gV29ybGQ=", "subType": "0"}}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # UUID subtype
        input_str = 'BinData(3, "AQIDBAU=")'
        expected = '{"$binary": {"base64": "AQIDBAU=", "subType": "3"}}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # MD5 subtype
        input_str = 'BinData(5, "1B2M2Y8AsgTpgAmY7PhCfg==")'
        expected = '{"$binary": {"base64": "1B2M2Y8AsgTpgAmY7PhCfg==", "subType": "5"}}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

    def test_minkey_maxkey_conversion(self):
        """Test MinKey and MaxKey conversion"""
        # MinKey
        input_str = "MinKey()"
        expected = '{"$minKey": 1}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # MaxKey
        input_str = "MaxKey()"
        expected = '{"$maxKey": 1}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # MinKey and MaxKey in a range query
        input_str = '{"value": {"$gte": MinKey(), "$lte": MaxKey()}}'
        expected = '{"value": {"$gte": {"$minKey": 1}, "$lte": {"$maxKey": 1}}}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

    def test_uuid_conversion(self):
        """Test UUID conversion"""
        # Standard UUID
        input_str = 'UUID("550e8400-e29b-41d4-a716-446655440000")'
        expected = '{"$uuid": "550e8400-e29b-41d4-a716-446655440000"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # UUID with uppercase letters
        input_str = 'UUID("550E8400-E29B-41D4-A716-446655440000")'
        expected = '{"$uuid": "550E8400-E29B-41D4-A716-446655440000"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

    def test_dbref_conversion(self):
        """Test DBRef conversion"""
        # Basic DBRef
        input_str = 'DBRef("users", "507f1f77bcf86cd799439011")'
        expected = '{"$ref": "users", "$id": "507f1f77bcf86cd799439011"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # DBRef with spaces
        input_str = 'DBRef("users",  "507f1f77bcf86cd799439011")'
        expected = '{"$ref": "users", "$id": "507f1f77bcf86cd799439011"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # DBRef with collection containing dots
        input_str = 'DBRef("db.collection", "507f1f77bcf86cd799439011")'
        expected = '{"$ref": "db.collection", "$id": "507f1f77bcf86cd799439011"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

    def test_code_conversion(self):
        """Test Code conversion"""
        # Simple function
        input_str = 'Code("function() { return 1; }")'
        expected = '{"$code": "function() { return 1; }"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

        # Note: The regex r'Code\("([^"]+)"\)' matches Code(" followed by any
        # non-quote characters, so it won't match strings with escaped quotes inside.
        # This is a limitation but acceptable since Code with escaped quotes is rare.
        # Test that it doesn't convert when there are escaped quotes
        input_str = 'Code("function() { return \\"hello\\"; }")'
        # Should remain unchanged since the regex doesn't match
        expected = 'Code("function() { return \\"hello\\"; }")'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), expected)

    def test_complex_aggregation_pipeline(self):
        """Test a complete aggregation pipeline with multiple MongoDB constructs"""
        input_str = """[
            {
                "$match": {
                    "released": {
                        "$gte": ISODate("2010-01-01T00:00:00.000+0000"),
                        "$lte": ISODate("2015-12-31T23:59:59.000+0000")
                    },
                    "_id": ObjectId("507f1f77bcf86cd799439011"),
                    "count": NumberLong(1000000),
                    "price": NumberDecimal("99.99")
                }
            },
            {
                "$group": {
                    "_id": "$category",
                    "total": {"$sum": NumberDecimal("10.50")},
                    "count": {"$sum": NumberInt(1)}
                }
            }
        ]"""

        # Convert and parse to verify it's valid JSON
        converted = convert_mongo_shell_to_extended_json(input_str)

        # Should be parseable by json_util
        parsed = json_util.loads(converted)

        # Verify the structure
        self.assertIsInstance(parsed, list)
        self.assertEqual(len(parsed), 2)

        # Check first stage
        match_stage = parsed[0]["$match"]
        self.assertIn("released", match_stage)
        self.assertIsInstance(match_stage["released"]["$gte"], datetime)
        self.assertIsInstance(match_stage["released"]["$lte"], datetime)

        # Check that _id was converted properly
        self.assertEqual(str(match_stage["_id"]), "507f1f77bcf86cd799439011")

        # Check that NumberLong was converted
        self.assertEqual(match_stage["count"], 1000000)

        # Check second stage
        group_stage = parsed[1]["$group"]
        self.assertEqual(group_stage["_id"], "$category")

    def test_no_conversion_needed(self):
        """Test that valid Extended JSON is not modified"""
        # Already in Extended JSON format
        input_str = '{"$date": "2010-01-01T00:00:00.000Z"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), input_str)

        input_str = '{"$oid": "507f1f77bcf86cd799439011"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), input_str)

        input_str = '{"$numberLong": "123456789"}'
        self.assertEqual(convert_mongo_shell_to_extended_json(input_str), input_str)

    def test_mixed_content(self):
        """Test conversion with mixed MongoDB shell and regular JSON"""
        input_str = """
        {
            "name": "John Doe",
            "birthdate": ISODate("1990-01-01T00:00:00.000Z"),
            "user_id": ObjectId("507f1f77bcf86cd799439011"),
            "age": 30,
            "balance": NumberDecimal("1234.56"),
            "transaction_count": NumberLong(42),
            "score": NumberInt(100),
            "active": true,
            "tags": ["user", "premium"]
        }
        """

        converted = convert_mongo_shell_to_extended_json(input_str)

        # Should be parseable
        parsed = json_util.loads(converted)

        # Verify conversions
        self.assertEqual(parsed["name"], "John Doe")
        self.assertEqual(parsed["age"], 30)
        self.assertIsInstance(parsed["birthdate"], datetime)
        self.assertEqual(str(parsed["user_id"]), "507f1f77bcf86cd799439011")
        self.assertEqual(parsed["transaction_count"], 42)
        self.assertEqual(parsed["score"], 100)
        self.assertTrue(parsed["active"])
        self.assertEqual(parsed["tags"], ["user", "premium"])

    def test_real_world_query(self):
        """Test the actual query from the reproduction script"""
        input_str = '[{"$match":{"released":{"$gte":ISODate("2010-01-01T00:00:00.000+0000"),"$lte":ISODate("2015-12-31T23:59:59.000+0000")}}}]'

        converted = convert_mongo_shell_to_extended_json(input_str)

        # Should be parseable
        parsed = json_util.loads(converted)

        # Verify it's a valid aggregation pipeline
        self.assertIsInstance(parsed, list)
        self.assertEqual(len(parsed), 1)
        self.assertIn("$match", parsed[0])

        # Verify dates were converted properly
        match_stage = parsed[0]["$match"]
        self.assertIsInstance(match_stage["released"]["$gte"], datetime)
        self.assertIsInstance(match_stage["released"]["$lte"], datetime)

        # Verify the dates are correct
        self.assertEqual(match_stage["released"]["$gte"].year, 2010)
        self.assertEqual(match_stage["released"]["$gte"].month, 1)
        self.assertEqual(match_stage["released"]["$gte"].day, 1)
        self.assertEqual(match_stage["released"]["$lte"].year, 2015)
        self.assertEqual(match_stage["released"]["$lte"].month, 12)
        self.assertEqual(match_stage["released"]["$lte"].day, 31)

    def test_nested_conversions(self):
        """Test deeply nested MongoDB constructs"""
        input_str = """
        {
            "query": {
                "$or": [
                    {"created": {"$gte": ISODate("2020-01-01T00:00:00.000Z")}},
                    {"updated": {"$gte": ISODate("2020-06-01T00:00:00.000Z")}}
                ],
                "user": {
                    "$in": [
                        ObjectId("507f1f77bcf86cd799439011"),
                        ObjectId("507f1f77bcf86cd799439012")
                    ]
                },
                "metadata": {
                    "version": NumberInt(2),
                    "size": NumberLong(1024000),
                    "checksum": BinData(0, "AQIDBA==")
                }
            }
        }
        """

        converted = convert_mongo_shell_to_extended_json(input_str)
        parsed = json_util.loads(converted)

        # Verify nested conversions
        query = parsed["query"]
        self.assertIsInstance(query["$or"][0]["created"]["$gte"], datetime)
        self.assertIsInstance(query["$or"][1]["updated"]["$gte"], datetime)
        self.assertEqual(len(query["user"]["$in"]), 2)
        self.assertEqual(query["metadata"]["version"], 2)
        self.assertEqual(query["metadata"]["size"], 1024000)

    def test_edge_cases(self):
        """Test edge cases and special scenarios"""
        # Empty constructs
        self.assertEqual(
            convert_mongo_shell_to_extended_json("MinKey()"), '{"$minKey": 1}'
        )

        # Multiple conversions in one line
        input_str = '{"a": ObjectId("507f1f77bcf86cd799439011"), "b": ISODate("2020-01-01T00:00:00.000Z")}'
        converted = convert_mongo_shell_to_extended_json(input_str)
        parsed = json_util.loads(converted)
        self.assertEqual(str(parsed["a"]), "507f1f77bcf86cd799439011")
        self.assertIsInstance(parsed["b"], datetime)

        # Conversion in array elements
        input_str = '[ObjectId("507f1f77bcf86cd799439011"), NumberLong(42), ISODate("2020-01-01T00:00:00.000Z")]'
        converted = convert_mongo_shell_to_extended_json(input_str)
        parsed = json_util.loads(converted)
        self.assertEqual(len(parsed), 3)
        self.assertEqual(str(parsed[0]), "507f1f77bcf86cd799439011")
        self.assertEqual(parsed[1], 42)
        self.assertIsInstance(parsed[2], datetime)

    def test_all_types_together(self):
        """Test all supported MongoDB types in one document"""
        input_str = """
        {
            "objectId": ObjectId("507f1f77bcf86cd799439011"),
            "date": ISODate("2020-01-01T00:00:00.000Z"),
            "numberLong": NumberLong(9223372036854775807),
            "numberInt": NumberInt(2147483647),
            "numberDecimal": NumberDecimal("99.99"),
            "timestamp": Timestamp(1234567890, 1),
            "binData": BinData(0, "SGVsbG8="),
            "minKey": MinKey(),
            "maxKey": MaxKey(),
            "uuid": UUID("550e8400-e29b-41d4-a716-446655440000"),
            "dbRef": DBRef("collection", "507f1f77bcf86cd799439011"),
            "code": Code("function() { return true; }")
        }
        """

        converted = convert_mongo_shell_to_extended_json(input_str)

        # Should be valid Extended JSON that can be parsed
        parsed = json_util.loads(converted)

        # Verify all conversions worked
        self.assertIn("objectId", parsed)
        self.assertIn("date", parsed)
        self.assertIn("numberLong", parsed)
        self.assertIn("numberInt", parsed)
        self.assertIn("numberDecimal", parsed)
        self.assertIn("timestamp", parsed)
        self.assertIn("binData", parsed)
        self.assertIn("minKey", parsed)
        self.assertIn("maxKey", parsed)
        self.assertIn("uuid", parsed)
        self.assertIn("dbRef", parsed)
        self.assertIn("code", parsed)


if __name__ == "__main__":
    unittest.main()
