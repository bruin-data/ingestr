# -*- coding: utf-8 -*-
from __future__ import annotations

import copy
import logging
from datetime import datetime
from typing import Any, Dict, Iterator, MutableMapping, Optional

_logger = logging.getLogger(__name__)  # type: ignore

_API_FIELD_TO_S3_OBJECT_PROPERTY = {
    "ETag": "etag",
    "CacheControl": "cache_control",
    "ContentDisposition": "content_disposition",
    "ContentEncoding": "content_encoding",
    "ContentLanguage": "content_language",
    "ContentLength": "content_length",
    "ContentType": "content_type",
    "Expires": "expires",
    "WebsiteRedirectLocation": "website_redirect_location",
    "ServerSideEncryption": "server_side_encryption",
    "SSECustomerAlgorithm": "sse_customer_algorithm",
    "SSEKMSKeyId": "sse_kms_key_id",
    "BucketKeyEnabled": "bucket_key_enabled",
    "StorageClass": "storage_class",
    "ObjectLockMode": "object_lock_mode",
    "ObjectLockRetainUntilDate": "object_lock_retain_until_date",
    "ObjectLockLegalHoldStatus": "object_lock_legal_hold_status",
    "Metadata": "metadata",
    "LastModified": "last_modified",
}


class S3ObjectType:
    S3_OBJECT_TYPE_DIRECTORY: str = "directory"
    S3_OBJECT_TYPE_FILE: str = "file"


class S3StorageClass:
    S3_STORAGE_CLASS_STANDARD: str = "STANDARD"
    S3_STORAGE_CLASS_REDUCED_REDUNDANCY: str = "REDUCED_REDUNDANCY"
    S3_STORAGE_CLASS_STANDARD_IA: str = "STANDARD_IA"
    S3_STORAGE_CLASS_ONEZONE_IA: str = "ONEZONE_IA"
    S3_STORAGE_CLASS_INTELLIGENT_TIERING: str = "INTELLIGENT_TIERING"
    S3_STORAGE_CLASS_GLACIER: str = "GLACIER"
    S3_STORAGE_CLASS_DEEP_ARCHIVE: str = "DEEP_ARCHIVE"
    S3_STORAGE_CLASS_OUTPOSTS: str = "OUTPOSTS"
    S3_STORAGE_CLASS_GLACIER_IR: str = "GLACIER_IR"

    S3_STORAGE_CLASS_BUCKET: str = "BUCKET"
    S3_STORAGE_CLASS_DIRECTORY: str = "DIRECTORY"


class S3Object(MutableMapping[str, Any]):
    def __init__(
        self,
        init: Dict[str, Any],
        **kwargs,
    ) -> None:
        if init:
            filtered = {}
            for k, v in init.items():
                if k not in _API_FIELD_TO_S3_OBJECT_PROPERTY:
                    continue
                filtered[_API_FIELD_TO_S3_OBJECT_PROPERTY[k]] = v
            if "StorageClass" not in init:
                # https://docs.aws.amazon.com/AmazonS3/latest/API/API_HeadObject.html#API_HeadObject_ResponseSyntax
                # Amazon S3 returns this header for all objects except for
                # S3 Standard storage class objects.
                filtered[
                    _API_FIELD_TO_S3_OBJECT_PROPERTY["StorageClass"]
                ] = S3StorageClass.S3_STORAGE_CLASS_STANDARD
            super().update(filtered)
            if "Size" in init:
                self.content_length = init["Size"]
                self.size = init["Size"]
            elif "ContentLength" in init:
                self.size = init["ContentLength"]
            else:
                self.content_length = 0
                self.size = 0
        super().update({_API_FIELD_TO_S3_OBJECT_PROPERTY.get(k, k): v for k, v in kwargs.items()})
        if self.get("key") is None:
            self.name = self.get("bucket")
        else:
            self.name = f"{self.get('bucket')}/{self.get('key')}"

    def get(self, key: str, default: Any = None) -> Any:
        return super().get(key, default)

    def __getitem__(self, item: str) -> Any:
        return self.__dict__.get(item)

    def __getattr__(self, item: str):
        return self.get(item)

    def __setitem__(self, key: str, value: Any) -> None:
        self.__dict__[key] = value

    def __setattr__(self, attr: str, value: Any) -> None:
        self[attr] = value

    def __delitem__(self, key: str) -> None:
        del self.__dict__[key]

    def __iter__(self) -> Iterator[str]:
        return iter(self.__dict__.keys())

    def __len__(self) -> int:
        return len(self.__dict__)

    def __str__(self):
        return str(self.__dict__)

    def to_dict(self) -> Dict[str, Any]:
        return copy.deepcopy(self.__dict__)

    def to_api_repr(self) -> Dict[str, Any]:
        fields = {}
        for k, v in _API_FIELD_TO_S3_OBJECT_PROPERTY.items():
            if k in ["ETag", "ContentLength", "LastModified"]:
                # Excluded from API representation
                continue
            field = self.get(v)
            if field is not None:
                fields[k] = field
        return fields


class S3PutObject:
    def __init__(self, response: Dict[str, Any]) -> None:
        self._expiration: Optional[str] = response.get("Expiration")
        self._version_id: Optional[str] = response.get("VersionId")
        self._etag: Optional[str] = response.get("ETag")
        self._checksum_crc32: Optional[str] = response.get("ChecksumCRC32")
        self._checksum_crc32c: Optional[str] = response.get("ChecksumCRC32C")
        self._checksum_sha1: Optional[str] = response.get("ChecksumSHA1")
        self._checksum_sha256: Optional[str] = response.get("ChecksumSHA256")
        self._server_side_encryption = response.get("ServerSideEncryption")
        self._sse_customer_algorithm = response.get("SSECustomerAlgorithm")
        self._sse_customer_key_md5 = response.get("SSECustomerKeyMD5")
        self._sse_kms_key_id = response.get("SSEKMSKeyId")
        self._sse_kms_encryption_context = response.get("SSEKMSEncryptionContext")
        self._bucket_key_enabled = response.get("BucketKeyEnabled")
        self._request_charged = response.get("RequestCharged")

    @property
    def expiration(self) -> Optional[str]:
        return self._expiration

    @property
    def version_id(self) -> Optional[str]:
        return self._version_id

    @property
    def etag(self) -> Optional[str]:
        return self._etag

    @property
    def checksum_crc32(self) -> Optional[str]:
        return self._checksum_crc32

    @property
    def checksum_crc32c(self) -> Optional[str]:
        return self._checksum_crc32c

    @property
    def checksum_sha1(self) -> Optional[str]:
        return self._checksum_sha1

    @property
    def checksum_sha256(self) -> Optional[str]:
        return self._checksum_sha256

    @property
    def server_side_encryption(self) -> Optional[str]:
        return self._server_side_encryption

    @property
    def sse_customer_algorithm(self) -> Optional[str]:
        return self._sse_customer_algorithm

    @property
    def sse_customer_key_md5(self) -> Optional[str]:
        return self._sse_customer_key_md5

    @property
    def sse_kms_key_id(self) -> Optional[str]:
        return self._sse_kms_key_id

    @property
    def sse_kms_encryption_context(self) -> Optional[str]:
        return self._sse_kms_encryption_context

    @property
    def bucket_key_enabled(self) -> Optional[bool]:
        return self._bucket_key_enabled

    @property
    def request_charged(self) -> Optional[str]:
        return self._request_charged

    def to_dict(self) -> Dict[str, Any]:
        return copy.deepcopy(self.__dict__)


class S3MultipartUpload:
    def __init__(self, response: Dict[str, Any]) -> None:
        self._abort_date = response.get("AbortDate")
        self._abort_rule_id = response.get("AbortRuleId")
        self._bucket = response.get("Bucket")
        self._key = response.get("Key")
        self._upload_id = response.get("UploadId")
        self._server_side_encryption = response.get("ServerSideEncryption")
        self._sse_customer_algorithm = response.get("SSECustomerAlgorithm")
        self._sse_customer_key_md5 = response.get("SSECustomerKeyMD5")
        self._sse_kms_key_id = response.get("SSEKMSKeyId")
        self._sse_kms_encryption_context = response.get("SSEKMSEncryptionContext")
        self._bucket_key_enabled = response.get("BucketKeyEnabled")
        self._request_charged = response.get("RequestCharged")
        self._checksum_algorithm = response.get("ChecksumAlgorithm")

    @property
    def abort_date(self) -> Optional[datetime]:
        return self._abort_date

    @property
    def abort_rule_id(self) -> Optional[str]:
        return self._abort_rule_id

    @property
    def bucket(self) -> Optional[str]:
        return self._bucket

    @property
    def key(self) -> Optional[str]:
        return self._key

    @property
    def upload_id(self) -> Optional[str]:
        return self._upload_id

    @property
    def server_side_encryption(self) -> Optional[str]:
        return self._server_side_encryption

    @property
    def sse_customer_algorithm(self) -> Optional[str]:
        return self._sse_customer_algorithm

    @property
    def sse_customer_key_md5(self) -> Optional[str]:
        return self._sse_customer_key_md5

    @property
    def sse_kms_key_id(self) -> Optional[str]:
        return self._sse_kms_key_id

    @property
    def sse_kms_encryption_context(self) -> Optional[str]:
        return self._sse_kms_encryption_context

    @property
    def bucket_key_enabled(self) -> Optional[bool]:
        return self._bucket_key_enabled

    @property
    def request_charged(self) -> Optional[str]:
        return self._request_charged

    @property
    def checksum_algorithm(self) -> Optional[str]:
        return self._checksum_algorithm


class S3MultipartUploadPart:
    def __init__(self, part_number: int, response: Dict[str, Any]) -> None:
        self._part_number = part_number
        self._copy_source_version_id: Optional[str] = response.get("CopySourceVersionId")
        copy_part_result = response.get("CopyPartResult")
        if copy_part_result:
            self._last_modified: Optional[datetime] = copy_part_result.get("LastModified")
            self._etag: Optional[str] = copy_part_result.get("ETag")
            self._checksum_crc32: Optional[str] = copy_part_result.get("ChecksumCRC32")
            self._checksum_crc32c: Optional[str] = copy_part_result.get("ChecksumCRC32C")
            self._checksum_sha1: Optional[str] = copy_part_result.get("ChecksumSHA1")
            self._checksum_sha256: Optional[str] = copy_part_result.get("ChecksumSHA256")
        else:
            self._last_modified = None
            self._etag = response.get("ETag")
            self._checksum_crc32 = response.get("ChecksumCRC32")
            self._checksum_crc32c = response.get("ChecksumCRC32C")
            self._checksum_sha1 = response.get("ChecksumSHA1")
            self._checksum_sha256 = response.get("ChecksumSHA256")
        self._server_side_encryption: Optional[str] = response.get("ServerSideEncryption")
        self._sse_customer_algorithm: Optional[str] = response.get("SSECustomerAlgorithm")
        self._sse_customer_key_md5: Optional[str] = response.get("SSECustomerKeyMD5")
        self._sse_kms_key_id: Optional[str] = response.get("SSEKMSKeyId")
        self._bucket_key_enabled: Optional[bool] = response.get("BucketKeyEnabled")
        self._request_charged: Optional[str] = response.get("RequestCharged")

    @property
    def part_number(self) -> int:
        return self._part_number

    @property
    def copy_source_version_id(self) -> Optional[str]:
        return self._copy_source_version_id

    @property
    def last_modified(self) -> Optional[datetime]:
        return self._last_modified

    @property
    def etag(self) -> Optional[str]:
        return self._etag

    @property
    def checksum_crc32(self) -> Optional[str]:
        return self._checksum_crc32

    @property
    def checksum_crc32c(self) -> Optional[str]:
        return self._checksum_crc32c

    @property
    def checksum_sha1(self) -> Optional[str]:
        return self._checksum_sha1

    @property
    def checksum_sha256(self) -> Optional[str]:
        return self._checksum_sha256

    @property
    def server_side_encryption(self) -> Optional[str]:
        return self._server_side_encryption

    @property
    def sse_customer_algorithm(self) -> Optional[str]:
        return self._sse_customer_algorithm

    @property
    def sse_customer_key_md5(self) -> Optional[str]:
        return self._sse_customer_key_md5

    @property
    def sse_kms_key_id(self) -> Optional[str]:
        return self._sse_kms_key_id

    @property
    def bucket_key_enabled(self) -> Optional[bool]:
        return self._bucket_key_enabled

    @property
    def request_charged(self) -> Optional[str]:
        return self._request_charged

    def to_api_repr(self) -> Dict[str, Any]:
        return {
            "ETag": self.etag,
            "ChecksumCRC32": self.checksum_crc32,
            "ChecksumCRC32C": self.checksum_crc32c,
            "ChecksumSHA1": self.checksum_sha1,
            "ChecksumSHA256": self.checksum_sha256,
            "PartNumber": self.part_number,
        }


class S3CompleteMultipartUpload:
    def __init__(self, response: Dict[str, Any]) -> None:
        self._location: Optional[str] = response.get("Location")
        self._bucket: Optional[str] = response.get("Bucket")
        self._key: Optional[str] = response.get("Key")
        self._expiration: Optional[str] = response.get("Expiration")
        self._version_id: Optional[str] = response.get("VersionId")
        self._etag: Optional[str] = response.get("ETag")
        self._checksum_crc32: Optional[str] = response.get("ChecksumCRC32")
        self._checksum_crc32c: Optional[str] = response.get("ChecksumCRC32C")
        self._checksum_sha1: Optional[str] = response.get("ChecksumSHA1")
        self._checksum_sha256: Optional[str] = response.get("ChecksumSHA256")
        self._server_side_encryption = response.get("ServerSideEncryption")
        self._sse_kms_key_id = response.get("SSEKMSKeyId")
        self._bucket_key_enabled = response.get("BucketKeyEnabled")
        self._request_charged = response.get("RequestCharged")

    @property
    def location(self) -> Optional[str]:
        return self._location

    @property
    def bucket(self) -> Optional[str]:
        return self._bucket

    @property
    def key(self) -> Optional[str]:
        return self._key

    @property
    def expiration(self) -> Optional[str]:
        return self._expiration

    @property
    def version_id(self) -> Optional[str]:
        return self._version_id

    @property
    def etag(self) -> Optional[str]:
        return self._etag

    @property
    def checksum_crc32(self) -> Optional[str]:
        return self._checksum_crc32

    @property
    def checksum_crc32c(self) -> Optional[str]:
        return self._checksum_crc32c

    @property
    def checksum_sha1(self) -> Optional[str]:
        return self._checksum_sha1

    @property
    def checksum_sha256(self) -> Optional[str]:
        return self._checksum_sha256

    @property
    def server_side_encryption(self) -> Optional[str]:
        return self._server_side_encryption

    @property
    def sse_kms_key_id(self) -> Optional[str]:
        return self._sse_kms_key_id

    @property
    def bucket_key_enabled(self) -> Optional[bool]:
        return self._bucket_key_enabled

    @property
    def request_charged(self) -> Optional[str]:
        return self._request_charged

    def to_dict(self):
        return copy.deepcopy(self.__dict__)
