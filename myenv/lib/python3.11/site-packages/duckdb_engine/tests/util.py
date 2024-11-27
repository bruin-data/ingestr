import sqlalchemy
from packaging.version import Version
from pytest import mark

is_sqlalchemy_1 = Version(sqlalchemy.__version__).major == 1
sqlalchemy_1_only = mark.skipif(
    not is_sqlalchemy_1,
    reason="Pandas doesn't yet support sqlalchemy 2+",
)
