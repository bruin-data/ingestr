from .base import BasePlatform


class BitbucketPlatform(BasePlatform):
    PATTERNS = {
        "https": (
            r"(?P<protocols>(git\+)?(?P<protocol>https))://(?P<_user>.+)@(?P<domain>.+?)"
            r"(?P<pathname>/(?P<owner>.+)/(?P<repo>.+?)(?:\.git)?)$"
        ),
        "ssh": (
            r"(?P<protocols>(git\+)?(?P<protocol>ssh))?(://)?git@(?P<domain>.+?):"
            r"(?P<pathname>(?P<owner>.+)/(?P<repo>.+?)(?:\.git)?)$"
        ),
    }
    FORMATS = {
        "https": r"https://%(owner)s@%(domain)s/%(owner)s/%(repo)s%(dot_git)s",
        "ssh": r"git@%(domain)s:%(owner)s/%(repo)s%(dot_git)s",
    }
    DOMAINS = ("bitbucket.org",)
    DEFAULTS = {"_user": "git"}
