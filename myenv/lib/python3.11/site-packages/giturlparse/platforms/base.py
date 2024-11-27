import itertools
import re


class BasePlatform:
    FORMATS = {
        "https": r"https://%(domain)s/%(repo)s%(dot_git)s",
        "ssh": r"git@%(domain)s:%(repo)s%(dot_git)s%(path_raw)s",
        "git": r"git://%(domain)s/%(repo)s%(dot_git)s%(path_raw)s",
    }

    PATTERNS = {
        "ssh": r"(?P<_user>.+)@(?P<domain>[^/]+?):(?P<repo>.+)(?:(\.git)?(/)?)",
        "http": r"(?P<protocols>(?P<protocol>http))://(?P<domain>[^/]+?)/(?P<repo>.+)(?:(\.git)?(/)?)",
        "https": r"(?P<protocols>(?P<protocol>https))://(?P<domain>[^/]+?)/(?P<repo>.+)(?:(\.git)?(/)?)",
        "git": r"(?P<protocols>(?P<protocol>git))://(?P<domain>[^/]+?)/(?P<repo>.+)(?:(\.git)?(/)?)",
    }

    # None means it matches all domains
    DOMAINS = None
    SKIP_DOMAINS = None
    DEFAULTS = {}

    def __init__(self):
        # Precompile PATTERNS
        self.COMPILED_PATTERNS = {proto: re.compile(regex, re.IGNORECASE) for proto, regex in self.PATTERNS.items()}

        # Supported protocols
        self.PROTOCOLS = self.PATTERNS.keys()

        if self.__class__ == BasePlatform:
            sub = [subclass.SKIP_DOMAINS for subclass in self.__class__.__subclasses__() if subclass.SKIP_DOMAINS]
            if sub:
                self.SKIP_DOMAINS = list(itertools.chain.from_iterable(sub))

    @staticmethod
    def clean_data(data):
        data["path"] = ""
        data["branch"] = ""
        data["protocols"] = list(filter(lambda x: x, data.get("protocols", "").split("+")))
        data["pathname"] = data.get("pathname", "").strip(":").rstrip("/")
        return data
