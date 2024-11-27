# https://packaging.python.org/guides/packaging-namespace-packages/#pkgutil-style-namespace-packages
# This file should only contain the following line. Otherwise other sub-packages databricks.* namespace
# may not be importable.
__path__ = __import__("pkgutil").extend_path(__path__, __name__)
