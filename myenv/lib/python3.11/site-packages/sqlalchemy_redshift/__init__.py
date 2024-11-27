from pkg_resources import DistributionNotFound, get_distribution, parse_version

for package in ['psycopg2', 'psycopg2-binary', 'psycopg2cffi']:
    try:
        if get_distribution(package).parsed_version < parse_version('2.5'):
            raise ImportError('Minimum required version for psycopg2 is 2.5')
        break
    except DistributionNotFound:
        pass

__version__ = get_distribution('sqlalchemy-redshift').version

from sqlalchemy.dialects import registry  # noqa

registry.register(
    "redshift", "sqlalchemy_redshift.dialect",
    "RedshiftDialect_psycopg2"
)
registry.register(
    "redshift.psycopg2", "sqlalchemy_redshift.dialect",
    "RedshiftDialect_psycopg2"
)
registry.register(
    'redshift+psycopg2cffi', 'sqlalchemy_redshift.dialect',
    'RedshiftDialect_psycopg2cffi',
)

registry.register(
    "redshift+redshift_connector", "sqlalchemy_redshift.dialect",
    "RedshiftDialect_redshift_connector"
)
