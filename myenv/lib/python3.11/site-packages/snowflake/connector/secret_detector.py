#!/usr/bin/env python
#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

"""The secret detector detects sensitive information.

It masks secrets that might be leaked from two potential avenues
    1. Out of Band Telemetry
    2. Logging
"""
from __future__ import annotations

import logging
import os
import re

MIN_TOKEN_LEN = os.getenv("MIN_TOKEN_LEN", 32)
MIN_PWD_LEN = os.getenv("MIN_PWD_LEN", 8)


class SecretDetector(logging.Formatter):
    AWS_KEY_PATTERN = re.compile(
        r"(aws_key_id|aws_secret_key|access_key_id|secret_access_key)\s*=\s*'([^']+)'",
        flags=re.IGNORECASE,
    )
    AWS_TOKEN_PATTERN = re.compile(
        r'(accessToken|tempToken|keySecret)"\s*:\s*"([a-z0-9/+]{32,}={0,2})"',
        flags=re.IGNORECASE,
    )
    SAS_TOKEN_PATTERN = re.compile(
        r"(sig|signature|AWSAccessKeyId|password|passcode)=(?P<secret>[a-z0-9%/+]{16,})",
        flags=re.IGNORECASE,
    )
    PRIVATE_KEY_PATTERN = re.compile(
        r"-{3,}BEGIN [A-Z ]*PRIVATE KEY-{3,}\n([\s\S]*?)\n-{3,}END [A-Z ]*PRIVATE KEY-{3,}",
        flags=re.MULTILINE | re.IGNORECASE,
    )
    PRIVATE_KEY_DATA_PATTERN = re.compile(
        r'"privateKeyData": "([a-z0-9/+=\\n]{10,})"', flags=re.MULTILINE | re.IGNORECASE
    )
    CONNECTION_TOKEN_PATTERN = re.compile(
        r"(token|assertion content)" r"([\'\"\s:=]+)" r"([a-z0-9=/_\-\+\.]{8,})",
        flags=re.IGNORECASE,
    )

    PASSWORD_PATTERN = re.compile(
        r"(password"
        r"|pwd)"
        r"([\'\"\s:=]+)"
        r"([a-z0-9!\"#\$%&\\\'\(\)\*\+\,-\./:;<=>\?\@\[\]\^_`\{\|\}~]{1,})",
        flags=re.IGNORECASE,
    )

    @staticmethod
    def mask_connection_token(text: str) -> str:
        return SecretDetector.CONNECTION_TOKEN_PATTERN.sub(r"\1\2****", text)

    @staticmethod
    def mask_password(text: str) -> str:
        return SecretDetector.PASSWORD_PATTERN.sub(r"\1\2****", text)

    @staticmethod
    def mask_aws_keys(text: str) -> str:
        return SecretDetector.AWS_KEY_PATTERN.sub(r"\1='****'", text)

    @staticmethod
    def mask_sas_tokens(text: str) -> str:
        return SecretDetector.SAS_TOKEN_PATTERN.sub(r"\1=****", text)

    @staticmethod
    def mask_aws_tokens(text: str) -> str:
        return SecretDetector.AWS_TOKEN_PATTERN.sub(r'\1":"XXXX"', text)

    @staticmethod
    def mask_private_key(text: str) -> str:
        return SecretDetector.PRIVATE_KEY_PATTERN.sub(
            "-----BEGIN PRIVATE KEY-----\\\\nXXXX\\\\n-----END PRIVATE KEY-----", text
        )

    @staticmethod
    def mask_private_key_data(text: str) -> str:
        return SecretDetector.PRIVATE_KEY_DATA_PATTERN.sub(
            '"privateKeyData": "XXXX"', text
        )

    @staticmethod
    def mask_secrets(text: str) -> tuple[bool, str, str | None]:
        """Masks any secrets. This is the method that should be used by outside classes.

        Args:
            text: A string which may contain a secret.

        Returns:
            The masked string.
        """
        if text is None:
            return (False, None, None)

        masked = False
        err_str = None
        try:
            masked_text = SecretDetector.mask_connection_token(
                SecretDetector.mask_password(
                    SecretDetector.mask_private_key_data(
                        SecretDetector.mask_private_key(
                            SecretDetector.mask_aws_tokens(
                                SecretDetector.mask_sas_tokens(
                                    SecretDetector.mask_aws_keys(text)
                                )
                            )
                        )
                    )
                )
            )
            if masked_text != text:
                masked = True
        except Exception as ex:
            # We'll assume that the exception was raised during masking
            # to be safe consider that the log has sensitive information
            # and do not raise an exception.
            masked = True
            masked_text = str(ex)
            err_str = str(ex)

        return masked, masked_text, err_str

    def format(self, record: logging.LogRecord) -> str:
        """Wrapper around logging module's formatter.

        This will ensure that the formatted message is free from sensitive credentials.

        Args:
            record: The logging record.

        Returns:
            Formatted desensitized log string.
        """
        try:
            unsanitized_log = super().format(record)
            masked, sanitized_log, err_str = SecretDetector.mask_secrets(
                unsanitized_log
            )
            if masked and err_str is not None:
                sanitized_log = "{} - {} {} - {} - {} - {}".format(
                    record.asctime,
                    record.threadName,
                    "secret_detector.py",
                    "sanitize_log_str",
                    record.levelname,
                    err_str,
                )
        except Exception as ex:
            sanitized_log = "{} - {} {} - {} - {} - {}".format(
                record.asctime,
                record.threadName,
                "secret_detector.py",
                "sanitize_log_str",
                record.levelname,
                "EXCEPTION - " + str(ex),
            )
        return sanitized_log
