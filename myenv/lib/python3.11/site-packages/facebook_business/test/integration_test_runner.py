# Copyright 2014 Facebook, Inc.

# You are hereby granted a non-exclusive, worldwide, royalty-free license to
# use, copy, modify, and distribute this software in source code or binary
# form for use in connection with the web services and APIs provided by
# Facebook.

# As with any software that integrates with the Facebook platform, your use
# of this software is subject to the Facebook Developer Principles and
# Policies [http://developers.facebook.com/policy/]. This copyright notice
# shall be included in all copies or substantial portions of the software.

# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
# THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
# FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
# DEALINGS IN THE SOFTWARE.

'''
Integration test runner for the Python Facebook Business SDK.

Note: 
    New integration test should follow the file name convention using integration_ as prefix,
    for example, integration_adset.py.

How to run:
    python -m facebook_business.test.integration_test_runner
'''

import os, subprocess

DIRECTORY = os.path.dirname(os.path.abspath(__file__))
COMMEND_BASE = "python -m facebook_business.test."
# test will run under release folder
RELEASE_PATH = DIRECTORY + "/../../"

# suffix of file name in the Test folder, which should not be executed
UTILS = "utils"
RUNNER = "runner"
CONSTANT = "constant"

integration_tests = [
    filename.split(".")[0]
    for filename in os.listdir(DIRECTORY)
    if filename.endswith(".py")
    and filename.startswith("integration_")
    and UTILS not in filename
    and RUNNER not in filename
    and CONSTANT not in filename
]

failed = False
for test in integration_tests:
    cmd = COMMEND_BASE + test
    try:
        subprocess.check_output(
            cmd,
            cwd=os.chdir(RELEASE_PATH),
            shell=True,
        )
    except subprocess.CalledProcessError as e:
        failed = True
        continue

if failed:
    exit(1)
