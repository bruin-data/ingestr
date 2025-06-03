import unittest
from unittest.mock import MagicMock, patch
import pendulum # Ensure pendulum is imported if used in assertions or setup

from ingestr.src.salesforce.helpers import get_records

class TestSalesforceGetRecords(unittest.TestCase):

    def setUp(self):
        self.mock_sf = MagicMock()
        self.mock_sf.bulk = MagicMock()

        # Common describe response
        self.describe_response = {
            "fields": [
                {"name": "Id", "type": "id", "compoundFieldName": None},
                {"name": "Name", "type": "string", "compoundFieldName": None},
                {"name": "CustomField__c", "type": "string", "compoundFieldName": None},
                {"name": "LastModifiedDate", "type": "datetime", "compoundFieldName": None},
                {"name": "BillingAddress", "type": "address", "compoundFieldName": None}, # Compound field
                {"name": "BillingStreet", "type": "string", "compoundFieldName": "BillingAddress"},
            ]
        }

        # Common query data
        self.query_data_page1 = [
            {"Id": "1", "Name": "Test Record 1", "LastModifiedDate": 1678886400000, "attributes": {"type": "Object"}}, # March 15, 2023
            {"Id": "2", "Name": "Test Record 2", "LastModifiedDate": 1678972800000, "attributes": {"type": "Object"}}, # March 16, 2023
        ]

    def test_get_records_custom_object(self):
        # Mock describe for custom object
        self.mock_sf.MyCustomObject__c.describe.return_value = self.describe_response
        # Mock bulk query for custom object
        self.mock_sf.bulk.MyCustomObject__c.query_all.return_value = iter([self.query_data_page1])

        # Expected fields in query (excluding BillingAddress, including BillingStreet)
        expected_fields = "Id, Name, CustomField__c, LastModifiedDate, BillingStreet"
        expected_query = f"SELECT {expected_fields} FROM MyCustomObject__c  " # Note the trailing space for potential predicate/orderby

        records = list(get_records(self.mock_sf, "custom:MyCustomObject"))

        self.mock_sf.MyCustomObject__c.describe.assert_called_once()
        self.mock_sf.bulk.MyCustomObject__c.query_all.assert_called_once_with(
            expected_query, # simple_salesforce might strip the query
            lazy_operation=True
        )

        self.assertEqual(len(records), 2)
        self.assertNotIn("attributes", records[0])
        self.assertEqual(records[0]["LastModifiedDate"], pendulum.from_timestamp(1678886400000 / 1000).strftime("%Y-%m-%dT%H:%M:%S.%fZ"))

    def test_get_records_standard_object(self):
        self.mock_sf.Account.describe.return_value = self.describe_response
        self.mock_sf.bulk.Account.query_all.return_value = iter([self.query_data_page1])

        expected_fields = "Id, Name, CustomField__c, LastModifiedDate, BillingStreet"
        expected_query = f"SELECT {expected_fields} FROM Account  " # Note the trailing space

        records = list(get_records(self.mock_sf, "Account"))

        self.mock_sf.Account.describe.assert_called_once()
        self.mock_sf.bulk.Account.query_all.assert_called_once_with(
            expected_query,
            lazy_operation=True
        )
        self.assertEqual(len(records), 2)
        self.assertNotIn("attributes", records[0])

    def test_get_records_with_replication_key(self):
        self.mock_sf.Opportunity.describe.return_value = self.describe_response
        self.mock_sf.bulk.Opportunity.query_all.return_value = iter([self.query_data_page1])

        expected_fields = "Id, Name, CustomField__c, LastModifiedDate, BillingStreet"
        last_state_val = "2023-01-01T00:00:00.000Z"
        replication_key_field = "LastModifiedDate"

        expected_query = f"SELECT {expected_fields} FROM Opportunity WHERE {replication_key_field} > {last_state_val} ORDER BY {replication_key_field} ASC"

        records = list(get_records(self.mock_sf, "Opportunity", last_state=last_state_val, replication_key=replication_key_field))

        self.mock_sf.Opportunity.describe.assert_called_once()
        self.mock_sf.bulk.Opportunity.query_all.assert_called_once_with(
            expected_query.strip(),
            lazy_operation=True
        )
        self.assertEqual(len(records), 2)

    def test_get_records_datetime_conversion(self):
        self.mock_sf.Event.describe.return_value = {
            "fields": [
                {"name": "Id", "type": "id", "compoundFieldName": None},
                {"name": "StartDateTime", "type": "datetime", "compoundFieldName": None},
            ]
        }
        query_data = [{"Id": "event1", "StartDateTime": 1678886400000}] # March 15, 2023
        self.mock_sf.bulk.Event.query_all.return_value = iter([query_data])

        records = list(get_records(self.mock_sf, "Event"))

        self.assertEqual(len(records), 1)
        expected_datetime_str = pendulum.from_timestamp(1678886400000 / 1000).strftime("%Y-%m-%dT%H:%M:%S.%fZ")
        self.assertEqual(records[0]["StartDateTime"], expected_datetime_str)

    def test_get_records_no_compound_fields_in_query(self):
        # Test that the main compound field (e.g. BillingAddress) is not in the SELECT query,
        # but its constituents (e.g. BillingStreet) are.
        self.mock_sf.Contact.describe.return_value = self.describe_response # Re-use setup describe
        self.mock_sf.bulk.Contact.query_all.return_value = iter([[]]) # No data needed for this check

        list(get_records(self.mock_sf, "Contact"))

        self.mock_sf.Contact.describe.assert_called_once()
        # Check the actual query string passed to query_all
        call_args = self.mock_sf.bulk.Contact.query_all.call_args
        self.assertIsNotNone(call_args)
        query_string = call_args[0][0]
        self.assertNotIn("BillingAddress", query_string.split(" FROM ")[0]) # Check only fields part
        self.assertIn("BillingStreet", query_string.split(" FROM ")[0])


if __name__ == "__main__":
    unittest.main()
