import random
import unittest
from datetime import date, datetime
from unittest.mock import patch

from ingestr.src.masking import MaskingEngine, create_masking_mapper


class TestMaskingEngine(unittest.TestCase):
    def setUp(self):
        self.engine = MaskingEngine()
        # Set seed for reproducible tests
        random.seed(42)

    def test_parse_mask_config(self):
        # Test basic config
        col, algo, param = self.engine.parse_mask_config("email:hash")
        self.assertEqual(col, "email")
        self.assertEqual(algo, "hash")
        self.assertIsNone(param)

        # Test config with parameter
        col, algo, param = self.engine.parse_mask_config("age:round:10")
        self.assertEqual(col, "age")
        self.assertEqual(algo, "round")
        self.assertEqual(param, "10")

        # Test invalid config
        with self.assertRaises(ValueError):
            self.engine.parse_mask_config("invalid")

    def test_hash_sha256(self):
        result = self.engine._hash_sha256("test@example.com")
        self.assertEqual(
            result, "973dfe463ec85785f5f95af5ba3906eedb2d931c24e69824a89ea65dba4e813b"
        )

        # Test consistency
        result2 = self.engine._hash_sha256("test@example.com")
        self.assertEqual(result, result2)

        # Test None handling
        self.assertIsNone(self.engine._hash_sha256(None))

    def test_hash_md5(self):
        result = self.engine._hash_md5("test@example.com")
        self.assertEqual(result, "55502f40dc8b7c769880b10874abc9d0")

        # Test None handling
        self.assertIsNone(self.engine._hash_md5(None))

    def test_hash_hmac(self):
        result = self.engine._hash_hmac("test@example.com", "secret-key")
        # HMAC should produce consistent results with same key
        result2 = self.engine._hash_hmac("test@example.com", "secret-key")
        self.assertEqual(result, result2)

        # Different keys should produce different results
        result3 = self.engine._hash_hmac("test@example.com", "different-key")
        self.assertNotEqual(result, result3)

    def test_mask_email(self):
        result = self.engine._mask_email("john.doe@example.com")
        self.assertTrue(result.endswith("@example.com"))
        self.assertTrue("*" in result)

        # Test short email
        result = self.engine._mask_email("ab@test.com")
        self.assertEqual(result, "**@test.com")

        # Test invalid email (no @)
        result = self.engine._mask_email("notanemail")
        self.assertTrue("*" in result)

        # Test None handling
        self.assertIsNone(self.engine._mask_email(None))

    def test_mask_phone(self):
        result = self.engine._mask_phone("555-123-4567")
        self.assertEqual(result, "555-***-****")

        result = self.engine._mask_phone("+1-555-123-4567")
        self.assertTrue("***" in result)

        result = self.engine._mask_phone("12345")
        self.assertEqual(result, "*****")

        self.assertIsNone(self.engine._mask_phone(None))

    def test_mask_credit_card(self):
        result = self.engine._mask_credit_card("4111-1111-1111-1111")
        self.assertEqual(result, "************1111")

        result = self.engine._mask_credit_card("4111111111111111")
        self.assertEqual(result, "************1111")

        result = self.engine._mask_credit_card("1234")
        self.assertEqual(result, "****")

        self.assertIsNone(self.engine._mask_credit_card(None))

    def test_mask_ssn(self):
        result = self.engine._mask_ssn("123-45-6789")
        self.assertEqual(result, "***-**-6789")

        result = self.engine._mask_ssn("123456789")
        self.assertEqual(result, "***-**-6789")

        result = self.engine._mask_ssn("12345")
        self.assertEqual(result, "*****")

        self.assertIsNone(self.engine._mask_ssn(None))

    def test_partial_mask(self):
        result = self.engine._partial_mask("Jonathan", 2)
        self.assertEqual(result, "Jo****an")

        result = self.engine._partial_mask("test", 1)
        self.assertEqual(result, "t**t")

        result = self.engine._partial_mask("ab", 2)
        self.assertEqual(result, "**")

        self.assertIsNone(self.engine._partial_mask(None, 2))

    def test_first_letter_mask(self):
        result = self.engine._first_letter_mask("Alice")
        self.assertEqual(result, "A****")

        result = self.engine._first_letter_mask("B")
        self.assertEqual(result, "B")

        # Test None handling
        self.assertIsNone(self.engine._first_letter_mask(None))

    def test_tokenize_uuid(self):
        result1 = self.engine._tokenize_uuid("customer1")
        result2 = self.engine._tokenize_uuid("customer1")

        # Same value should get same UUID
        self.assertEqual(result1, result2)

        # Different values should get different UUIDs
        result3 = self.engine._tokenize_uuid("customer2")
        self.assertNotEqual(result1, result3)

        # Test None handling
        self.assertIsNone(self.engine._tokenize_uuid(None))

    def test_tokenize_sequential(self):
        engine = MaskingEngine()  # Fresh engine for clean counter
        result1 = engine._tokenize_sequential("customer1")
        self.assertEqual(result1, 1)

        result2 = engine._tokenize_sequential("customer2")
        self.assertEqual(result2, 2)

        # Same value should get same sequential ID
        result3 = engine._tokenize_sequential("customer1")
        self.assertEqual(result3, 1)

        # Test None handling
        self.assertIsNone(engine._tokenize_sequential(None))

    def test_round_number(self):
        result = self.engine._round_number(52300, 5000)
        self.assertEqual(result, 50000)

        result = self.engine._round_number(34, 10)
        self.assertEqual(result, 30)

        result = self.engine._round_number(37.5, 10)
        self.assertEqual(result, 40)

        # Test non-numeric value
        result = self.engine._round_number("not_a_number", 10)
        self.assertEqual(result, "not_a_number")

        # Test None handling
        self.assertIsNone(self.engine._round_number(None, 10))

    def test_range_mask(self):
        result = self.engine._range_mask(45000, 10000)
        self.assertEqual(result, "40000-50000")

        result = self.engine._range_mask(234, 100)
        self.assertEqual(result, "200-300")

        # Test non-numeric value
        result = self.engine._range_mask("not_a_number", 100)
        self.assertEqual(result, "not_a_number")

        # Test None handling
        self.assertIsNone(self.engine._range_mask(None, 100))

    def test_add_noise(self):
        # Reset seed for consistent test
        random.seed(42)
        result = self.engine._add_noise(100, 0.1)
        # Result should be within 10% of original
        self.assertTrue(90 <= result <= 110)

        # Test integer preservation
        result = self.engine._add_noise(100, 0.1)
        self.assertIsInstance(result, int)

        # Test float preservation
        result = self.engine._add_noise(100.5, 0.1)
        self.assertIsInstance(result, float)

        # Test non-numeric value
        result = self.engine._add_noise("not_a_number", 0.1)
        self.assertEqual(result, "not_a_number")

        # Test None handling
        self.assertIsNone(self.engine._add_noise(None, 0.1))

    def test_year_only(self):
        test_date = date(2024, 3, 15)
        result = self.engine._year_only(test_date)
        self.assertEqual(result, 2024)

        test_datetime = datetime(2024, 3, 15, 10, 30)
        result = self.engine._year_only(test_datetime)
        self.assertEqual(result, 2024)

        # Test string date
        result = self.engine._year_only("2024-03-15")
        self.assertEqual(result, 2024)

        # Test invalid date string
        result = self.engine._year_only("not_a_date")
        self.assertEqual(result, "not_a_date")

        # Test None handling
        self.assertIsNone(self.engine._year_only(None))

    def test_month_year(self):
        test_date = date(2024, 3, 15)
        result = self.engine._month_year(test_date)
        self.assertEqual(result, "2024-03")

        test_datetime = datetime(2024, 3, 15, 10, 30)
        result = self.engine._month_year(test_datetime)
        self.assertEqual(result, "2024-03")

        # Test string date
        result = self.engine._month_year("2024-03-15")
        self.assertEqual(result, "2024-03")

        # Test invalid date string
        result = self.engine._month_year("not_a_date")
        self.assertEqual(result, "not_a_date")

        # Test None handling
        self.assertIsNone(self.engine._month_year(None))

    def test_get_masking_function(self):
        # Test hash algorithms
        func = self.engine.get_masking_function("hash")
        self.assertIsNotNone(func)

        func = self.engine.get_masking_function("sha256")
        self.assertIsNotNone(func)

        func = self.engine.get_masking_function("md5")
        self.assertIsNotNone(func)

        # Test with parameters
        func = self.engine.get_masking_function("partial", "3")
        result = func("testing")
        self.assertEqual(result, "tes*ing")

        func = self.engine.get_masking_function("round", "100")
        result = func(456)
        self.assertEqual(result, 500)

        # Test fixed value
        func = self.engine.get_masking_function("fixed", "MASKED")
        result = func("anything")
        self.assertEqual(result, "MASKED")

        # Test unknown algorithm
        with self.assertRaises(ValueError):
            self.engine.get_masking_function("unknown_algo")


class TestCreateMaskingMapper(unittest.TestCase):
    def test_create_masking_mapper_basic(self):
        mask_configs = ["email:hash", "phone:partial:3", "ssn:redact"]

        mapper = create_masking_mapper(mask_configs)

        row = {
            "email": "test@example.com",
            "phone": "555-123-4567",
            "ssn": "123-45-6789",
            "name": "John Doe",
        }

        result = mapper(row)

        # Email should be hashed
        self.assertEqual(
            result["email"],
            "973dfe463ec85785f5f95af5ba3906eedb2d931c24e69824a89ea65dba4e813b",
        )

        # Phone should be partially masked
        self.assertTrue("*" in result["phone"])

        # SSN should be redacted
        self.assertEqual(result["ssn"], "REDACTED")

        # Name should be unchanged
        self.assertEqual(result["name"], "John Doe")

    def test_create_masking_mapper_empty_config(self):
        mapper = create_masking_mapper([])

        row = {"email": "test@example.com"}
        result = mapper(row)

        # Should return unchanged
        self.assertEqual(result, row)

    def test_create_masking_mapper_non_dict_row(self):
        mask_configs = ["email:hash"]
        mapper = create_masking_mapper(mask_configs)

        # Non-dict should be returned unchanged
        result = mapper("not a dict")
        self.assertEqual(result, "not a dict")

        result = mapper(None)
        self.assertIsNone(result)

        result = mapper(123)
        self.assertEqual(result, 123)

    def test_create_masking_mapper_missing_column(self):
        mask_configs = ["email:hash", "phone:redact"]
        mapper = create_masking_mapper(mask_configs)

        row = {"name": "John Doe"}  # No email or phone
        result = mapper(row)

        # Should not add new columns, just return unchanged
        self.assertEqual(result, {"name": "John Doe"})

    def test_create_masking_mapper_with_numeric_masks(self):
        mask_configs = ["age:round:10", "salary:range:10000", "score:noise:0.1"]

        # Set seed for reproducible noise test
        random.seed(42)

        mapper = create_masking_mapper(mask_configs)

        row = {"age": 34, "salary": 45000, "score": 100}

        result = mapper(row)

        self.assertEqual(result["age"], 30)
        self.assertEqual(result["salary"], "40000-50000")
        # Score should be within noise range
        self.assertTrue(90 <= result["score"] <= 110)

    @patch("builtins.print")
    def test_create_masking_mapper_error_handling(self, mock_print):
        mask_configs = ["value:round:10"]  # round expects numeric
        mapper = create_masking_mapper(mask_configs)

        row = {"value": "not_a_number"}
        result = mapper(row)

        # Should handle error gracefully and return unchanged
        self.assertEqual(result["value"], "not_a_number")

        # But for valid numeric it should work
        row2 = {"value": 123}
        result2 = mapper(row2)
        self.assertEqual(result2["value"], 120)


if __name__ == "__main__":
    unittest.main()
