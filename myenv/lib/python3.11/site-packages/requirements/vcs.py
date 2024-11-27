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

VCS = [
    'git',
    'hg',
    'svn',
    'bzr',
]

VCS_SCHEMES = [
    'git',
    'git+https',
    'git+ssh',
    'git+git',
    'hg+http',
    'hg+https',
    'hg+static-http',
    'hg+ssh',
    'svn',
    'svn+svn',
    'svn+http',
    'svn+https',
    'svn+ssh',
    'bzr+http',
    'bzr+https',
    'bzr+ssh',
    'bzr+sftp',
    'bzr+ftp',
    'bzr+lp',
]
