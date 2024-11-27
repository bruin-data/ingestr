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
import time
from confluent_kafka import Producer, KafkaException
from verifiable_client import VerifiableClient


class VerifiableProducer(VerifiableClient):
    """
    confluent-kafka-python backed VerifiableProducer class for use with
    Kafka's kafkatests client tests.
    """

    def __init__(self, conf):
        """
        conf is a config dict passed to confluent_kafka.Producer()
        """
        super(VerifiableProducer, self).__init__(conf)
        self.conf['on_delivery'] = self.dr_cb
        self.producer = Producer(**self.conf)
        self.num_acked = 0
        self.num_sent = 0
        self.num_err = 0

    def dr_cb(self, err, msg):
        """ Per-message Delivery report callback. Called from poll() """
        if err:
            self.num_err += 1
            self.send({'name': 'producer_send_error',
                       'message': str(err),
                       'topic': msg.topic(),
                       'key': msg.key(),
                       'value': msg.value()})
        else:
            self.num_acked += 1
            self.send({'name': 'producer_send_success',
                       'topic': msg.topic(),
                       'partition': msg.partition(),
                       'offset': msg.offset(),
                       'key': msg.key(),
                       'value': msg.value()})

        pass


if __name__ == '__main__':

    parser = argparse.ArgumentParser(description='Verifiable Python Producer')
    parser.add_argument('--topic', type=str, required=True)
    parser.add_argument('--throughput', type=int, default=0)
    parser.add_argument('--broker-list', dest='conf_bootstrap.servers', required=True)
    parser.add_argument('--bootstrap-server', dest='conf_bootstrap.servers')
    parser.add_argument('--max-messages', type=int, dest='max_msgs', default=1000000)  # avoid infinite
    parser.add_argument('--value-prefix', dest='value_prefix', type=str, default=None)
    parser.add_argument('--acks', type=int, dest='topicconf_request.required.acks', default=-1)
    parser.add_argument('--message-create-time', type=int, dest='create_time', default=0)
    parser.add_argument('--repeating-keys', type=int, dest='repeating_keys', default=0)
    parser.add_argument('--producer.config', dest='producer_config')
    parser.add_argument('-X', nargs=1, dest='extra_conf', action='append', help='Configuration property', default=[])
    args = vars(parser.parse_args())

    conf = {'broker.version.fallback': '0.9.0',
            'produce.offset.report': True}

    if args.get('producer_config', None) is not None:
        args.update(VerifiableClient.read_config_file(args['producer_config']))

    args.update([x[0].split('=') for x in args.get('extra_conf', [])])

    VerifiableClient.set_config(conf, args)

    vp = VerifiableProducer(conf)

    vp.max_msgs = args['max_msgs']
    throughput = args['throughput']
    topic = args['topic']
    if args['value_prefix'] is not None:
        value_fmt = args['value_prefix'] + '.%d'
    else:
        value_fmt = '%d'

    repeating_keys = args['repeating_keys']
    key_counter = 0

    if throughput > 0:
        delay = 1.0/throughput
    else:
        delay = 0

    vp.dbg('Producing %d messages at a rate of %d/s' % (vp.max_msgs, throughput))

    try:
        for i in range(0, vp.max_msgs):
            if not vp.run:
                break

            t_end = time.time() + delay
            while vp.run:
                if repeating_keys != 0:
                    key = '%d' % key_counter
                    key_counter = (key_counter + 1) % repeating_keys
                else:
                    key = None

                try:
                    vp.producer.produce(topic, value=(value_fmt % i), key=key,
                                        timestamp=args.get('create_time', 0))
                    vp.num_sent += 1
                except KafkaException as e:
                    vp.err('produce() #%d/%d failed: %s' % (i, vp.max_msgs, str(e)))
                    vp.num_err += 1
                except BufferError:
                    vp.dbg('Local produce queue full (produced %d/%d msgs), waiting for deliveries..' %
                           (i, vp.max_msgs))
                    vp.producer.poll(timeout=0.5)
                    continue
                break

            # Delay to achieve desired throughput,
            # but make sure poll is called at least once
            # to serve DRs.
            while True:
                remaining = max(0, t_end - time.time())
                vp.producer.poll(timeout=remaining)
                if remaining <= 0.00000001:
                    break

    except KeyboardInterrupt:
        pass

    # Flush remaining messages to broker.
    vp.dbg('Flushing')
    try:
        vp.producer.flush(5)
    except KeyboardInterrupt:
        pass

    vp.send({'name': 'shutdown_complete', '_qlen': len(vp.producer)})

    vp.dbg('All done')
