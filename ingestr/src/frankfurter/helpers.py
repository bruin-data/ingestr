from datetime import datetime
from typing import Optional, Tuple

from dlt.sources.helpers import requests
from dlt.common.typing import StrAny

FRANKFURTER_API_URL = "https://api.frankfurter.dev/v1/"

def get_url_with_retry(url: str) -> StrAny:
    r = requests.get(url)
    return r.json()  # type: ignore

def get_path_with_retry(path: str) -> StrAny:
    return get_url_with_retry(f"{FRANKFURTER_API_URL}{path}")

CURRENCY_CODES = {
  "AUD": "Australian Dollar",
  "BGN": "Bulgarian Lev",
  "BRL": "Brazilian Real",
  "CAD": "Canadian Dollar",
  "CHF": "Swiss Franc",
  "CNY": "Chinese Renminbi Yuan",
  "CZK": "Czech Koruna",
  "DKK": "Danish Krone",
  "EUR": "Euro",
  "GBP": "British Pound",
  "HKD": "Hong Kong Dollar",
  "HUF": "Hungarian Forint",
  "IDR": "Indonesian Rupiah",
  "ILS": "Israeli New Sheqel",
  "INR": "Indian Rupee",
  "ISK": "Icelandic Króna",
  "JPY": "Japanese Yen",
  "KRW": "South Korean Won",
  "MXN": "Mexican Peso",
  "MYR": "Malaysian Ringgit",
  "NOK": "Norwegian Krone",
  "NZD": "New Zealand Dollar",
  "PHP": "Philippine Peso",
  "PLN": "Polish Złoty",
  "RON": "Romanian Leu",
  "SEK": "Swedish Krona",
  "SGD": "Singapore Dollar",
  "THB": "Thai Baht",
  "TRY": "Turkish Lira",
  "USD": "United States Dollar",
  "ZAR": "South African Rand"
}


def validate_dates(start_date: Optional[str], end_date: Optional[str]) -> Tuple[datetime, Optional[datetime]]:
   
    date_format = "%Y-%m-%d"

    # Validate and format start_date 
    if start_date:
        try:
            start_date_obj = datetime.strptime(start_date, date_format)
        except ValueError:
            raise ValueError(f"Start_date invalid: '{start_date}'. The date is either invalid or does not conform to expected format: YYYY-MM-DD.")
    else:
        # If no start_date is provided, use the current date
        start_date = datetime.now().strptime("%Y-%m-%d")

    # Validate and format end_date format
    if end_date:
        try:
            end_date_obj = datetime.strptime(end_date, date_format)
        except ValueError:
            raise ValueError(f"End_date invalid: '{end_date}'. The date is either invalid or does not conform to expected format: YYYY-MM-DD.")
    else:
        end_date = None

    # Check if end_date is after start_date
    if start_date and end_date:
        if end_date_obj <= start_date_obj:
            raise ValueError("End date must be after start date.")
    
    return start_date, end_date


# from typing import List

def validate_currency_codes(base_currency: str, target_currency_list: str) -> None:
    """
    Validates the base currency and target currencies against a predefined list of valid currency codes.

    Args:
        base_currency (str): The base currency to validate.
        target_currency_list (str): A comma-separated string of target currencies to validate.

    Raises:
        ValueError: If any currency code in the base or target list is invalid.
    """
    # Get the list of valid currency codes from the CURRENCY_CODES dictionary
    valid_codes = set(CURRENCY_CODES.keys())

    # Split the target_currency_list into individual currencies
    target_currencies = target_currency_list.split(",") if target_currency_list else []

    # Combine base currency and target currencies into a single list
    all_currencies = [base_currency] + target_currencies

    # Filter out None or empty values
    all_currencies = [code for code in all_currencies if code]

    # Find invalid codes
    invalid_codes = [code for code in all_currencies if code not in valid_codes]

    # Raise an error if there are invalid codes
    if invalid_codes:
        raise ValueError(
            f"Invalid currency codes: {', '.join(invalid_codes)}. "
            f"Valid codes are: {', '.join(valid_codes)}"
        )