#!/usr/bin/python3

'''
    Implements sending messages via fedora-messaging. To send messages
    one needs credentials to the restricted Fedora broker. In a developer
    workflow, one can also run it against a local rabbitmq instance.
    For more details, see:

    https://fedora-messaging.readthedocs.io/en/latest/quick-start.html
'''

import copy
import threading
import uuid

import multiprocessing as mp

from fedora_messaging import message
from fedora_messaging.api import publish, twisted_consume
from fedora_messaging.config import conf

from twisted.internet import reactor

# these files are part of fedora-messaging
FEDORA_MESSAGING_PUBLIC_CONF = {
    'prod': '/etc/fedora-messaging/fedora.toml',
    'stg': '/etc/fedora-messaging/fedora.stg.toml',
}

FEDORA_MESSAGING_COREOS_TOPIC_PREFIX = {
    'prod': 'org.fedoraproject.prod.coreos',
    'stg': 'org.fedoraproject.stg.coreos',
}

# You can look at examples of recent fedmsgs for particular topics via
# datagrepper, e.g.:
#
# https://apps.fedoraproject.org/datagrepper/raw?topic=org.fedoraproject.prod.coreos.build.request.ostree-sign&delta=100000
# https://apps.fedoraproject.org/datagrepper/raw?topic=org.fedoraproject.prod.coreos.build.request.artifacts-sign&delta=100000

# Default to timeout after 60 seconds
DEFAULT_REQUEST_TIMEOUT_SEC = 60


# This is used for requesting other services to perform specific actions. The
# function does not return until the service replies (or we time out).
# Supported request types:
# - ostree-sign: sent by build pipeline to sign OSTree commits
# - artifacts-sign: sent by build pipeline to sign images
# - ostree-import: sent by release pipeline to import OSTree commits into the
#                  canonical Fedora repos
def send_request_and_wait_for_response(request_type,
                                       config=None,
                                       environment='prod',
                                       request_timeout=DEFAULT_REQUEST_TIMEOUT_SEC,
                                       body={}):
    assert environment in ['prod', 'stg']
    assert request_type in ['ostree-sign', 'artifacts-sign', 'ostree-import']

    # Generate a unique id for this request
    request_id = str(uuid.uuid4())

    # We'll watch for the request response in a thread. Here we create a
    # request_state variable to pass information back and forth and we
    # use threading.Condition() to wake up other threads waiting on
    # the condition.
    global request_state
    request_state = {"status": "pending"}
    cond = threading.Condition()
    start_consumer_thread(cond, request_type, request_id, environment)

    # Send the message/request
    send_message(config=config,
                 topic=get_request_topic(request_type, environment),
                 body={**body, 'request_id': request_id})
    # Wait for the response to come back
    return wait_for_response(cond, request_timeout)


# This is used for informational messages for which don't expect a reply.
# Supported broadcast types:
# - build.state.change: sent by build pipeline when build started or finished
# - stream.release: sent by release pipeline when a new stream release occurred
# - stream.metadata.update: sent by metadata sync job when stream metadata is updated
def broadcast_fedmsg(broadcast_type,
                     config=None,
                     environment='prod',
                     body={}):
    assert environment in ['prod', 'stg']
    assert broadcast_type in ['build.state.change', 'stream.release',
                              'stream.metadata.update']

    # Send the message/request
    send_message(config=config,
                 topic=get_broadcast_topic(broadcast_type, environment),
                 body=body)


def get_broadcast_topic(broadcast_type, environment):
    return f'{FEDORA_MESSAGING_COREOS_TOPIC_PREFIX[environment]}.{broadcast_type}'


def get_request_topic(request_type, environment):
    return f'{FEDORA_MESSAGING_COREOS_TOPIC_PREFIX[environment]}.build.request.{request_type}'


def get_request_finished_topic(request_type, environment):
    return get_request_topic(request_type, environment) + '.finished'


def send_message(config, topic, body):
    print(f"Sending {topic} with body {body}")
    # This is a bit hacky; we fork to publish the message here so that we can
    # load the publishing fedora-messaging config. The TL;DR is: we need auth
    # to publish, but we need to use the public endpoint for consuming so we
    # can create temporary queues. We use the 'spawn' start method so we don't
    # inherit anything by default (like the Twisted state).
    ctx = mp.get_context('spawn')
    p = ctx.Process(target=send_message_impl,
                    args=(config, topic, body))
    p.start()
    p.join()


def send_message_impl(config, topic, body):
    if config:
        conf.load_config(config)
    publish(
        message.Message(body=body, topic=topic)
    )


def wait_for_response(cond, request_timeout):
    with cond:
        print("Waiting for a response to the sent request")
        cond.wait_for(lambda: request_state['status'] != 'pending',
                      timeout=request_timeout)
        # waiting is over now let's make sure it wasn't a timeout
        if request_state['status'] == 'pending':
            raise Exception("Timed out waiting for request response message")
        return copy.deepcopy(request_state)


def start_consumer_thread(cond, request_type, request_id, environment):
    registered = threading.Event()
    t = threading.Thread(target=watch_finished_messages,
                         args=(cond, registered,
                               request_type, request_id, environment),
                         daemon=True)
    t.start()
    registered.wait()
    print("Successfully started consumer thread")


def watch_finished_messages(cond, registered,
                            request_type, request_id, environment):
    def callback(message):
        if 'request_id' not in message.body or message.body['request_id'] != request_id:
            return
        with cond:
            global request_state
            request_state = message.body
            cond.notify()

    queue = str(uuid.uuid4())

    def registered_cb(consumers):
        for consumer in consumers:
            if consumer.queue == queue:
                registered.set()
                break

    def error_cb(failure):
        print(f"Consumer hit failure {failure}")
        reactor.stop()  # pylint: disable=E1101

    # use the public config for this; see related comment in send_message()
    conf.load_config(FEDORA_MESSAGING_PUBLIC_CONF[environment])

    bindings = [{
        'exchange': 'amq.topic',
        'queue': queue,
        'routing_keys': [get_request_finished_topic(request_type, environment)]
    }]
    queues = {
        queue: {
            "durable": False,
            "auto_delete": True,
            "exclusive": True,
            "arguments": {}
        }
    }

    consumers = twisted_consume(callback, bindings=bindings, queues=queues)
    consumers.addCallback(registered_cb)
    consumers.addErrback(error_cb)  # pylint: disable=E1101
    reactor.run(installSignalHandlers=False)  # pylint: disable=E1101
