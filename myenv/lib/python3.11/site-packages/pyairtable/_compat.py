from typing import TYPE_CHECKING

if TYPE_CHECKING:  # mypy really does not like this conditional import.
    import pydantic as pydantic
else:
    # Pydantic v2 broke a bunch of stuff. Luckily they provide a built-in v1.
    try:
        import pydantic.v1 as pydantic
    except ImportError:  # pragma: no cover
        import pydantic

__all__ = ["pydantic"]
