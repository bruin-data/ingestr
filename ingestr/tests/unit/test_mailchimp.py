import unittest

from ingestr.src.errors import MissingValueError
from ingestr.src.sources import MailchimpSource


class TestMailchimpSource(unittest.TestCase):
    def setUp(self):
        self.source = MailchimpSource()

    def test_valid_mailchimp_uri(self):
        """Test that valid mailchimp:// URI works"""
        uri = "mailchimp://?api_key=test_key&server=us1"
        # Should not raise an error
        try:
            self.source.dlt_source(uri, "lists")
        except Exception as e:
            # We expect it to fail on actual API call, not on URI validation
            self.assertNotIsInstance(e, ValueError)
            self.assertNotIn("Invalid URI scheme", str(e))

    def test_invalid_scheme(self):
        """Test that non-mailchimp:// schemes are rejected"""
        invalid_uris = [
            "http://?api_key=test_key&server=us1",
            "https://?api_key=test_key&server=us1",
            "foobar://?api_key=test_key&server=us1",
            "?api_key=test_key&server=us1",
        ]

        for uri in invalid_uris:
            with self.subTest(uri=uri):
                with self.assertRaises(ValueError) as context:
                    self.source.dlt_source(uri, "lists")
                self.assertIn("Invalid URI scheme", str(context.exception))
                self.assertIn("Expected 'mailchimp://'", str(context.exception))

    def test_missing_api_key(self):
        """Test that missing api_key raises MissingValueError"""
        uri = "mailchimp://?server=us1"
        with self.assertRaises(MissingValueError) as context:
            self.source.dlt_source(uri, "lists")
        self.assertIn("api_key", str(context.exception))

    def test_missing_server(self):
        """Test that missing server raises MissingValueError"""
        uri = "mailchimp://?api_key=test_key"
        with self.assertRaises(MissingValueError) as context:
            self.source.dlt_source(uri, "lists")
        self.assertIn("server", str(context.exception))

    def test_missing_both_parameters(self):
        """Test that missing both api_key and server raises MissingValueError"""
        uri = "mailchimp://"
        with self.assertRaises(MissingValueError):
            self.source.dlt_source(uri, "lists")

    def test_handles_incrementality(self):
        """Test that Mailchimp source does not handle incrementality"""
        self.assertFalse(self.source.handles_incrementality())


if __name__ == "__main__":
    unittest.main()
