# Copyright 2016 Confluent Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


import datetime
import json
import os
import re
import signal
import socket
import sys
import time


class VerifiableClient(object):
    """
    Generic base class for a kafkatest verifiable client.
    Implements the common kafkatest protocol and semantics.
    """

    def __init__(self, conf):
        """
        """
        super(VerifiableClient, self).__init__()
        self.conf = conf
        self.conf['client.id'] = 'python@' + socket.gethostname()
        self.run = True
        signal.signal(signal.SIGTERM, self.sig_term)
        self.dbg('Pid is %d' % os.getpid())

    def sig_term(self, sig, frame):
        self.dbg('SIGTERM')
        self.run = False

    @staticmethod
    def _timestamp():
        return time.strftime('%H:%M:%S', time.localtime())

    def dbg(self, s):
        """ Debugging printout """
        sys.stderr.write('%% %s DEBUG: %s\n' % (self._timestamp(), s))

    def err(self, s, term=False):
        """ Error printout, if term=True the process will terminate immediately. """
        sys.stderr.write('%% %s ERROR: %s\n' % (self._timestamp(), s))
        if term:
            sys.stderr.write('%% FATAL ERROR ^\n')
            sys.exit(1)

    def send(self, d):
        """ Send dict as JSON to stdout for consumtion by kafkatest handler """
        d['_time'] = str(datetime.datetime.now())
        self.dbg('SEND: %s' % json.dumps(d))
        sys.stdout.write('%s\n' % json.dumps(d))
        sys.stdout.flush()

    @staticmethod
    def set_config(conf, args):
        """ Set client config properties using args dict. """
        for n, v in args.iteritems():
            if v is None:
                continue

            if n.startswith('topicconf_'):
                conf[n[10:]] = v
                continue

            if not n.startswith('conf_'):
                # App config, skip
                continue

            # Remove conf_ prefix
            n = n[5:]

            # Handle known Java properties to librdkafka properties.
            if n == 'partition.assignment.strategy':
                # Convert Java class name to config value.
                # "org.apache.kafka.clients.consumer.RangeAssignor" -> "range"
                v = re.sub(r'org.apache.kafka.clients.consumer.(\w+)Assignor',
                           lambda x: x.group(1).lower(), v)
                if v == 'sticky':
                    v = 'cooperative-sticky'

            conf[n] = v

    @staticmethod
    def read_config_file(path):
        """Read (java client) config file and return dict with properties"""
        conf = {}

        with open(path, 'r') as f:
            for line in f:
                line = line.strip()

                if line.startswith('#') or len(line) == 0:
                    continue

                fi = line.find('=')
                if fi < 1:
                    raise Exception('%s: invalid line, no key=value pair: %s' % (path, line))

                k = line[:fi]
                v = line[fi+1:]

                conf[k] = v

        return conf
