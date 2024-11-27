import enum
import numbers
import re
import warnings
try:
    from collections.abc import Iterable
except ImportError:
    from collections import Iterable

import sqlalchemy as sa
from sqlalchemy import exc as sa_exc
from sqlalchemy.ext import compiler as sa_compiler
from sqlalchemy.sql import expression as sa_expression


# At the time of this implementation, no specification for a session token was
# found. After looking at a few session tokens they appear to be the same as
# the aws_secret_access_key pattern, but much longer. An example token can be
# found here:
#   https://docs.aws.amazon.com/STS/latest/APIReference/API_GetSessionToken.html
# The regexs for access keys can be found here:
#   https://blogs.aws.amazon.com/security/blog/tag/key+rotation
# The pattern of IAM role ARNs can be found here:
#   http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html#arn-syntax-iam

ACCESS_KEY_ID_RE = re.compile('[A-Z0-9]{20}')
SECRET_ACCESS_KEY_RE = re.compile('[A-Za-z0-9/+=]{40}')
TOKEN_RE = re.compile('[A-Za-z0-9/+=]+')
AWS_PARTITIONS = frozenset({'aws', 'aws-cn', 'aws-us-gov'})
AWS_ACCOUNT_ID_RE = re.compile('[0-9]{12}')
IAM_ROLE_NAME_RE = re.compile('[A-Za-z0-9+=,.@\-_]{1,64}')  # noqa
IAM_ROLE_ARN_RE = re.compile('arn:(aws|aws-cn|aws-us-gov):iam::'
                             '[0-9]{12}:role/[A-Za-z0-9+=,.@\-_]{1,64}')  # noqa


def _process_aws_credentials(access_key_id=None, secret_access_key=None,
                             session_token=None, aws_partition='aws',
                             aws_account_id=None, iam_role_name=None,
                             iam_role_arns=None):
    uses_iam_role = aws_account_id is not None and iam_role_name is not None
    uses_iam_roles = iam_role_arns is not None
    uses_key = access_key_id is not None and secret_access_key is not None

    if uses_iam_role + uses_iam_roles + uses_key > 1:
        raise TypeError(
            'Either access key based credentials or role based credentials '
            'should be specified, but not both'
        )

    credentials = None

    if aws_account_id is not None and iam_role_name is not None:
        if aws_partition not in AWS_PARTITIONS:
            raise ValueError('invalid AWS partition')
        if not AWS_ACCOUNT_ID_RE.match(aws_account_id):
            raise ValueError(
                'invalid AWS account ID; does not match {pattern}'.format(
                    pattern=AWS_ACCOUNT_ID_RE.pattern,
                )
            )
        elif not IAM_ROLE_NAME_RE.match(iam_role_name):
            raise ValueError(
                'invalid IAM role name; does not match {pattern}'.format(
                    pattern=IAM_ROLE_NAME_RE.pattern,
                )
            )

        credentials = 'aws_iam_role=arn:{0}:iam::{1}:role/{2}'.format(
            aws_partition,
            aws_account_id,
            iam_role_name,
        )

    if iam_role_arns is not None:
        if isinstance(iam_role_arns, str):
            iam_role_arns = [iam_role_arns]
        if not isinstance(iam_role_arns, list):
            raise ValueError('iam_role_arns must be a list')
        for arn in iam_role_arns:
            if not IAM_ROLE_ARN_RE.match(arn):
                raise ValueError(
                    'invalid AWS account ID; does not match {pattern}'.format(
                        pattern=IAM_ROLE_ARN_RE.pattern,
                    )
                )

        credentials = 'aws_iam_role=' + ','.join(iam_role_arns)

    if access_key_id is not None and secret_access_key is not None:
        if not ACCESS_KEY_ID_RE.match(access_key_id):
            raise ValueError(
                'invalid access_key_id; does not match {pattern}'.format(
                    pattern=ACCESS_KEY_ID_RE.pattern,
                )
            )
        if not SECRET_ACCESS_KEY_RE.match(secret_access_key):
            raise ValueError(
                'invalid secret_access_key; does not match {pattern}'.format(
                    pattern=SECRET_ACCESS_KEY_RE.pattern,
                )
            )

        credentials = 'aws_access_key_id={0};aws_secret_access_key={1}'.format(
            access_key_id,
            secret_access_key,
        )

        if session_token is not None:
            if not TOKEN_RE.match(session_token):
                raise ValueError(
                    'invalid session_token; does not match {pattern}'.format(
                        pattern=TOKEN_RE.pattern,
                    )
                )
            credentials += ';token={0}'.format(session_token)

    if credentials is None:
        raise TypeError(
            'Either access key based credentials or role based credentials '
            'should be specified'
        )

    return credentials


def _process_fixed_width(spec):
    return ','.join(('{0}:{1:d}'.format(col, width) for col, width in spec))


class _ExecutableClause(sa_expression.Executable,
                        sa_expression.ClauseElement):
    pass


class AlterTableAppendCommand(_ExecutableClause):
    """
    Prepares an `ALTER TABLE APPEND` statement to efficiently move data from
    one table to another, much faster than an INSERT INTO ... SELECT.

    CAUTION: This moves the underlying storage blocks from the source table to
    the target table, so the source table will be *empty* after this command
    finishes.

    See the documentation for additional restrictions and other information:
    https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_TABLE_APPEND.html

    Parameters
    ----------

    source: sqlalchemy.Table
        The table to move data from. Must be an existing permanent table.
    target: sqlalchemy.Table
        The table to move data into. Must be an existing permanent table.
    ignore_extra: bool, optional
        If the source table includes columns not present in the target table,
        discard those columns. Mutually exclusive with `fill_target`.
    fill_target: bool, optional
        If the target table includes columns not present in the source table,
        fill those columns with the default column value or NULL. Mutually
        exclusive with `ignore_extra`.
    """
    def __init__(self, source, target, ignore_extra=False, fill_target=False):
        if ignore_extra and fill_target:
            raise ValueError(
                '"ignore_extra" cannot be used with "fill_target".')

        self.source = source
        self.target = target
        self.ignore_extra = ignore_extra
        self.fill_target = fill_target


@sa_compiler.compiles(AlterTableAppendCommand)
def visit_alter_table_append_command(element, compiler, **kw):
    """
    Returns the actual SQL query for the AlterTableAppendCommand class.
    """
    if element.ignore_extra:
        fill_option = 'IGNOREEXTRA'
    elif element.fill_target:
        fill_option = 'FILLTARGET'
    else:
        fill_option = ''

    query_text = \
        'ALTER TABLE {target} APPEND FROM {source} {fill_option}'.format(
            target=compiler.preparer.format_table(element.target),
            source=compiler.preparer.format_table(element.source),
            fill_option=fill_option,
        )
    return compiler.process(sa.text(query_text), **kw)


class UnloadFromSelect(_ExecutableClause):
    """
    Prepares a Redshift unload statement to drop a query to Amazon S3
    https://docs.aws.amazon.com/redshift/latest/dg/r_UNLOAD_command_examples.html

    Parameters
    ----------
    select: sqlalchemy.sql.selectable.Selectable
        The selectable Core Table Expression query to unload from.
    unload_location: str
        The Amazon S3 location where the file will be created, or a manifest
        file if the `manifest` option is used
    access_key_id: str, optional
        Access Key. Required unless you supply role-based credentials
        (``aws_account_id`` and ``iam_role_name`` or ``iam_role_arns``)
    secret_access_key: str, optional
        Secret Access Key ID. Required unless you supply role-based credentials
        (``aws_account_id`` and ``iam_role_name`` or ``iam_role_arns``)
    session_token : str, optional
    iam_role_arns : str or list of strings, optional
        Either a single arn or a list of arns of roles to assume when unloading
        Required unless you supply key based credentials (``access_key_id`` and
        ``secret_access_key``) or (``aws_account_id`` and ``iam_role_name``)
        separately.
    aws_partition: str, optional
        AWS partition to use with role-based credentials. Defaults to
        ``'aws'``. Not applicable when using key based credentials
        (``access_key_id`` and ``secret_access_key``) or role arns
        (``iam_role_arns``) directly.
    aws_account_id: str, optional
        AWS account ID for role-based credentials. Required unless you supply
        key based credentials (``access_key_id`` and ``secret_access_key``)
        or role arns (``iam_role_arns``) directly.
    iam_role_name: str, optional
        IAM role name for role-based credentials. Required unless you supply
        key based credentials (``access_key_id`` and ``secret_access_key``)
        or role arns (``iam_role_arns``) directly.
    manifest: bool, optional
        Boolean value denoting whether data_location is a manifest file.
    delimiter: File delimiter, optional
        defaults to '|'
    fixed_width: iterable of (str, int), optional
        List of (column name, length) pairs to control fixed-width output.
    encrypted: bool, optional
        Write to encrypted S3 key.
    gzip: bool, optional
        Create file using GZIP compression.
    add_quotes: bool, optional
        Quote fields so that fields containing the delimiter can be
        distinguished.
    null: str, optional
        Write null values as the given string. Defaults to ''.
    escape: bool, optional
        For CHAR and VARCHAR columns in delimited unload files, an escape
        character (``\\``) is placed before every occurrence of the following
        characters: ``\\r``, ``\\n``, ``\\``, the specified delimiter string.
        If `add_quotes` is specified, ``"`` and ``'`` are also escaped.
    allow_overwrite: bool, optional
        Overwrite the key at unload_location in the S3 bucket.
    parallel: bool, optional
        If disabled unload sequentially as one file.
    header: bool, optional
        Boolean value denoting whether to add header line
        containing column names at the top of each output file.
        Text transformation options, such as delimiter, add_quotes,
        and escape, also apply to the header line.
        `header` can't be used with fixed_width.
    region: str, optional
        The AWS region where the target S3 bucket is located, if the Redshift
        cluster isn't in the same region as the S3 bucket.
    max_file_size: int, optional
        Maximum size (in bytes) of files to create in S3. This must be between
        5 * 1024**2 and 6.24 * 1024**3. Note that Redshift appears to round
        to the nearest KiB.
    format : Format, optional
        Indicates the type of file to unload to.
    """

    def __init__(self, select, unload_location, access_key_id=None,
                 secret_access_key=None, session_token=None,
                 aws_partition='aws', aws_account_id=None, iam_role_name=None,
                 manifest=False, delimiter=None, fixed_width=None,
                 encrypted=False, gzip=False, add_quotes=False, null=None,
                 escape=False, allow_overwrite=False, parallel=True,
                 header=False, region=None, max_file_size=None,
                 format=None, iam_role_arns=None):

        if delimiter is not None and len(delimiter) != 1:
            raise ValueError(
                '"delimiter" parameter must be a single character'
            )

        if header and fixed_width is not None:
            raise ValueError(
                "'header' cannot be used with 'fixed_width'"
            )

        credentials = _process_aws_credentials(
            access_key_id=access_key_id,
            secret_access_key=secret_access_key,
            session_token=session_token,
            aws_partition=aws_partition,
            aws_account_id=aws_account_id,
            iam_role_name=iam_role_name,
            iam_role_arns=iam_role_arns,
        )

        self.select = select
        self.unload_location = unload_location
        self.credentials = credentials
        self.manifest = manifest
        self.header = header
        self.format = _check_enum(Format, format)
        self.delimiter = delimiter
        self.fixed_width = fixed_width
        self.encrypted = encrypted
        self.gzip = gzip
        self.add_quotes = add_quotes
        self.null = null
        self.escape = escape
        self.allow_overwrite = allow_overwrite
        self.parallel = parallel
        self.region = region
        self.max_file_size = max_file_size


@sa_compiler.compiles(UnloadFromSelect)
def visit_unload_from_select(element, compiler, **kw):
    """Returns the actual sql query for the UnloadFromSelect class."""

    template = """
       UNLOAD (:select) TO :unload_location
       CREDENTIALS :credentials
       {manifest}
       {header}
       {format}
       {delimiter}
       {encrypted}
       {fixed_width}
       {gzip}
       {add_quotes}
       {null}
       {escape}
       {allow_overwrite}
       {parallel}
       {region}
       {max_file_size}
    """
    el = element

    if el.format is None:
        format_ = ''
    elif el.format == Format.csv:
        format_ = 'FORMAT AS {}'.format(el.format.value)
        if el.delimiter is not None or el.fixed_width is not None:
            raise ValueError(
                'CSV format cannot be used with delimiter or fixed_width')
    elif el.format == Format.parquet:
        format_ = 'FORMAT AS {}'.format(el.format.value)
        if any((
            el.delimiter, el.fixed_width, el.add_quotes, el.escape, el.null,
            el.header, el.gzip
        )):
            raise ValueError(
                "Parquet format can't be used with `delimiter`, `fixed_width`,"
                ' `add_quotes`, `escape`, `null`, `header`, or `gzip`.'
            )
    else:
        raise ValueError(
            'Only CSV and Parquet formats are currently supported.'
        )

    qs = template.format(
        manifest='MANIFEST' if el.manifest else '',
        header='HEADER' if el.header else '',
        format=format_,
        delimiter=(
            'DELIMITER AS :delimiter' if el.delimiter is not None else ''
        ),
        encrypted='ENCRYPTED' if el.encrypted else '',
        fixed_width='FIXEDWIDTH AS :fixed_width' if el.fixed_width else '',
        gzip='GZIP' if el.gzip else '',
        add_quotes='ADDQUOTES' if el.add_quotes else '',
        escape='ESCAPE' if el.escape else '',
        null='NULL AS :null_as' if el.null is not None else '',
        allow_overwrite='ALLOWOVERWRITE' if el.allow_overwrite else '',
        parallel='PARALLEL OFF' if not el.parallel else '',
        region='REGION :region' if el.region is not None else '',
        max_file_size=(
            'MAXFILESIZE :max_file_size MB'
            if el.max_file_size is not None else ''
        ),
    )

    query = sa.text(qs)

    if el.delimiter is not None:
        query = query.bindparams(sa.bindparam(
            'delimiter', value=element.delimiter, type_=sa.String,
        ))

    if el.fixed_width:
        query = query.bindparams(sa.bindparam(
            'fixed_width',
            value=_process_fixed_width(el.fixed_width),
            type_=sa.String,
        ))

    if el.null is not None:
        query = query.bindparams(sa.bindparam(
            'null_as', value=el.null, type_=sa.String
        ))

    if el.region is not None:
        query = query.bindparams(sa.bindparam(
            'region', value=el.region, type_=sa.String
        ))

    if el.max_file_size is not None:
        max_file_size_mib = float(el.max_file_size) / 1024 / 1024
        query = query.bindparams(sa.bindparam(
            'max_file_size', value=max_file_size_mib, type_=sa.Float
        ))

    return compiler.process(
        query.bindparams(
            sa.bindparam('credentials', value=el.credentials, type_=sa.String),
            sa.bindparam(
                'unload_location', value=el.unload_location, type_=sa.String,
            ),
            sa.bindparam(
                'select',
                value=compiler.process(
                    el.select,
                    literal_binds=True,
                ),
                type_=sa.String,
            ),
        ),
        **kw
    )


class Format(enum.Enum):
    csv = 'CSV'
    json = 'JSON'
    avro = 'AVRO'
    orc = 'ORC'
    parquet = 'PARQUET'
    fixed_width = 'FIXEDWIDTH'


class Compression(enum.Enum):
    gzip = 'GZIP'
    lzop = 'LZOP'
    bzip2 = 'BZIP2'


class Encoding(enum.Enum):
    utf8 = 'UTF8'
    utf16 = 'UTF16'
    utf16le = 'UTF16LE'
    utf16be = 'UTF16BE'


def _check_enum(Enum, val):
    if val is None:
        return

    cleaned = Enum(val)
    if cleaned is not val:
        tpl = '{val!r} should be, {cleaned!r}, an instance of {Enum!r}'
        msg = tpl.format(val=val, cleaned=cleaned, Enum=Enum)
        warnings.warn(msg, DeprecationWarning)

    return cleaned


class CopyCommand(_ExecutableClause):
    """
    Prepares a Redshift COPY statement.

    Parameters
    ----------
    to : sqlalchemy.Table or iterable of sqlalchemy.ColumnElement
        The table or columns to copy data into
    data_location : str
        The Amazon S3 location from where to copy, or a manifest file if
        the `manifest` option is used
    access_key_id: str, optional
        Access Key. Required unless you supply role-based credentials
        (``aws_account_id`` and ``iam_role_name`` or ``iam_role_arns``)
    secret_access_key: str, optional
        Secret Access Key ID. Required unless you supply role-based credentials
        (``aws_account_id`` and ``iam_role_name`` or ``iam_role_arns``)
    session_token : str, optional
    iam_role_arns : str or list of strings, optional
        Either a single arn or a list of arns of roles to assume when unloading
        Required unless you supply key based credentials (``access_key_id`` and
        ``secret_access_key``) or (``aws_account_id`` and ``iam_role_name``)
        separately.
    aws_partition: str, optional
        AWS partition to use with role-based credentials. Defaults to
        ``'aws'``. Not applicable when using key based credentials
        (``access_key_id`` and ``secret_access_key``) or role arns
        (``iam_role_arns``) directly.
    aws_account_id: str, optional
        AWS account ID for role-based credentials. Required unless you supply
        key based credentials (``access_key_id`` and ``secret_access_key``)
         or role arns (``iam_role_arns``) directly.
    iam_role_name: str, optional
        IAM role name for role-based credentials. Required unless you supply
        key based credentials (``access_key_id`` and ``secret_access_key``)
        or role arns (``iam_role_arns``) directly.
    format : Format, optional
        Indicates the type of file to copy from
    quote : str, optional
        Specifies the character to be used as the quote character when using
        ``format=Format.csv``. The default is a double quotation mark ( ``"`` )
    delimiter : Field delimiter, optional
        defaults to ``|``
    path_file : str, optional
        Specifies an Amazon S3 location to a JSONPaths file to explicitly map
        Avro or JSON data elements to columns.
        defaults to ``'auto'``
    fixed_width: iterable of (str, int), optional
        List of (column name, length) pairs to control fixed-width output.
    compression : Compression, optional
        indicates the type of compression of the file to copy
    accept_any_date : bool, optional
        Allows any date format, including invalid formats such as
        ``00/00/00 00:00:00``, to be loaded as NULL without generating an error
        defaults to False
    accept_inv_chars : str, optional
        Enables loading of data into VARCHAR columns even if the data contains
        invalid UTF-8 characters. When specified each invalid UTF-8 byte is
        replaced by the specified replacement character
    blanks_as_null : bool, optional
        Boolean value denoting whether to load VARCHAR fields with whitespace
        only values as NULL instead of whitespace
    date_format : str, optional
        Specified the date format. If you want Amazon Redshift to automatically
        recognize and convert the date format in your source data, specify
        ``'auto'``
    empty_as_null : bool, optional
        Boolean value denoting whether to load VARCHAR fields with empty
        values as NULL instead of empty string
    encoding : Encoding, optional
        Specifies the encoding type of the load data defaults to
        ``Encoding.utf8``
    escape : bool, optional
        When this parameter is specified, the backslash character (``\\``) in
        input data is treated as an escape character. The character that
        immediately follows the backslash character is loaded into the table
        as part of the current column value, even if it is a character that
        normally serves a special purpose
    explicit_ids : bool, optional
        Override the autogenerated IDENTITY column values with explicit values
        from the source data files for the tables
    fill_record : bool, optional
        Allows data files to be loaded when contiguous columns are missing at
        the end of some of the records. The missing columns are filled with
        either zero-length strings or NULLs, as appropriate for the data types
        of the columns in question.
    ignore_blank_lines : bool, optional
        Ignores blank lines that only contain a line feed in a data file and
        does not try to load them
    ignore_header : int, optional
        Integer value of number of lines to skip at the start of each file
    dangerous_null_delimiter : str, optional
        Optional string value denoting what to interpret as a NULL value from
        the file. Note that this parameter *is not properly quoted* due to a
        difference between redshift's and postgres's COPY commands
        interpretation of strings. For example, null bytes must be passed to
        redshift's ``NULL`` verbatim as ``'\\0'`` whereas postgres's ``NULL``
        accepts ``'\\x00'``.
    remove_quotes : bool, optional
        Removes surrounding quotation marks from strings in the incoming data.
        All characters within the quotation marks, including delimiters, are
        retained.
    roundec : bool, optional
        Rounds up numeric values when the scale of the input value is greater
        than the scale of the column
    time_format : str, optional
        Specified the date format. If you want Amazon Redshift to automatically
        recognize and convert the time format in your source data, specify
        ``'auto'``
    trim_blanks : bool, optional
        Removes the trailing white space characters from a VARCHAR string
    truncate_columns : bool, optional
        Truncates data in columns to the appropriate number of characters so
        that it fits the column specification
    comp_rows : int, optional
        Specifies the number of rows to be used as the sample size for
        compression analysis
    comp_update : bool, optional
        Controls whether compression encodings are automatically applied.
        If omitted or None, COPY applies automatic compression only if the
        target table is empty and all the table columns either have RAW
        encoding or no encoding.
        If True COPY applies automatic compression if the table is empty, even
        if the table columns already have encodings other than RAW.
        If False automatic compression is disabled
    max_error : int, optional
        If the load returns the ``max_error`` number of errors or greater, the
        load fails
        defaults to 100000
    no_load : bool, optional
        Checks the validity of the data file without actually loading the data
    stat_update : bool, optional
        Update statistics automatically regardless of whether the table is
        initially empty
    manifest : bool, optional
        Boolean value denoting whether data_location is a manifest file.
    region: str, optional
        The AWS region where the target S3 bucket is located, if the Redshift
        cluster isn't in the same region as the S3 bucket.
    """

    def __init__(self, to, data_location, access_key_id=None,
                 secret_access_key=None, session_token=None,
                 aws_partition='aws', aws_account_id=None, iam_role_name=None,
                 format=None, quote=None,
                 path_file='auto', delimiter=None, fixed_width=None,
                 compression=None, accept_any_date=False,
                 accept_inv_chars=None, blanks_as_null=False, date_format=None,
                 empty_as_null=False, encoding=None, escape=False,
                 explicit_ids=False, fill_record=False,
                 ignore_blank_lines=False, ignore_header=None,
                 dangerous_null_delimiter=None, remove_quotes=False,
                 roundec=False, time_format=None, trim_blanks=False,
                 truncate_columns=False, comp_rows=None, comp_update=None,
                 max_error=None, no_load=False, stat_update=None,
                 manifest=False, region=None, iam_role_arns=None):

        credentials = _process_aws_credentials(
            access_key_id=access_key_id,
            secret_access_key=secret_access_key,
            session_token=session_token,
            aws_partition=aws_partition,
            aws_account_id=aws_account_id,
            iam_role_name=iam_role_name,
            iam_role_arns=iam_role_arns,
        )

        if delimiter is not None and len(delimiter) != 1:
            raise ValueError('"delimiter" parameter must be a single '
                             'character')

        if ignore_header is not None:
            if not isinstance(ignore_header, numbers.Integral):
                raise TypeError(
                    '"ignore_header" parameter should be an integer'
                )

        table = None
        columns = []
        if isinstance(to, Iterable):
            for column in to:
                if table is not None and table != column.table:
                    raise ValueError(
                        'All columns must come from the same table: '
                        '%s comes from %s not %s' % (
                            column, column.table, table
                        ),
                    )
                columns.append(column)
                table = column.table
        else:
            table = to

        self.table = table
        self.columns = columns
        self.data_location = data_location
        self.credentials = credentials
        self.format = _check_enum(Format, format)
        self.quote = quote
        self.path_file = path_file
        self.delimiter = delimiter
        self.fixed_width = fixed_width
        self.compression = _check_enum(Compression, compression)
        self.manifest = manifest
        self.accept_any_date = accept_any_date
        self.accept_inv_chars = accept_inv_chars
        self.blanks_as_null = blanks_as_null
        self.date_format = date_format
        self.empty_as_null = empty_as_null
        self.encoding = _check_enum(Encoding, encoding)
        self.escape = escape
        self.explicit_ids = explicit_ids
        self.fill_record = fill_record
        self.ignore_blank_lines = ignore_blank_lines
        self.ignore_header = ignore_header
        self.dangerous_null_delimiter = dangerous_null_delimiter
        self.remove_quotes = remove_quotes
        self.roundec = roundec
        self.time_format = time_format
        self.trim_blanks = trim_blanks
        self.truncate_columns = truncate_columns
        self.comp_rows = comp_rows
        self.comp_update = comp_update
        self.max_error = max_error
        self.no_load = no_load
        self.stat_update = stat_update
        self.region = region


@sa_compiler.compiles(CopyCommand)
def visit_copy_command(element, compiler, **kw):
    """
    Returns the actual sql query for the CopyCommand class.
    """
    qs = """COPY {table}{columns} FROM :data_location
        WITH CREDENTIALS AS :credentials
        {format}
        {parameters}"""
    parameters = []
    bindparams = [
        sa.bindparam(
            'data_location',
            value=element.data_location,
            type_=sa.String,
        ),
        sa.bindparam(
            'credentials',
            value=element.credentials,
            type_=sa.String,
        ),
    ]

    if element.format == Format.csv:
        format_ = 'FORMAT AS CSV'
        if element.quote is not None:
            format_ += ' QUOTE AS :quote_character'
            bindparams.append(sa.bindparam(
                'quote_character',
                value=element.quote,
                type_=sa.String,
            ))
    elif element.format == Format.json:
        format_ = 'FORMAT AS JSON AS :json_option'
        bindparams.append(sa.bindparam(
            'json_option',
            value=element.path_file,
            type_=sa.String,
        ))
    elif element.format == Format.avro:
        format_ = 'FORMAT AS AVRO AS :avro_option'
        bindparams.append(sa.bindparam(
            'avro_option',
            value=element.path_file,
            type_=sa.String,
        ))
    elif element.format == Format.orc:
        format_ = 'FORMAT AS ORC'
    elif element.format == Format.parquet:
        format_ = 'FORMAT AS PARQUET'
    elif element.format == Format.fixed_width and element.fixed_width is None:
        raise sa_exc.CompileError(
            "'fixed_width' argument required for format 'FIXEDWIDTH'.")
    else:
        format_ = ''

    if element.delimiter is not None:
        parameters.append('DELIMITER AS :delimiter_char')
        bindparams.append(sa.bindparam(
            'delimiter_char',
            value=element.delimiter,
            type_=sa.String,
        ))

    if element.fixed_width is not None:
        parameters.append('FIXEDWIDTH AS :fixedwidth_spec')
        bindparams.append(sa.bindparam(
            'fixedwidth_spec',
            value=_process_fixed_width(element.fixed_width),
            type_=sa.String,
        ))

    if element.compression is not None:
        parameters.append(Compression(element.compression).value)

    if element.manifest:
        parameters.append('MANIFEST')

    if element.accept_any_date:
        parameters.append('ACCEPTANYDATE')

    if element.accept_inv_chars is not None:
        parameters.append('ACCEPTINVCHARS AS :replacement_char')
        bindparams.append(sa.bindparam(
            'replacement_char',
            value=element.accept_inv_chars,
            type_=sa.String
        ))

    if element.blanks_as_null:
        parameters.append('BLANKSASNULL')

    if element.date_format is not None:
        parameters.append('DATEFORMAT AS :dateformat_string')
        bindparams.append(sa.bindparam(
            'dateformat_string',
            value=element.date_format,
            type_=sa.String,
        ))

    if element.empty_as_null:
        parameters.append('EMPTYASNULL')

    if element.encoding is not None:
        parameters.append('ENCODING AS ' + Encoding(element.encoding).value)

    if element.escape:
        parameters.append('ESCAPE')

    if element.explicit_ids:
        parameters.append('EXPLICIT_IDS')

    if element.fill_record:
        parameters.append('FILLRECORD')

    if element.ignore_blank_lines:
        parameters.append('IGNOREBLANKLINES')

    if element.ignore_header is not None:
        parameters.append('IGNOREHEADER AS :number_rows')
        bindparams.append(sa.bindparam(
            'number_rows',
            value=element.ignore_header,
            type_=sa.Integer,
        ))

    if element.dangerous_null_delimiter is not None:
        parameters.append("NULL AS '%s'" % element.dangerous_null_delimiter)

    if element.remove_quotes:
        parameters.append('REMOVEQUOTES')

    if element.roundec:
        parameters.append('ROUNDEC')

    if element.time_format is not None:
        parameters.append('TIMEFORMAT AS :timeformat_string')
        bindparams.append(sa.bindparam(
            'timeformat_string',
            value=element.time_format,
            type_=sa.String,
        ))

    if element.trim_blanks:
        parameters.append('TRIMBLANKS')

    if element.truncate_columns:
        parameters.append('TRUNCATECOLUMNS')

    if element.comp_rows:
        parameters.append('COMPROWS :numrows')
        bindparams.append(sa.bindparam(
            'numrows',
            value=element.comp_rows,
            type_=sa.Integer,
        ))

    if element.comp_update:
        parameters.append('COMPUPDATE ON')
    elif element.comp_update is not None:
        parameters.append('COMPUPDATE OFF')

    if element.max_error is not None:
        parameters.append('MAXERROR AS :error_count')
        bindparams.append(sa.bindparam(
            'error_count',
            value=element.max_error,
            type_=sa.Integer,
        ))

    if element.no_load:
        parameters.append('NOLOAD')

    if element.stat_update:
        parameters.append('STATUPDATE ON')
    elif element.stat_update is not None:
        parameters.append('STATUPDATE OFF')

    if element.region is not None:
        parameters.append('REGION :region')
        bindparams.append(sa.bindparam(
            'region',
            value=element.region,
            type_=sa.String
        ))

    columns = ' (%s)' % ', '.join(
        compiler.preparer.format_column(column) for column in element.columns
    ) if element.columns else ''

    qs = qs.format(
        table=compiler.preparer.format_table(element.table),
        columns=columns,
        format=format_,
        parameters='\n'.join(parameters)
    )

    return compiler.process(sa.text(qs).bindparams(*bindparams), **kw)


class CreateLibraryCommand(_ExecutableClause):
    """Prepares a Redshift CREATE LIBRARY statement.
    https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_LIBRARY.html

    Parameters
    ----------
    library_name: str, required
        The name of the library to install.
    location: str, required
        The location of the library file. Must be either a HTTP/HTTPS URL or an
        S3 location.
    access_key_id: str, optional
        Access Key. Required unless you supply role-based credentials
        (``aws_account_id`` and ``iam_role_name`` or ``iam_role_arns``)
    secret_access_key: str, optional
        Secret Access Key ID. Required unless you supply role-based credentials
        (``aws_account_id`` and ``iam_role_name`` or ``iam_role_arns``)
    session_token : str, optional
    iam_role_arns : str or list of strings, optional
        Either a single arn or a list of arns of roles to assume when unloading
        Required unless you supply key based credentials (``access_key_id`` and
        ``secret_access_key``) or (``aws_account_id`` and ``iam_role_name``)
        separately.
    aws_partition: str, optional
        AWS partition to use with role-based credentials. Defaults to
        ``'aws'``. Not applicable when using key based credentials
        (``access_key_id`` and ``secret_access_key``) or role arns
        (``iam_role_arns``) directly.
    aws_account_id: str, optional
        AWS account ID for role-based credentials. Required unless you supply
        key based credentials (``access_key_id`` and ``secret_access_key``)
         or role arns (``iam_role_arns``) directly.
    iam_role_name: str, optional
        IAM role name for role-based credentials. Required unless you supply
        key based credentials (``access_key_id`` and ``secret_access_key``)
        or role arns (``iam_role_arns``) directly.
    replace: bool, optional, default False
        Controls the presence of ``OR REPLACE`` in the compiled statement. See
        the command documentation for details.
    region: str, optional
        The AWS region where the library's S3 bucket is located, if the
        Redshift cluster isn't in the same region as the S3 bucket.
    """
    def __init__(self, library_name, location, access_key_id=None,
                 secret_access_key=None, session_token=None,
                 aws_account_id=None, iam_role_name=None, replace=False,
                 region=None, iam_role_arns=None):
        self.library_name = library_name
        self.location = location
        self.credentials = _process_aws_credentials(
            access_key_id=access_key_id,
            secret_access_key=secret_access_key,
            session_token=session_token,
            aws_account_id=aws_account_id,
            iam_role_name=iam_role_name,
            iam_role_arns=iam_role_arns,
        )
        self.replace = replace
        self.region = region


@sa_compiler.compiles(CreateLibraryCommand)
def visit_create_library_command(element, compiler, **kw):
    """
    Returns the actual sql query for the CreateLibraryCommand class.
    """
    query = """
        CREATE {or_replace} LIBRARY {name}
        LANGUAGE pythonplu
        FROM :location
        WITH CREDENTIALS AS :credentials
        {region}
    """
    bindparams = [
        sa.bindparam(
            'location',
            value=element.location,
            type_=sa.String,
        ),
        sa.bindparam(
            'credentials',
            value=element.credentials,
            type_=sa.String,
        ),
    ]

    if element.region is not None:
        bindparams.append(sa.bindparam(
            'region',
            value=element.region,
            type_=sa.String,
        ))

    quoted_lib_name = compiler.preparer.quote_identifier(element.library_name)
    query = query.format(name=quoted_lib_name,
                         or_replace='OR REPLACE' if element.replace else '',
                         region='REGION :region' if element.region else '')
    return compiler.process(sa.text(query).bindparams(*bindparams), **kw)


class RefreshMaterializedView(_ExecutableClause):
    """
    Prepares a Redshift REFRESH MATERIALIZED VIEW statement.
    SEE:
    docs.aws.amazon.com/redshift/latest/dg/materialized-view-refresh-sql-command

    This reruns the query underlying the view to ensure the materialized data
    is up to date.

    >>> import sqlalchemy as sa
    >>> from sqlalchemy_redshift.dialect import RefreshMaterializedView
    >>> engine = sa.create_engine('redshift+psycopg2://example')
    >>> refresh = RefreshMaterializedView('materialized_view_of_users')
    >>> print(refresh.compile(engine))
    <BLANKLINE>
    REFRESH MATERIALIZED VIEW materialized_view_of_users
    <BLANKLINE>
    <BLANKLINE>

    This can be included in any execute() statement.
    """
    def __init__(self, name):
        """
        Builds the Executable/ClauseElement that represents the refresh command

        Parameters
        ----------
        name: str, required
            The name of the view to refresh
        """
        self.name = name


@sa_compiler.compiles(RefreshMaterializedView)
def compile_refresh_materialized_view(element, compiler, **kw):
    """
    Formats and returns the refresh statement for materialized views.
    """
    text = "REFRESH MATERIALIZED VIEW {name}"
    return text.format(name=element.name)
