import unittest

from giturlparse import parse

# Test data
REWRITE_URLS = (
    # GitHub SSH
    ("git@github.com:Org/Repo.git", "ssh", "git@github.com:Org/Repo.git"),
    ("git@github.com:Org/Repo.git/", "ssh", "git@github.com:Org/Repo.git"),
    ("git@github.com/Org/Repo", "ssh", "git@github.com:Org/Repo.git"),
    ("git@github.com/Org/Repo/", "ssh", "git@github.com:Org/Repo.git"),
    ("git@github.com:Org/Repo.git", "https", "https://github.com/Org/Repo.git"),
    ("git@github.com:Org/Repo.git", "git", "git://github.com/Org/Repo.git"),
    # GitHub HTTPS
    ("https://github.com/Org/Repo.git", "ssh", "git@github.com:Org/Repo.git"),
    ("https://github.com/Org/Repo.git", "https", "https://github.com/Org/Repo.git"),
    ("https://github.com/Org/Repo.git", "git", "git://github.com/Org/Repo.git"),
    # GitHub GIT
    ("git://github.com/Org/Repo.git", "ssh", "git@github.com:Org/Repo.git"),
    ("git://github.com/Org/Repo.git", "https", "https://github.com/Org/Repo.git"),
    ("git://github.com/Org/Repo.git", "git", "git://github.com/Org/Repo.git"),
    (
        "git://github.com/Org/Repo/blob/master/dir/subdir/path",
        "git",
        "git://github.com/Org/Repo.git/blob/master/dir/subdir/path",
    ),
    # BitBucket SSH
    ("git@bitbucket.org:Org/Repo.git", "ssh", "git@bitbucket.org:Org/Repo.git"),
    ("git@bitbucket.org:Org/Repo.git", "https", "https://Org@bitbucket.org/Org/Repo.git"),
    # BitBucket HTTPS
    ("https://Org@bitbucket.org/Org/Repo.git", "ssh", "git@bitbucket.org:Org/Repo.git"),
    ("https://Org@bitbucket.org/Org/Repo.git", "https", "https://Org@bitbucket.org/Org/Repo.git"),
    # Assembla GIT
    ("git://git.assembla.com/SomeRepoID.git", "ssh", "git@git.assembla.com:SomeRepoID.git"),
    ("git://git.assembla.com/SomeRepoID.git", "git", "git://git.assembla.com/SomeRepoID.git"),
    # Assembla SSH
    ("git@git.assembla.com:SomeRepoID.git", "ssh", "git@git.assembla.com:SomeRepoID.git"),
    ("git@git.assembla.com:SomeRepoID.git", "git", "git://git.assembla.com/SomeRepoID.git"),
    # FriendCode HTTPS
    ("https://friendco.de/Aaron@user/test-repo.git", "https", "https://friendco.de/Aaron@user/test-repo.git"),
    # Generic
    ("git://git.buildroot.net/buildroot", "https", "https://git.buildroot.net/buildroot.git"),
    ("https://git.buildroot.net/buildroot", "git", "git://git.buildroot.net/buildroot.git"),
    ("https://git.buildroot.net/buildroot", "ssh", "git@git.buildroot.net:buildroot.git"),
    # Gitlab SSH
    ("git@host.org:Org/Repo.git", "ssh", "git@host.org:Org/Repo.git"),
    ("git@host.org:9999/Org/Repo.git", "ssh", "git@host.org:9999/Org/Repo.git"),
    ("git@host.org:Org/Repo.git", "https", "https://host.org/Org/Repo.git"),
    ("git@host.org:9999/Org/Repo.git", "https", "https://host.org/Org/Repo.git"),
    # Gitlab HTTPS
    ("https://host.org/Org/Repo.git", "ssh", "git@host.org:Org/Repo.git"),
    ("https://host.org/Org/Repo.git", "https", "https://host.org/Org/Repo.git"),
    ("https://host.org/Org/Group/Repo.git", "ssh", "git@host.org:Org/Group/Repo.git"),
    (
        "https://host.org/Org/Group/Repo/-/blob/master/file.py",
        "ssh",
        "git@host.org:Org/Group/Repo.git/-/blob/master/file.py",
    ),
    (
        "https://host.org/Org/Group/Repo/blob/master/file.py",
        "ssh",
        "git@host.org:Org/Group/Repo.git/blob/master/file.py",
    ),
)

INVALID_PARSE_URLS = (
    ("SSH Bad Username", "gitx@github.com:Org/Repo.git"),
    ("SSH No Repo", "git@github.com:Org"),
    ("HTTPS No Repo", "https://github.com/Org"),
    ("GIT No Repo", "git://github.com/Org"),
)


class UrlRewriteTestCase(unittest.TestCase):
    def _test_rewrite(self, source, protocol, expected):
        parsed = parse(source)
        self.assertTrue(parsed.valid, "Invalid Url: %s" % source)
        return self.assertEqual(parse(source).format(protocol), expected)

    def test_rewrites(self):
        for data in REWRITE_URLS:
            self._test_rewrite(*data)


# Test Suite
suite = unittest.TestLoader().loadTestsFromTestCase(UrlRewriteTestCase)
