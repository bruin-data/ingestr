class NoReportsFoundError(Exception):
    def __init__(self):
        super().__init__("No Report instances found for the given date range")


class NoOngoingReportRequestsFoundError(Exception):
    def __init__(self):
        super().__init__(
            "No ONGOING report requests found (or they're stopped due to inactivity)"
        )


class NoSuchReportError(Exception):
    def __init__(self, report_name):
        super().__init__(f"No such report found: {report_name}")
