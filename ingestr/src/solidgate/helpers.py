import base64
import hashlib
import hmac
import json

import pendulum

from ingestr.src.http_client import create_client


class SolidgateClient:
    def __init__(self, public_key, secret_key):
        self.base_url = "https://reports.solidgate.com/api/v1"
        self.public_key = public_key
        self.secret_key = secret_key
        self.client = create_client()

    def fetch_data(
        self,
        path: str,
        date_from: pendulum.DateTime,
        date_to: pendulum.DateTime,
    ):
        request_payload = {"date_from": date_from, "date_to": date_to}
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
            if "data" not in response_json:
                print(f"API Response: {response_json}")
                raise Exception(
                    "Solidgate API returned a response without the expected data"
                )
            data = response_json["items"]
            yield data

            next_page_iterator = response_json.get("metadata", {}).get(
                "next_page_iterator"
            )
            if not next_page_iterator or next_page_iterator == "None":
                break

    def generateSignature(self, json_string):
        data = self.public_key + json_string + self.public_key
        hmac_hash = hmac.new(
            self.secret_key.encode("utf-8"), data.encode("utf-8"), hashlib.sha512
        ).digest()
        return base64.b64encode(hmac_hash.hex().encode("utf-8")).decode("utf-8")
