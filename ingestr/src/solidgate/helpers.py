import base64
import csv
import hashlib
import hmac
import json
import time
from io import StringIO

import pendulum

from ingestr.src.http_client import create_client


class SolidgateClient:
    def __init__(self, public_key, secret_key):
        self.base_url = "https://reports.solidgate.com/api/v1"
        self.public_key = public_key
        self.secret_key = secret_key
        self.client = create_client(retry_status_codes=[204])

    def fetch_data(
        self,
        path: str,
        date_from: pendulum.DateTime,
        date_to: pendulum.DateTime,
    ):
        request_payload = {
            "date_from": date_from.format("YYYY-MM-DD HH:mm:ss"),
            "date_to": date_to.format("YYYY-MM-DD HH:mm:ss"),
        }

        json_string = json.dumps(request_payload)
        signature = self.generateSignature(json_string)
        headers = {
            "merchant": self.public_key,
            "Signature": signature,
            "Content-Type": "application/json",
        }

        next_page_iterator = None
        url = f"{self.base_url}/{path}"

        while True:
            payload = request_payload.copy()
            if next_page_iterator:
                payload["page_iterator"] = next_page_iterator

            response = self.client.post(url, headers=headers, json=payload)
            response.raise_for_status()
            response_json = response.json()

            if path == "subscriptions":
                data = response_json["subscriptions"]
                for _, value in data.items():
                    if "updated_at" in value:
                        value["updated_at"] = pendulum.parse(value["updated_at"])
                    yield value

            else:
                data = response_json["orders"]
                for value in data:
                    if "updated_at" in value:
                        value["updated_at"] = pendulum.parse(value["updated_at"])
                    yield value

            next_page_iterator = response_json.get("metadata", {}).get(
                "next_page_iterator"
            )
            if not next_page_iterator or next_page_iterator == "None":
                break

    def fetch_financial_entry_data(
        self, date_from: pendulum.DateTime, date_to: pendulum.DateTime
    ):
        request_payload = {
            "date_from": date_from.format("YYYY-MM-DD HH:mm:ss"),
            "date_to": date_to.format("YYYY-MM-DD HH:mm:ss"),
        }

        json_string = json.dumps(request_payload)
        signature = self.generateSignature(json_string)
        headers = {
            "merchant": self.public_key,
            "Signature": signature,
            "Content-Type": "application/json",
        }

        url = f"{self.base_url}/finance/financial_entries"
        post_response = self.client.post(url, headers=headers, json=request_payload)
        post_response.raise_for_status()
        report_url = post_response.json().get("report_url")
        if not report_url:
            return f"Report URL not found in the response: {post_response.json()}", 400

        data = self.public_key + self.public_key
        hmac_hash = hmac.new(
            self.secret_key.encode("utf-8"), data.encode("utf-8"), hashlib.sha512
        ).digest()
        signature_get = base64.b64encode(hmac_hash.hex().encode("utf-8")).decode(
            "utf-8"
        )

        headers_get = {
            "merchant": self.public_key,
            "Signature": signature_get,
            "Content-Type": "application/json",
        }

        # Retry getting the report for up to 10 minutes (600 seconds) with 5-second intervals
        max_retries = 120  # 10 minutes / 5 seconds = 120 attempts
        retry_count = 0

        while retry_count < max_retries:
            get_response = self.client.get(report_url, headers=headers_get)

            if get_response.status_code == 200:
                try:
                    response_json = json.loads(get_response.content)
                    if "error" in response_json:
                        raise Exception(
                            f"API Error: {response_json['error']['messages']}"
                        )
                except json.JSONDecodeError:
                    try:
                        csv_data = get_response.content.decode("utf-8")
                        reader = csv.DictReader(StringIO(csv_data))
                        rows = []
                        for row in reader:
                            if row["created_at"]:
                                row["created_at"] = pendulum.parse(row["created_at"])
                            else:
                                row["created_at"] = None

                            row2 = {k: v for k, v in row.items() if v != ""}
                            rows.append(row2)

                        return rows
                    except Exception as e:
                        raise Exception(f"Error reading CSV: {e}")
            else:
                # Report might not be ready yet, wait and retry
                retry_count += 1
                if retry_count >= max_retries:
                    raise Exception(
                        f"Failed to get report after {max_retries} attempts. Status code: {get_response.status_code}"
                    )
                time.sleep(5)  # Wait 5 seconds before retrying

    def generateSignature(self, json_string):
        data = self.public_key + json_string + self.public_key
        hmac_hash = hmac.new(
            self.secret_key.encode("utf-8"), data.encode("utf-8"), hashlib.sha512
        ).digest()
        return base64.b64encode(hmac_hash.hex().encode("utf-8")).decode("utf-8")
