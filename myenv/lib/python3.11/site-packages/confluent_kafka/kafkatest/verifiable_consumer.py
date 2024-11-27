#!/usr/bin/env python
#
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
#

import argparse
import os
import time
from confluent_kafka import Consumer, KafkaError, KafkaException
from verifiable_client import VerifiableClient


class VerifiableConsumer(VerifiableClient):
    """
    confluent-kafka-python backed VerifiableConsumer class for use with
    Kafka's kafkatests client tests.
    """

    def __init__(self, conf):
        """
        conf is a config dict passed to confluent_kafka.Consumer()
        """
        super(VerifiableConsumer, self).__init__(conf)
        self.conf['on_commit'] = self.on_commit
        self.consumer = Consumer(**conf)
        self.consumed_msgs = 0
        self.consumed_msgs_last_reported = 0
        self.consumed_msgs_at_last_commit = 0
        self.use_auto_commit = False
        self.use_async_commit = False
        self.max_msgs = -1
        self.assignment = []
        self.assignment_dict = dict()

    def find_assignment(self, topic, partition):
        """ Find and return existing assignment based on topic and partition,
        or None on miss. """
        skey = '%s %d' % (topic, partition)
        return self.assignment_dict.get(skey)

    def send_records_consumed(self, immediate=False):
        """ Send records_consumed, every 100 messages, on timeout,
            or if immediate is set. """
        if self.consumed_msgs <= self.consumed_msgs_last_reported + (0 if immediate else 100):
            return

        if len(self.assignment) == 0:
            return

        d = {'name': 'records_consumed',
             'count': self.consumed_msgs - self.consumed_msgs_last_reported,
             'partitions': []}

        for a in self.assignment:
            if a.min_offset == -1:
                # Skip partitions that havent had any messages since last time.
                # This is to circumvent some minOffset checks in kafkatest.
                continue
            d['partitions'].append(a.to_dict())
            a.min_offset = -1

        self.send(d)
        self.consumed_msgs_last_reported = self.consumed_msgs

    def send_assignment(self, evtype, partitions):
        """ Send assignment update, evtype is either 'assigned' or 'revoked' """
        d = {'name': 'partitions_' + evtype,
             'partitions': [{'topic': x.topic, 'partition': x.partition} for x in partitions]}
        self.send(d)

    def on_assign(self, consumer, partitions):
        """ Rebalance on_assign callback """
        old_assignment = self.assignment
        self.assignment = [AssignedPartition(p.topic, p.partition) for p in partitions]
        # Move over our last seen offsets so that we can report a proper
        # minOffset even after a rebalance loop.
        for a in old_assignment:
            b = self.find_assignment(a.topic, a.partition)
            b.min_offset = a.min_offset

        self.assignment_dict = {a.skey: a for a in self.assignment}
        self.send_assignment('assigned', partitions)

    def on_revoke(self, consumer, partitions):
        """ Rebalance on_revoke callback """
        # Send final consumed records prior to rebalancing to make sure
        # latest consumed is in par with what is going to be committed.
        self.send_records_consumed(immediate=True)
        self.do_commit(immediate=True, asynchronous=False)
        self.assignment = list()
        self.assignment_dict = dict()
        self.send_assignment('revoked', partitions)

    def on_commit(self, err, partitions):
        """ Offsets Committed callback """
        if err is not None and err.code() == KafkaError._NO_OFFSET:
            self.dbg('on_commit(): no offsets to commit')
            return

        # Report consumed messages to make sure consumed position >= committed position
        self.send_records_consumed(immediate=True)

        d = {'name': 'offsets_committed',
             'offsets': []}

        if err is not None:
            d['success'] = False
            d['error'] = str(err)
        else:
            d['success'] = True
            d['error'] = ''

        for p in partitions:
            pd = {'topic': p.topic, 'partition': p.partition, 'offset': p.offset}
            if p.error is not None:
                pd['error'] = str(p.error)
            d['offsets'].append(pd)

        if len(self.assignment) == 0:
            self.dbg('Not sending offsets_committed: No current assignment: would be: %s' % d)
            return

        self.send(d)

    def do_commit(self, immediate=False, asynchronous=None):
        """ Commit every 1000 messages or whenever there is a consume timeout
            or immediate. """
        if (self.use_auto_commit
                or self.consumed_msgs_at_last_commit + (0 if immediate else 1000) >
                self.consumed_msgs):
            return

        # Make sure we report consumption before commit,
        # otherwise tests may fail because of commit > consumed
        if self.consumed_msgs_at_last_commit < self.consumed_msgs:
            self.send_records_consumed(immediate=True)

        if asynchronous is None:
            async_mode = self.use_async_commit
        else:
            async_mode = asynchronous

        self.dbg('Committing %d messages (Async=%s)' %
                 (self.consumed_msgs - self.consumed_msgs_at_last_commit,
                  async_mode))

        retries = 3
        while True:
            try:
                self.dbg('Commit')
                offsets = self.consumer.commit(asynchronous=async_mode)
                self.dbg('Commit done: offsets %s' % offsets)

                if not async_mode:
                    self.on_commit(None, offsets)

                break

            except KafkaException as e:
                if e.args[0].code() == KafkaError._NO_OFFSET:
                    self.dbg('No offsets to commit')
                    break
                elif e.args[0].code() in (KafkaError.REQUEST_TIMED_OUT,
                                          KafkaError.NOT_COORDINATOR,
                                          KafkaError._WAIT_COORD):
                    self.dbg('Commit failed: %s (%d retries)' % (str(e), retries))
                    if retries <= 0:
                        raise
                    retries -= 1
                    time.sleep(1)
                    continue
                else:
                    raise

        self.consumed_msgs_at_last_commit = self.consumed_msgs

    def msg_consume(self, msg):
        """ Handle consumed message (or error event) """
        if msg.error():
            self.err('Consume failed: %s' % msg.error(), term=False)
            return

        if self.verbose:
            self.send({'name': 'record_data',
                       'topic': msg.topic(),
                       'partition': msg.partition(),
                       'key': msg.key(),
                       'value': msg.value(),
                       'offset': msg.offset()})

        if self.max_msgs >= 0 and self.consumed_msgs >= self.max_msgs:
            return  # ignore extra messages

        # Find assignment.
        a = self.find_assignment(msg.topic(), msg.partition())
        if a is None:
            self.err('Received message on unassigned partition %s [%d] @ %d' %
                     (msg.topic(), msg.partition(), msg.offset()), term=True)

        a.consumed_msgs += 1
        if a.min_offset == -1:
            a.min_offset = msg.offset()
        if a.max_offset < msg.offset():
            a.max_offset = msg.offset()

        self.consumed_msgs += 1

        self.consumer.store_offsets(message=msg)
        self.send_records_consumed(immediate=False)
        self.do_commit(immediate=False)


class AssignedPartition(object):
    """ Local state container for assigned partition. """

    def __init__(self, topic, partition):
        super(AssignedPartition, self).__init__()
        self.topic = topic
        self.partition = partition
        self.skey = '%s %d' % (self.topic, self.partition)
        self.consumed_msgs = 0
        self.min_offset = -1
        self.max_offset = 0

    def to_dict(self):
        """ Return a dict of this partition's state """
        return {'topic': self.topic, 'partition': self.partition,
                'minOffset': self.min_offset, 'maxOffset': self.max_offset}


if __name__ == '__main__':

    parser = argparse.ArgumentParser(description='Verifiable Python Consumer')
    parser.add_argument('--topic', action='append', type=str, required=True)
    parser.add_argument('--group-id', dest='conf_group.id', required=True)
    parser.add_argument('--group-instance-id', dest='conf_group.instance.id')
    parser.add_argument('--broker-list', dest='conf_bootstrap.servers', required=True)
    parser.add_argument('--bootstrap-server', dest='conf_bootstrap.servers')
    parser.add_argument('--session-timeout', type=int, dest='conf_session.timeout.ms', default=6000)
    parser.add_argument('--enable-autocommit', action='store_true', dest='conf_enable.auto.commit', default=False)
    parser.add_argument('--max-messages', type=int, dest='max_messages', default=-1)
    parser.add_argument('--assignment-strategy', dest='conf_partition.assignment.strategy')
    parser.add_argument('--reset-policy', dest='topicconf_auto.offset.reset', default='earliest')
    parser.add_argument('--verbose', action='store_true', dest='verbose', default=False, help='Per-message stats')
    parser.add_argument('--consumer.config', dest='consumer_config')
    parser.add_argument('-X', nargs=1, dest='extra_conf', action='append', help='Configuration property', default=[])
    args = vars(parser.parse_args())

    conf = {'broker.version.fallback': '0.9.0',
            # Do explicit manual offset stores to avoid race conditions
            # where a message is consumed from librdkafka but not yet handled
            # by the Python code that keeps track of last consumed offset.
            'enable.auto.offset.store': False}

    if args.get('consumer_config', None) is not None:
        args.update(VerifiableClient.read_config_file(args['consumer_config']))

    args.update([x[0].split('=') for x in args.get('extra_conf', [])])

    VerifiableClient.set_config(conf, args)

    vc = VerifiableConsumer(conf)
    vc.use_auto_commit = args['conf_enable.auto.commit']
    vc.max_msgs = args['max_messages']
    vc.verbose = args['verbose']

    vc.dbg('Pid %d' % os.getpid())
    vc.dbg('Using config: %s' % conf)

    vc.dbg('Subscribing to %s' % args['topic'])
    vc.consumer.subscribe(args['topic'],
                          on_assign=vc.on_assign, on_revoke=vc.on_revoke)

    failed = False

    try:
        while vc.run:
            msg = vc.consumer.poll(timeout=1.0)
            if msg is None:
                # Timeout.
                # Try reporting consumed messages
                vc.send_records_consumed(immediate=True)
                # Commit every poll() timeout instead of on every message.
                # Also commit on every 1000 messages, whichever comes first.
                vc.do_commit(immediate=True)
                continue

            # Handle message (or error event)
            vc.msg_consume(msg)

    except KeyboardInterrupt:
        vc.dbg('KeyboardInterrupt')
        vc.run = False
        pass

    except Exception as e:
        vc.dbg('Terminating on exception: %s' % str(e))
        failed = True

    vc.dbg('Closing consumer')
    vc.send_records_consumed(immediate=True)

    if not failed:
        try:
            if not vc.use_auto_commit:
                vc.do_commit(immediate=True, asynchronous=False)
            vc.consumer.close()
        except Exception as e:
            vc.dbg('Ignoring exception while closing: %s' % str(e))
            failed = True

    vc.send({'name': 'shutdown_complete', 'failed': failed})

    vc.dbg('All done')
