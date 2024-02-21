import os

import machineid
import rudderstack.analytics as rudder_analytics  # type: ignore

rudder_analytics.write_key = "2cUr13DDQcX2x2kAfMEfdrKvrQa"
rudder_analytics.dataPlaneUrl = "https://getbruinbumlky.dataplane.rudderstack.com"


def track(event_name, event_properties):
    if os.environ.get("DISABLE_TELEMETRY", False):
        return

    rudder_analytics.track(machineid.hashed_id(), event_name, event_properties)
