import os
import tempfile
from concurrent.futures import ThreadPoolExecutor

from main_test import DESTINATIONS, SOURCES  # type: ignore


def pytest_configure(config):
    if is_master(config):
        config.shared_directory = tempfile.mkdtemp()


def pytest_configure_node(node):
    """xdist hook"""
    node.workerinput["shared_directory"] = node.config.shared_directory


def is_master(config):
    """True if the code running the given pytest.config object is running in a xdist master
    node or not running xdist at all.
    """
    return not hasattr(config, "workerinput")


def start_containers(config):
    if hasattr(config, "workerinput"):
        return

    unique_containers = set(SOURCES.values()) | set(DESTINATIONS.values())
    for container in unique_containers:
        container.container_lock_dir = config.shared_directory

    with ThreadPoolExecutor() as executor:
        for container in unique_containers:
            executor.submit(container.start_fully)
        # futures = [
        #     executor.submit(container.start_fully) for container in unique_containers
        # ]
        # # Wait for all futures to complete
        # for future in futures:
        #     future.result()


def stop_containers(config):
    if hasattr(config, "workerinput"):
        return

    should_manage_containers = os.environ.get("PYTEST_XDIST_WORKER", "gw0") == "gw0"
    if not should_manage_containers:
        return

    unique_containers = set(SOURCES.values()) | set(DESTINATIONS.values())

    for container in unique_containers:
        container.stop_fully()


def pytest_sessionstart(session):
    start_containers(session.config)


def pytest_sessionfinish(session, exitstatus):
    stop_containers(session.config)
