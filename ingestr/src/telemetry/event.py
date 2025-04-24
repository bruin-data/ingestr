import os


def track(event_name, event_properties: dict):
    if os.environ.get("DISABLE_TELEMETRY", False) or os.environ.get(
        "INGESTR_DISABLE_TELEMETRY", False
    ):
        return

    import platform

    import machineid
    import rudderstack.analytics as rudder_analytics  # type: ignore

    from ingestr.src.version import __version__  # type: ignore

    rudder_analytics.write_key = "2cUr13DDQcX2x2kAfMEfdrKvrQa"
    rudder_analytics.dataPlaneUrl = "https://getbruinbumlky.dataplane.rudderstack.com"

    try:
        if not event_properties:
            event_properties = {}

        event_properties["version"] = __version__
        event_properties["os"] = platform.system()
        event_properties["platform"] = platform.platform()
        event_properties["python_version"] = platform.python_version()
        rudder_analytics.track(machineid.hashed_id(), event_name, event_properties)
    except Exception:
        pass
