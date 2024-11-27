"""
Compatibility module for projects referencing sqlalchemy_redshift
by its old name "redshift_sqlalchemy".
"""

import sys
import warnings

import sqlalchemy_redshift


DEPRECATION_MESSAGE = """\
redshift_sqlalchemy has been renamed to sqlalchemy_redshift.

The redshift_sqlalchemy compatibility package will be removed in
a future release, so it is recommended to update all package references.
"""

warnings.warn(DEPRECATION_MESSAGE, DeprecationWarning)

# All references to module redshift_sqlalchemy will map to sqlalchemy_redshift
sys.modules['redshift_sqlalchemy'] = sqlalchemy_redshift
