import sqlalchemy as sa
from sqlalchemy.ext import compiler as sa_compiler
from sqlalchemy.schema import DDLElement


def _check_if_key_exists(key):
    return isinstance(key, sa.Column) or key


def get_table_attributes(preparer,
                         diststyle=None,
                         distkey=None,
                         sortkey=None,
                         interleaved_sortkey=None):
    """
    Parse the table attributes into an acceptable string for Redshift,
    checking for valid combinations of distribution options.

    Parameters
    ----------
    preparer: RedshiftIdentifierPreparer, required
        The preparer associated with the compiler, usually accessed through
        compiler.preparer.  You can use a RedshiftDDLCompiler instance to
        access it.
    diststyle: str, optional
        The diststle to use for the table attributes.  This must be one of:
        ("ALL", "EVEN", "KEY").  If unset, Redshift passes AUTO. If KEY is used
        a distkey argument must be provided.  Inversely, if a diststyle other
        than KEY is provided, a distkey argument cannot be provided.
    distkey: str or sqlalchemy.Column, optional
        The distribution key to use the for the table attributes.  This can be
        provided without any distsyle specified or with KEY diststyle
        specified.
    sortkey: str or sqlalchemy.Column (or iterable thereof), optional
        The (compound) sort key(s) to use for the table attributes. Mutually
        exclusive option from interleaved_sortkey.
    interleaved_sortkey: str or sqlalchemy.Column (or iterable), optional
        The (interleaved) sort key(s) to use for the table attributes. Mutually
        exclusive option from sortkey.

    Returns
    -------
    string
        the table_attributes to append to a DDLElement, normally when creating
        a table or materialized view.

    Raises
    ------
    sqlalchemy.exc.ArgumentError
        when an invalid diststyle is set,
        when incompatable (diststyle, distkey) pairs are used,
        when both sortkey and interleaved_sortkey is specified.
    """
    text = ""

    has_distkey = _check_if_key_exists(distkey)
    if diststyle:
        diststyle = diststyle.upper()
        if diststyle not in ('EVEN', 'KEY', 'ALL'):
            raise sa.exc.ArgumentError(
                u"diststyle {0} is invalid".format(diststyle)
            )
        if diststyle != 'KEY' and has_distkey:
            raise sa.exc.ArgumentError(
                u"DISTSTYLE EVEN/ALL is not compatible with a DISTKEY."
            )
        if diststyle == 'KEY' and not has_distkey:
            raise sa.exc.ArgumentError(
                u"DISTKEY specification is required for DISTSTYLE KEY"
            )
        text += " DISTSTYLE " + diststyle

    if has_distkey:
        if isinstance(distkey, sa.Column):
            distkey = distkey.name
        text += " DISTKEY ({0})".format(preparer.quote(distkey))

    has_sortkey = _check_if_key_exists(sortkey)
    has_interleaved = _check_if_key_exists(interleaved_sortkey)
    if has_sortkey and has_interleaved:
        raise sa.exc.ArgumentError(
            "Parameters sortkey and interleaved_sortkey are "
            "mutually exclusive; you may not specify both."
        )

    if has_sortkey or has_interleaved:
        keys = sortkey if has_sortkey else interleaved_sortkey
        if isinstance(keys, (str, sa.Column)):
            keys = [keys]
        keys = [key.name if isinstance(key, sa.Column) else key
                for key in keys]
        if has_interleaved:
            text += " INTERLEAVED"
        sortkey_string = ", ".join(preparer.quote(key)
                                   for key in keys)
        text += " SORTKEY ({0})".format(sortkey_string)
    return text


class CreateMaterializedView(DDLElement):
    """
    DDLElement to create a materialized view in Redshift where the distribution
    options can be set.
    SEE:
    docs.aws.amazon.com/redshift/latest/dg/materialized-view-create-sql-command

    This works for any selectable.  Consider the trivial example of this table:

    >>> import sqlalchemy as sa
    >>> from sqlalchemy_redshift.dialect import CreateMaterializedView
    >>> engine = sa.create_engine('redshift+psycopg2://example')
    >>> metadata = sa.MetaData()
    >>> user = sa.Table(
    ...     'user',
    ...     metadata,
    ...     sa.Column('id', sa.Integer, primary_key=True),
    ...     sa.Column('name', sa.String)
    ... )
    >>> selectable = sa.select([user.c.id, user.c.name], from_obj=user)
    >>> view = CreateMaterializedView(
    ...     'materialized_view_of_users',
    ...     selectable,
    ...     distkey='id',
    ...     sortkey='name'
    ... )
    >>> print(view.compile(engine))
    <BLANKLINE>
    CREATE MATERIALIZED VIEW materialized_view_of_users
    DISTKEY (id) SORTKEY (name)
    AS SELECT "user".id, "user".name
    FROM "user"
    <BLANKLINE>
    <BLANKLINE>

    The materialized view can take full advantage of Redshift's distributed
    architecture via distribution styles and sort keys.

    The CreateMaterializedView is a DDLElement, so it can be executed via any
    execute() command, be it from an Engine, Session, or Connection.
    """
    def __init__(self, name, selectable, backup=True, diststyle=None,
                 distkey=None, sortkey=None, interleaved_sortkey=None):
        """
        Parameters
        ----------
        name: str, required
            the name of the materialized view to be created.
        selectable: str, required
            the sqlalchemy selectable to be the base query for the view.
        diststyle: str, optional
            The diststle to use for the table attributes.  This must be one of:
            ("ALL", "EVEN", "KEY").  If unset, Redshift passes AUTO. If KEY is
            used, a distkey argument must be provided.  Inversely, if
            a diststyle other than KEY is provided, a distkey argument cannot
            be provided.
        distkey: str or sqlalchemy.Column, optional
            The distribution key to use the for the table attributes.  This can
            be provided without any distsyle specified or with KEY diststyle
            specified.
        sortkey: str or sqlalchemy.Column (or iterable thereof), optional
            The (compound) sort key(s) to use for the table attributes.
            Mutually exclusive option from interleaved_sortkey.
        interleaved_sortkey: str or sqlalchemy.Column (or iterable), optional
            The (interleaved) sort key(s) to use for the table attributes.
            Mutually exclusive option from sortkey.
        """
        self.name = name
        self.selectable = selectable
        self.backup = backup
        self.diststyle = diststyle
        self.distkey = distkey
        self.sortkey = sortkey
        self.interleaved_sortkey = interleaved_sortkey


@sa_compiler.compiles(CreateMaterializedView)
def compile_create_materialized_view(element, compiler, **kw):
    """
    Returns the sql query that creates the materialized view
    """

    text = """\
        CREATE MATERIALIZED VIEW {name}
        {backup}
        {table_attributes}
        AS {selectable}\
    """
    table_attributes = get_table_attributes(
        compiler.preparer,
        diststyle=element.diststyle,
        distkey=element.distkey,
        sortkey=element.sortkey,
        interleaved_sortkey=element.interleaved_sortkey
    )
    # Defaults to yes, so omit default cas3
    backup = "" if element.backup else "BACKUP NO "
    selectable = compiler.sql_compiler.process(element.selectable,
                                               literal_binds=True)
    text = text.format(
        name=element.name,
        backup=backup,
        table_attributes=table_attributes,
        selectable=selectable
    )
    # Clean it up to have no leading spaces
    text = "\n".join([line.strip() for line in text.split("\n")
                      if line.strip()])
    return text


class DropMaterializedView(DDLElement):
    """
    Drop the materialized view from the database.
    SEE:
    docs.aws.amazon.com/redshift/latest/dg/materialized-view-drop-sql-command

    This undoes the create command, as expected:

    >>> import sqlalchemy as sa
    >>> from sqlalchemy_redshift.dialect import DropMaterializedView
    >>> engine = sa.create_engine('redshift+psycopg2://example')
    >>> drop = DropMaterializedView(
    ...     'materialized_view_of_users',
    ...     if_exists=True
    ... )
    >>> print(drop.compile(engine))
    <BLANKLINE>
    DROP MATERIALIZED VIEW IF EXISTS materialized_view_of_users
    <BLANKLINE>
    <BLANKLINE>

    This can be included in any execute() statement.
    """
    def __init__(self, name, if_exists=False, cascade=False):
        """
        Build the DropMaterializedView DDLElement.

        Parameters
        ----------
        name: str
            name of the materialized view to drop
        if_exists: bool, optional
            if True, the IF EXISTS clause is added. This will make the query
            successful even if the view does not exist, i.e. it lets you drop
            a non-existant view. Defaults to False.
        cascade: bool, optional
            if True, the CASCADE clause is added. This will drop all
            views/objects in the DB that depend on this materialized view.
            Defaults to False.
        """
        self.name = name
        self.if_exists = if_exists
        self.cascade = cascade


@sa_compiler.compiles(DropMaterializedView)
def compile_drop_materialized_view(element, compiler, **kw):
    """
    Formats and returns the drop statement for materialized views.
    """
    text = "DROP MATERIALIZED VIEW {if_exists}{name}{cascade}"
    if_exists = "IF EXISTS " if element.if_exists else ""
    cascade = " CASCADE" if element.cascade else ""
    return text.format(if_exists=if_exists, name=element.name, cascade=cascade)
