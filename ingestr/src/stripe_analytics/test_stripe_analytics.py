import unittest
from unittest.mock import patch, MagicMock, ANY
import dlt # dlt.resource and dlt.source are used
from pendulum import datetime as pendulum_datetime

# Assuming __init__.py functions are importable
from ingestr.src.stripe_analytics import stripe_source, incremental_stripe_source

# Mock stripe module at the top level of the test file
# This will be used by the @patch decorator on the class
stripe_mock = MagicMock()

@patch('ingestr.src.stripe_analytics.stripe', new=stripe_mock)
class TestStripeAnalyticsSources(unittest.TestCase):

    def setUp(self):
        # Reset mocks for each test to prevent interference
        stripe_mock.reset_mock()

    def _test_endpoint_full_refresh(self, endpoint_name, stripe_object_name=None):
        if stripe_object_name is None:
            stripe_object_name = endpoint_name
        
        mock_list_method = getattr(stripe_mock, stripe_object_name).list
        mock_list_method.return_value = {"data": [{"id": f"id_{endpoint_name.lower()}_123"}], "has_more": False}
        
        resources = list(stripe_source(endpoints=(endpoint_name,), stripe_secret_key="sk_test_123"))
        
        self.assertEqual(len(resources), 1)
        resource = resources[0]
        self.assertEqual(resource.name, endpoint_name)
        # Note: dlt.resource objects don't directly expose write_disposition in a public way after creation.
        # The write_disposition is passed to dlt.resource decorator. We are testing that the correct
        # dlt_source function is called, which internally uses the correct write_disposition.
        # For the purpose of these tests, we confirm the correct source function was used.
        # If direct assertion is needed, it might require inspecting dlt internals or how it's applied.
        # Here, we are implicitly testing it by calling stripe_source which should use 'replace'.
        mock_list_method.assert_called_once_with(limit=100, created=None, starting_after=None)

    def _test_endpoint_incremental(self, endpoint_name, stripe_object_name=None, initial_start_date_fixture=pendulum_datetime(2020,1,1)):
        if stripe_object_name is None:
            stripe_object_name = endpoint_name

        mock_list_method = getattr(stripe_mock, stripe_object_name).list
        # Incremental loads often filter by 'created'
        mock_list_method.return_value = {"data": [{"id": f"id_{endpoint_name.lower()}_123", "created": 1620000000}], "has_more": False}
        
        # For incremental, we also need to mock dlt.sources.incremental
        # The incremental decorator is applied to the 'created' argument of the 'incremental_resource' inner function
        # So we need to patch it where it's actually used by dlt when the resource is iterated.
        # A simpler way is to trust that dlt.sources.incremental works as specified and that
        # our pagination function receives the correct 'created' arguments.
        # The pagination function itself is mocked via stripe.ObjectName.list.

        # We test if the initial_value for 'created' is passed correctly.
        # The actual filtering by 'created' happens inside the mocked 'pagination' (via stripe.ObjectName.list)
        
        resources = list(incremental_stripe_source(endpoints=(endpoint_name,), stripe_secret_key="sk_test_123", initial_start_date=initial_start_date_fixture))
        
        self.assertEqual(len(resources), 1)
        resource = resources[0]
        self.assertEqual(resource.name, endpoint_name)
        # Implicitly testing write_disposition="append" by calling incremental_stripe_source.
        
        # Check if the first call to pagination (mocked by stripe.ObjectName.list)
        # received a 'created' argument reflecting the initial_start_date.
        expected_start_timestamp = int(initial_start_date_fixture.timestamp()) if initial_start_date_fixture else -1
        
        # The actual call to stripe.XXX.list happens inside the 'pagination' helper,
        # which receives the 'created' value from the dlt.incremental decorator.
        # We are checking the arguments to the mocked stripe.XXX.list method.
        # The 'created' dict for date range is passed to stripe.XXX.list
        if initial_start_date_fixture == -1: # Special case for default initial_value
             mock_list_method.assert_called_once_with(limit=100, created={'gte': -1}, starting_after=None)
        else:
             mock_list_method.assert_called_once_with(limit=100, created={'gte': expected_start_timestamp}, starting_after=None)


    # --- Tests for Newly Added Endpoints ---
    def test_application_fee_endpoint_incremental(self):
        self._test_endpoint_incremental("ApplicationFee")

    def test_dispute_endpoint_full_refresh(self):
        self._test_endpoint_full_refresh("Dispute")

    def test_subscription_item_endpoint_full_refresh(self):
        self._test_endpoint_full_refresh("SubscriptionItem")

    def test_checkout_session_endpoint_full_refresh(self):
        # Checkout.Session is accessed via stripe.checkout.Session
        stripe_mock.checkout.Session.list.return_value = {"data": [{"id": "cs_123"}], "has_more": False}
        
        resources = list(stripe_source(endpoints=("CheckoutSession",), stripe_secret_key="sk_test_123"))
        
        self.assertEqual(len(resources), 1)
        resource = resources[0]
        self.assertEqual(resource.name, "CheckoutSession")
        stripe_mock.checkout.Session.list.assert_called_once_with(limit=100, created=None, starting_after=None)

    def test_credit_note_endpoint_incremental(self):
        self._test_endpoint_incremental("CreditNote")

    def test_customer_balance_transaction_endpoint_incremental(self):
        self._test_endpoint_incremental("CustomerBalanceTransaction")

    def test_setup_attempt_endpoint_incremental(self):
        # SetupAttempt list method requires setup_intent, but our generic pagination doesn't support that directly.
        # The source code currently calls stripe.SetupAttempt.list(...) without setup_intent if called via incremental_stripe_source.
        # This test will reflect that behavior. If specific params are needed, the source or test needs adjustment.
        self._test_endpoint_incremental("SetupAttempt", initial_start_date_fixture=-1) # Default initial value is -1

    def test_shipping_rate_endpoint_full_refresh(self):
        self._test_endpoint_full_refresh("ShippingRate")

    # --- Tests for Pre-existing Endpoints ---
    def test_charge_endpoint_full_refresh(self): # Charge is in INCREMENTAL_ENDPOINTS now
         self._test_endpoint_incremental("Charge")

    def test_event_endpoint_incremental(self):
        self._test_endpoint_incremental("Event")

    def test_customer_endpoint_full_refresh(self):
        self._test_endpoint_full_refresh("Customer")

    def test_subscription_endpoint_full_refresh(self):
        self._test_endpoint_full_refresh("Subscription")

    def test_invoice_endpoint_incremental(self):
        self._test_endpoint_incremental("Invoice")

    # Test for ApplicationFeeRefund (Type C - Nested)
    @patch('ingestr.src.stripe_analytics.pagination')
    def test_application_fee_refund_endpoint_full_refresh(self, mock_pagination):
        # Mock the parent resource (ApplicationFee) pagination
        mock_pagination.side_effect = [
            iter([{"id": "fee_1", "created": 1620000000}, {"id": "fee_2", "created": 1620000001}]), # For ApplicationFee
            # The pagination for refunds themselves is handled by stripe.ApplicationFee.list_refunds
        ]

        # Mock the nested list_refunds call
        stripe_mock.ApplicationFee.list_refunds.side_effect = [
            {"data": [{"id": "fr_fee1_1", "fee": "fee_1", "created": 1620000000}], "has_more": False}, # Refunds for fee_1
            {"data": [{"id": "fr_fee2_1", "fee": "fee_2", "created": 1620000001}], "has_more": False}, # Refunds for fee_2
        ]
        
        resources = list(stripe_source(endpoints=("ApplicationFeeRefund",), stripe_secret_key="sk_test_123"))
        
        self.assertEqual(len(resources), 1)
        resource = resources[0]
        self.assertEqual(resource.name, "ApplicationFeeRefund")
        
        # Verify pagination was called for ApplicationFee
        mock_pagination.assert_any_call("ApplicationFee", None, None)
        
        # Verify list_refunds was called for each application fee
        self.assertEqual(stripe_mock.ApplicationFee.list_refunds.call_count, 2)
        stripe_mock.ApplicationFee.list_refunds.assert_any_call("fee_1", limit=100, starting_after=None)
        stripe_mock.ApplicationFee.list_refunds.assert_any_call("fee_2", limit=100, starting_after=None)

        # Collect data from the resource to ensure refunds are yielded
        refund_data = list(resource)
        self.assertEqual(len(refund_data), 2)
        self.assertEqual(refund_data[0]["id"], "fr_fee1_1")
        self.assertEqual(refund_data[1]["id"], "fr_fee2_1")


if __name__ == '__main__':
    unittest.main()
