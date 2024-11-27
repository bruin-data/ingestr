import re
from sqlalchemy.sql import compiler


class DatabricksIdentifierPreparer(compiler.IdentifierPreparer):
    # SparkSQL identifier specification:
    # ref: https://spark.apache.org/docs/latest/sql-ref-identifier.html

    legal_characters = re.compile(r"^[A-Z0-9_]+$", re.I)

    def __init__(self, dialect):
        super().__init__(dialect, initial_quote="`")


class DatabricksDDLCompiler(compiler.DDLCompiler):
    def post_create_table(self, table):
        return " USING DELTA"
