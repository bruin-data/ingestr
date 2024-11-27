import enum


DEFAULT_MIN_LEN = 1
INVALID_CHAR_ERR_MSG_TMPL = "invalids=({invalid}), value={value}"


_NTFS_RESERVED_FILE_NAMES = (
    "$Mft",
    "$MftMirr",
    "$LogFile",
    "$Volume",
    "$AttrDef",
    "$Bitmap",
    "$Boot",
    "$BadClus",
    "$Secure",
    "$Upcase",
    "$Extend",
    "$Quota",
    "$ObjId",
    "$Reparse",
)  # Only in root directory


@enum.unique
class Platform(enum.Enum):
    """
    Platform specifier enumeration.
    """

    #: POSIX compatible platform.
    POSIX = "POSIX"

    #: platform independent. note that absolute paths cannot specify this.
    UNIVERSAL = "universal"

    LINUX = "Linux"
    WINDOWS = "Windows"
    MACOS = "macOS"
