import hashlib
import hmac
import random
import re
import string
import uuid
from datetime import date, datetime, timedelta
from typing import Any, Callable, Dict, Optional, Tuple, Union


class MaskingEngine:
    def __init__(self):
        self.token_cache: Dict[str, Union[str, int]] = {}
        self.sequential_counter = 0

    def parse_mask_config(self, config: str) -> Tuple[str, str, Optional[str]]:
        parts = config.split(":")
        if len(parts) == 2:
            return parts[0], parts[1], None
        elif len(parts) == 3:
            return parts[0], parts[1], parts[2]
        else:
            raise ValueError(
                f"Invalid mask configuration: {config}. Expected format: 'column:algorithm[:param]'"
            )

    def get_masking_function(
        self, algorithm: str, param: Optional[str] = None
    ) -> Callable:
        algorithm = algorithm.lower()

        # Hash-based masking
        if algorithm == "hash" or algorithm == "sha256":
            return self._hash_sha256
        elif algorithm == "md5":
            return self._hash_md5
        elif algorithm == "hmac":
            return lambda x: self._hash_hmac(x, param or "default-key")

        # Format-preserving masking
        elif algorithm == "email":
            return self._mask_email
        elif algorithm == "phone":
            return self._mask_phone
        elif algorithm == "credit_card":
            return self._mask_credit_card
        elif algorithm == "ssn":
            return self._mask_ssn

        # Redaction strategies
        elif algorithm == "redact":
            return lambda x: "REDACTED"
        elif algorithm == "stars":
            return lambda x: "*" * len(str(x)) if x else ""
        elif algorithm == "fixed":
            return lambda x: param or "MASKED"
        elif algorithm == "random":
            return self._random_replace

        # Partial masking
        elif algorithm == "partial":
            chars = int(param) if param else 2
            return lambda x: self._partial_mask(x, chars)
        elif algorithm == "first_letter":
            return self._first_letter_mask

        # Tokenization
        elif algorithm == "uuid":
            return self._tokenize_uuid
        elif algorithm == "sequential":
            return self._tokenize_sequential

        # Numeric masking
        elif algorithm == "round":
            precision = int(param) if param else 10
            return lambda x: self._round_number(x, precision)
        elif algorithm == "range":
            bucket_size = int(param) if param else 100
            return lambda x: self._range_mask(x, bucket_size)
        elif algorithm == "noise":
            noise_level = float(param) if param else 0.1
            return lambda x: self._add_noise(x, noise_level)

        # Date masking
        elif algorithm == "date_shift":
            max_days = int(param) if param else 30
            return lambda x: self._date_shift(x, max_days)
        elif algorithm == "year_only":
            return self._year_only
        elif algorithm == "month_year":
            return self._month_year

        else:
            raise ValueError(f"Unknown masking algorithm: {algorithm}")

    # Hash functions
    def _hash_sha256(self, value: Any) -> Optional[str]:
        if value is None:
            return None
        return hashlib.sha256(str(value).encode()).hexdigest()

    def _hash_md5(self, value: Any) -> Optional[str]:
        if value is None:
            return None
        return hashlib.md5(str(value).encode()).hexdigest()

    def _hash_hmac(self, value: Any, key: str) -> Optional[str]:
        if value is None:
            return None
        return hmac.new(key.encode(), str(value).encode(), hashlib.sha256).hexdigest()

    # Format-preserving masks
    def _mask_email(self, value: Any) -> Any:
        if value is None or not value:
            return value
        email_str = str(value)
        if "@" not in email_str:
            return self._partial_mask(email_str, 2)

        local, domain = email_str.split("@", 1)
        if len(local) <= 2:
            masked_local = "*" * len(local)
        else:
            masked_local = local[0] + "*" * (len(local) - 2) + local[-1]
        return f"{masked_local}@{domain}"

    def _mask_phone(self, value: Any) -> Any:
        if value is None or not value:
            return value
        phone_str = re.sub(r"\D", "", str(value))
        if len(phone_str) < 10:
            return "*" * len(phone_str)

        # Keep country code and area code, mask the rest
        if len(phone_str) >= 10:
            return phone_str[:3] + "-***-****"
        return phone_str

    def _mask_credit_card(self, value: Any) -> Any:
        if value is None or not value:
            return value
        cc_str = re.sub(r"\D", "", str(value))
        if len(cc_str) < 12:
            return "*" * len(cc_str)
        return "*" * (len(cc_str) - 4) + cc_str[-4:]

    def _mask_ssn(self, value: Any) -> Any:
        if value is None or not value:
            return value
        ssn_str = re.sub(r"\D", "", str(value))
        if len(ssn_str) != 9:
            return "*" * len(ssn_str)
        return "***-**-" + ssn_str[-4:]

    # Partial masking
    def _partial_mask(self, value: Any, chars_to_show: int) -> Any:
        if value is None or not value:
            return value
        val_str = str(value)
        if len(val_str) <= chars_to_show * 2:
            return "*" * len(val_str)
        return (
            val_str[:chars_to_show]
            + "*" * (len(val_str) - chars_to_show * 2)
            + val_str[-chars_to_show:]
        )

    def _first_letter_mask(self, value: Any) -> Any:
        if value is None or not value:
            return value
        val_str = str(value)
        if len(val_str) <= 1:
            return val_str
        return val_str[0] + "*" * (len(val_str) - 1)

    # Random replacement
    def _random_replace(self, value: Any) -> Any:
        if value is None:
            return value

        if isinstance(value, (int, float)):
            # Generate random number in similar range
            if isinstance(value, int):
                magnitude = len(str(abs(value)))
                return random.randint(10 ** (magnitude - 1), 10**magnitude - 1)
            else:
                return random.uniform(0, abs(value) * 2)
        elif isinstance(value, str):
            # Generate random string of same length
            return "".join(
                random.choices(string.ascii_letters + string.digits, k=len(value))
            )
        else:
            return str(value)

    # Tokenization
    def _tokenize_uuid(self, value: Any) -> Optional[str]:
        if value is None:
            return None
        val_str = str(value)
        if val_str not in self.token_cache:
            self.token_cache[val_str] = str(uuid.uuid4())
        return str(self.token_cache[val_str])

    def _tokenize_sequential(self, value: Any) -> Optional[int]:
        if value is None:
            return None
        val_str = str(value)
        if val_str not in self.token_cache:
            self.sequential_counter += 1
            self.token_cache[val_str] = self.sequential_counter
        return int(self.token_cache[val_str])

    # Numeric masking
    def _round_number(self, value: Any, precision: int) -> Any:
        if value is None:
            return value
        try:
            num = float(value)
            return round(num / precision) * precision
        except (ValueError, TypeError):
            return value

    def _range_mask(self, value: Any, bucket_size: int) -> Any:
        if value is None:
            return value
        try:
            num = float(value)
            lower = int(num // bucket_size) * bucket_size
            upper = lower + bucket_size
            return f"{lower}-{upper}"
        except (ValueError, TypeError):
            return value

    def _add_noise(self, value: Any, noise_level: float) -> Any:
        if value is None:
            return value
        try:
            num = float(value)
            noise = random.uniform(-noise_level, noise_level) * abs(num)
            result = num + noise
            if isinstance(value, int):
                return int(result)
            return result
        except (ValueError, TypeError):
            return value

    # Date masking
    def _date_shift(self, value: Any, max_days: int) -> Any:
        if value is None:
            return value

        if isinstance(value, (date, datetime)):
            shift_days = random.randint(-max_days, max_days)
            return value + timedelta(days=shift_days)

        # Try to parse string dates
        try:
            from dateutil import parser  # type: ignore

            dt = parser.parse(str(value))
            shift_days = random.randint(-max_days, max_days)
            result = dt + timedelta(days=shift_days)
            if isinstance(value, str):
                return result.strftime("%Y-%m-%d")
            return result
        except Exception:
            return value

    def _year_only(self, value: Any) -> Any:
        if value is None:
            return value

        if isinstance(value, (date, datetime)):
            return value.year

        # Try to parse string dates
        try:
            from dateutil import parser

            dt = parser.parse(str(value))
            return dt.year
        except Exception:
            return value

    def _month_year(self, value: Any) -> Any:
        if value is None:
            return value

        if isinstance(value, (date, datetime)):
            return f"{value.year}-{value.month:02d}"

        # Try to parse string dates
        try:
            from dateutil import parser

            dt = parser.parse(str(value))
            return f"{dt.year}-{dt.month:02d}"
        except Exception:
            return value


def create_masking_mapper(mask_configs: list[str]) -> Callable:
    engine = MaskingEngine()

    # Parse all configurations
    masks = {}
    for config in mask_configs:
        column, algorithm, param = engine.parse_mask_config(config)
        masks[column] = engine.get_masking_function(algorithm, param)

    def apply_masks(data: Any) -> Any:
        # Handle PyArrow tables
        try:
            import pyarrow as pa  # type: ignore

            if isinstance(data, pa.Table):
                # Convert to pandas for easier manipulation
                df = data.to_pandas()

                # Apply masks to each column
                for column, mask_func in masks.items():
                    if column in df.columns:
                        df[column] = df[column].apply(mask_func)

                # Convert back to PyArrow table
                return pa.Table.from_pandas(df)
        except ImportError:
            pass

        # Handle dictionaries (original behavior)
        if isinstance(data, dict):
            for column, mask_func in masks.items():
                if column in data:
                    try:
                        data[column] = mask_func(data[column])
                    except Exception as e:
                        print(f"Warning: Failed to mask column {column}: {e}")
            return data

        # Return as-is if not a supported type
        return data

    return apply_masks
