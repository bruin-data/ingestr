import logging
import json
from typing import Optional

logger = logging.getLogger(__name__)


class OAuthToken:
    def __init__(self, access_token, refresh_token):
        self._access_token = access_token
        self._refresh_token = refresh_token

    @property
    def access_token(self) -> str:
        return self._access_token

    @property
    def refresh_token(self) -> str:
        return self._refresh_token


class OAuthPersistence:
    def persist(self, hostname: str, oauth_token: OAuthToken):
        pass

    def read(self, hostname: str) -> Optional[OAuthToken]:
        pass


class OAuthPersistenceCache(OAuthPersistence):
    def __init__(self):
        self.tokens = {}

    def persist(self, hostname: str, oauth_token: OAuthToken):
        self.tokens[hostname] = oauth_token

    def read(self, hostname: str) -> Optional[OAuthToken]:
        return self.tokens.get(hostname)


# Note this is only intended to be used for development
class DevOnlyFilePersistence(OAuthPersistence):
    def __init__(self, file_path):
        self._file_path = file_path

    def persist(self, hostname: str, token: OAuthToken):
        logger.info(f"persisting token in {self._file_path}")

        # Data to be written
        dictionary = {
            "refresh_token": token.refresh_token,
            "access_token": token.access_token,
            "hostname": hostname,
        }

        # Serializing json
        json_object = json.dumps(dictionary, indent=4)

        with open(self._file_path, "w") as outfile:
            outfile.write(json_object)

    def read(self, hostname: str) -> Optional[OAuthToken]:
        try:
            with open(self._file_path, "r") as infile:
                json_as_string = infile.read()

                token_as_json = json.loads(json_as_string)
                hostname_in_token = token_as_json["hostname"]
                if hostname != hostname_in_token:
                    msg = (
                        f"token was persisted for host {hostname_in_token} does not match {hostname} "
                        f"This is a dev only persistence and it only supports a single Databricks hostname."
                        f"\n manually delete {self._file_path} file and restart this process"
                    )
                    logger.error(msg)
                    raise Exception(msg)
                return OAuthToken(
                    token_as_json["access_token"], token_as_json["refresh_token"]
                )
        except Exception as e:
            return None
