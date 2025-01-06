class MissingValueError(Exception):
    def __init__(self, value, source):
        super().__init__(f"{value} is required to connect to {source}")