import typing

azure_headers: typing.Dict[str, str] = {
    "Content-Type": "application/x-www-form-urlencoded",
    "Accept": "application/json",
}
okta_headers: typing.Dict[str, str] = {
    "Accept": "application/json",
    "Content-Type": "application/json",
    "Cache-Control": "no-cache",
}
# order of preference when searching for attributes in SAML response
SAML_RESP_NAMESPACES: typing.List[str] = ["saml2:", "saml:", ""]
