from json import dumps


class PGType:
    def __init__(self: "PGType", value) -> None:
        self.value: str = value

    def encode(self, encoding) -> bytes:
        return str(self.value).encode(encoding)


class PGEnum(PGType):
    def __init__(self: "PGEnum", value) -> None:
        if isinstance(value, str):
            self.value = value
        else:
            self.value = value.value


class PGJson(PGType):
    def encode(self: "PGJson", encoding: str) -> bytes:
        return dumps(self.value).encode(encoding)


class PGJsonb(PGType):
    def encode(self: "PGJsonb", encoding: str) -> bytes:
        return dumps(self.value).encode(encoding)


class PGTsvector(PGType):
    pass


class PGVarchar(str):
    pass


class PGText(str):
    pass
