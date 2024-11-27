# -*- coding: utf-8 -*-
from pyathena.sqlalchemy.base import AthenaDialect
from pyathena.util import strtobool


class AthenaArrowDialect(AthenaDialect):
    driver = "arrow"
    supports_statement_cache = True

    def create_connect_args(self, url):
        from pyathena.arrow.cursor import ArrowCursor

        opts = super()._create_connect_args(url)
        opts.update({"cursor_class": ArrowCursor})
        cursor_kwargs = dict()
        if "unload" in opts:
            cursor_kwargs.update({"unload": bool(strtobool(opts.pop("unload")))})
        if cursor_kwargs:
            opts.update({"cursor_kwargs": cursor_kwargs})
        return [[], opts]
