
import warnings
from rudderstack.analytics.version import VERSION
from rudderstack.analytics.client import Client
import deprecation
__version__ = VERSION

"""Settings."""
write_key = Client.DefaultConfig.write_key

@property
@deprecation.deprecated(deprecated_in="2.0",
                        current_version=__version__,
                        details="Use the dataPlaneUrl property instead")
def host(self):
    warnings.warn('The use of host is deprecated. Use dataPlaneUrl instead', DeprecationWarning)
    return host

@host.setter
@deprecation.deprecated(deprecated_in="2.0",
                        current_version=__version__,
                        details="Use the dataPlaneUrl property instead")
def host(self, value: str):
    warnings.warn('The use of host is deprecated. Use dataPlaneUrl instead', DeprecationWarning)
    self.host = value
    
host = Client.DefaultConfig.host

@property
def dataPlaneUrl(self):
    return dataPlaneUrl

@dataPlaneUrl.setter
def dataPlaneUrl(self, value: str):
     self.host = value   

on_error = Client.DefaultConfig.on_error
debug = Client.DefaultConfig.debug
send = Client.DefaultConfig.send
sync_mode = Client.DefaultConfig.sync_mode
max_queue_size = Client.DefaultConfig.max_queue_size
gzip = Client.DefaultConfig.gzip
timeout = Client.DefaultConfig.timeout
upload_interval = Client.DefaultConfig.upload_interval
upload_size = Client.DefaultConfig.upload_size
max_retries = Client.DefaultConfig.max_retries

default_client = None


def track(*args, **kwargs):
    """Send a track call."""
    _proxy('track', *args, **kwargs)


def identify(*args, **kwargs):
    """Send a identify call."""
    _proxy('identify', *args, **kwargs)


def group(*args, **kwargs):
    """Send a group call."""
    _proxy('group', *args, **kwargs)


def alias(*args, **kwargs):
    """Send a alias call."""
    _proxy('alias', *args, **kwargs)


def page(*args, **kwargs):
    """Send a page call."""
    _proxy('page', *args, **kwargs)


def screen(*args, **kwargs):
    """Send a screen call."""
    _proxy('screen', *args, **kwargs)


def flush():
    """Tell the client to flush."""
    _proxy('flush')


def join():
    """Block program until the client clears the queue"""
    _proxy('join')


def shutdown():
    """Flush all messages and cleanly shutdown the client"""
    _proxy('flush')
    _proxy('join')


def _proxy(method, *args, **kwargs):
    """Create an analytics client if one doesn't exist and send to it."""
    global default_client
    if not default_client:
        if isinstance(dataPlaneUrl,str) and dataPlaneUrl != "":
            finalDataplaneUrl = dataPlaneUrl
        else:
            finalDataplaneUrl = host    
        
        default_client = Client(write_key, host=finalDataplaneUrl, debug=debug,
                                max_queue_size=max_queue_size,
                                send=send, on_error=on_error,
                                gzip=gzip, max_retries=max_retries,
                                sync_mode=sync_mode, timeout=timeout)

    fn = getattr(default_client, method)
    fn(*args, **kwargs)
