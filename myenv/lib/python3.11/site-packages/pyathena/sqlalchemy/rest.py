# -*- coding: utf-8 -*-
from pyathena.sqlalchemy.base import AthenaDialect


class AthenaRestDialect(AthenaDialect):
    driver = "rest"
    supports_statement_cache = True
