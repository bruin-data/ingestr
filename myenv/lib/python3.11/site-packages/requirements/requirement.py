# encoding: utf-8

# This file is part of requirements-parser library.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0

from __future__ import unicode_literals

import re
from typing import Any, Dict, List, Match, Optional, Tuple, cast

from packaging.requirements import Requirement as Req

from .fragment import get_hash_info, parse_extras_require, parse_fragment
from .vcs import VCS, VCS_SCHEMES

URI_REGEX = re.compile(
    r'^(?P<scheme>https?|file|ftps?)://(?P<path>[^#]+)'
    r'(#(?P<fragment>\S+))?'
)

VCS_OPTIONAL_NAME_REGEX = r'(?:(?P<name>[\w\[\]_\-,]+)\s*@)?\s*'
VCS_SCHEMES_REGEX = r'|'.join([scheme.replace('+', r'\+') for scheme in VCS_SCHEMES])
VCS_REGEX = re.compile(
    rf'^{VCS_OPTIONAL_NAME_REGEX}(?P<scheme>{VCS_SCHEMES_REGEX})://((?P<login>[^/@]+)@)?'
    r'(?P<path>[^#@]+)(@(?P<revision>[^#]+))?(#(?P<fragment>\S+))?'
)

# This matches just about everything
LOCAL_REGEX = re.compile(r'^((?P<scheme>file)://)?(?P<path>[^#]+)#(?P<fragment>\S+)?')


class Requirement:
    """
    Represents a single requirement

    Typically instances of this class are created with ``Requirement.parse``.
    For local file requirements, there's no verification that the file
    exists. This class attempts to be *dict-like*.

    See: http://www.pip-installer.org/en/latest/logic.html

    **Members**:

    * ``line`` - the actual requirement line being parsed
    * ``editable`` - a boolean whether this requirement is "editable"
    * ``local_file`` - a boolean whether this requirement is a local file/path
    * ``specifier`` - a boolean whether this requirement used a requirement
      specifier (eg. "django>=1.5" or "requirements")
    * ``vcs`` - a string specifying the version control system
    * ``revision`` - a version control system specifier
    * ``name`` - the name of the requirement
    * ``uri`` - the URI if this requirement was specified by URI
    * ``subdirectory`` - the subdirectory fragment of the URI
    * ``path`` - the local path to the requirement
    * ``hash_name`` - the type of hashing algorithm indicated in the line
    * ``hash`` - the hash value indicated by the requirement line
    * ``extras`` - a list of extras for this requirement
      (eg. "mymodule[extra1, extra2]")
    * ``specs`` - a list of specs for this requirement
      (eg. "mymodule>1.5,<1.6" => [('>', '1.5'), ('<', '1.6')])
    """

    def __init__(self, line: str) -> None:
        # Do not call this private method
        self.line = line
        self.editable = False
        self.local_file = False
        self.specifier = False
        self.vcs = None
        self.name = None
        self.subdirectory = None
        self.uri = None
        self.path = None
        self.revision = None
        self.hash_name = None
        self.hash = None
        self.extras: List[str] = []
        self.specs: List[Tuple[str, str]] = []

    def __repr__(self) -> str:
        return f'<Requirement: "{self.line}">'

    def __getitem__(self, key: str) -> Any:
        return getattr(self, key)

    def __eq__(self, other: object) -> bool:
        if isinstance(other, Requirement):
            return all([
                self.name == other.name,
                set(self.specs) == set(other.specs),
                self.editable == other.editable,
                self.specifier == other.specifier,
                self.revision == other.revision,
                self.hash_name == other.hash_name,
                self.hash == other.hash,
                set(self.extras) == set(other.extras),
            ])
        return False

    def __ne__(self, other: object) -> bool:
        return not self == other

    def keys(self) -> Any:
        return self.__dict__.keys()

    @classmethod
    def parse_editable(cls, line: str) -> 'Requirement':
        """
        Parses a Requirement from an "editable" requirement which is either
        a local project path or a VCS project URI.

        See: pip/req.py:from_editable()

        :param line: an "editable" requirement
        :returns: a Requirement instance for the given line
        :raises: ValueError on an invalid requirement
        """

        req = cls(f'-e {line}')
        req.editable = True

        if ' #' in line:
            line = line[:line.find(' #')]

        vcs_match: Optional[Match[str]] = VCS_REGEX.match(line)
        local_match: Optional[Match[str]] = LOCAL_REGEX.match(line)

        if vcs_match is not None:
            groups: Dict[str, str] = vcs_match.groupdict()
            if groups.get('login'):
                req.uri = f'{groups["scheme"]}://{groups["login"]}@{groups["path"]}'  # type: ignore
            else:
                req.uri = f'{groups["scheme"]}://{groups["path"]}'  # type: ignore
            req.revision = groups['revision']  # type: ignore
            if groups['fragment']:
                fragment = parse_fragment(groups['fragment'])
                egg = cast(str, fragment.get('egg'))
                req.name, req.extras = parse_extras_require(egg)  # type: ignore
                req.hash_name, req.hash = get_hash_info(fragment)  # type: ignore
                req.subdirectory = fragment.get('subdirectory')  # type: ignore
            if groups['name']:
                req.name, req.extras = parse_extras_require(groups['name'])  # type: ignore
            for vcs in VCS:
                if str(req.uri).startswith(vcs):
                    req.vcs = vcs  # type: ignore
        elif local_match is not None:
            groups = local_match.groupdict()
            req.local_file = True
            if groups['fragment']:
                fragment = parse_fragment(groups['fragment'])
                egg = cast(str, fragment.get('egg'))
                req.name, req.extras = parse_extras_require(egg)  # type: ignore
                req.hash_name, req.hash = get_hash_info(fragment)  # type: ignore
                req.subdirectory = fragment.get('subdirectory')  # type: ignore
            req.path = cast(str, groups['path'])  # type: ignore
        else:
            req.local_file = True
            req.path, _, req.name = line.rpartition('/')  # type: ignore

        return req

    @classmethod
    def parse_line(cls, line: str) -> 'Requirement':
        """
        Parses a Requirement from a non-editable requirement.

        See: pip/req.py:from_line()

        :param line: a "non-editable" requirement
        :returns: a Requirement instance for the given line
        :raises: ValueError on an invalid requirement
        """

        req = cls(line)

        vcs_match: Optional[Match[str]] = VCS_REGEX.match(line)
        uri_match: Optional[Match[str]] = URI_REGEX.match(line)
        local_match: Optional[Match[str]] = LOCAL_REGEX.match(line)

        if vcs_match is not None:
            groups = vcs_match.groupdict()
            if groups.get('login'):
                req.uri = f'{groups["scheme"]}://{groups["login"]}@{groups["path"]}'  # type: ignore
            else:
                req.uri = f'{groups["scheme"]}://{groups["path"]}'  # type: ignore
            req.revision = groups['revision']  # type: ignore
            if groups['fragment']:
                fragment = parse_fragment(groups['fragment'])
                egg = fragment.get('egg')
                req.name, req.extras = parse_extras_require(egg)  # type: ignore
                req.hash_name, req.hash = get_hash_info(fragment)  # type: ignore
                req.subdirectory = fragment.get('subdirectory')  # type: ignore
            for vcs in VCS:
                if str(req.uri).startswith(vcs):
                    req.vcs = vcs  # type: ignore
            if groups['name']:
                req.name, req.extras = parse_extras_require(groups['name'])  # type: ignore
        elif uri_match is not None:
            groups = uri_match.groupdict()
            req.uri = f'{groups["scheme"]}://{groups["path"]}'  # type: ignore
            if groups['fragment']:
                fragment = parse_fragment(groups['fragment'])
                egg = fragment.get('egg')
                req.name, req.extras = parse_extras_require(egg)  # type: ignore
                req.hash_name, req.hash = get_hash_info(fragment)  # type: ignore
                req.subdirectory = fragment.get('subdirectory')  # type: ignore
            if groups['scheme'] == 'file':
                req.local_file = True
        elif '#egg=' in line:
            # Assume a local file match
            assert local_match is not None, 'This should match everything'
            groups = local_match.groupdict()
            req.local_file = True
            if groups['fragment']:
                fragment = parse_fragment(groups['fragment'])
                egg = fragment.get('egg')
                name, extras = parse_extras_require(egg)
                req.name = fragment.get('egg')  # type: ignore
                req.hash_name, req.hash = get_hash_info(fragment)  # type: ignore
                req.subdirectory = fragment.get('subdirectory')  # type: ignore
            req.path = groups['path']  # type: ignore
        else:
            # This is a requirement specifier.
            # Delegate to packaging.requirements and hope for the best
            req.specifier = True
            line_without_comment = re.sub('#.*', '', line)
            pkg_req = Req(line_without_comment)
            req.name = pkg_req.name  # type: ignore
            req.extras = [x.lower() for x in list(pkg_req.extras)]
            # Convert packaging.specifiers.SpecifierSet into
            # pkg_resources specs, i.e., a list of (op,version) tuples
            specs = []
            for specifier in pkg_req.specifier:
                spec = re.split('([=<>~!]+)', str(specifier), maxsplit=1)
                spec = list(filter(None, spec))
                specs.append(tuple(spec))
            req.specs = specs  # type: ignore
        return req

    @classmethod
    def parse(cls, line: str) -> 'Requirement':
        """
        Parses a Requirement from a line of a requirement file.

        :param line: a line of a requirement file
        :returns: a Requirement instance for the given line
        :raises: ValueError on an invalid requirement
        """

        if line.startswith('-e') or line.startswith('--editable'):
            # Editable installs are either a local project path
            # or a VCS project URI
            return cls.parse_editable(
                re.sub(r'^(-e|--editable=?)\s*', '', line))

        if ' --hash=' in line:
            line = line[:line.find(' --hash=')]
        if ' \\' in line:
            line = line[:line.find(' \\')]

        return cls.parse_line(line)
