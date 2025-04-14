from typing import Callable


def for_each(source):
    """
    Apply a function to each resource in a source.
    """
    if hasattr(source, "selected_resources") and source.selected_resources:
        resource_names = list(source.selected_resources.keys())
        for res in resource_names:
            ex(source.resources[res])  # type: ignore[union-attr]
    else:
        ex(source)  # type: ignore[arg-type]
