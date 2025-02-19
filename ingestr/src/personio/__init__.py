"""Fetches Personio Employees, Absences, Attendances."""

from typing import Iterable, Optional

import dlt
from dlt.common import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource

from .helpers import PersonioAPI


@dlt.source(name="personio", max_table_nesting=0)
def personio_source(
    start_date: TAnyDateTime,
    end_date: Optional[TAnyDateTime] = None,
    client_id: str = dlt.secrets.value,
    client_secret: str = dlt.secrets.value,
    items_per_page: int = 200,
) -> Iterable[DltResource]:
    """
    The source for the Personio pipeline. Available resources are employees, absences, and attendances.

    Args:
        client_id: The client ID of your app.
        client_secret: The client secret of your app.
        items_per_page: The max number of items to fetch per page. Defaults to 200.
    Returns:
        Iterable: A list of DltResource objects representing the data resources.
    """

    client = PersonioAPI(client_id, client_secret)

    @dlt.resource(primary_key="id", write_disposition="merge", max_table_nesting=0)
    def employees(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "last_modified_at", initial_value=None, allow_external_schedulers=True
        ),
        items_per_page: int = items_per_page,
    ) -> Iterable[TDataItem]:
        """
        The resource for employees, supports incremental loading and pagination.

        Args:
            updated_at: The saved state of the last 'last_modified_at' value.
            items_per_page: The max number of items to fetch per page. Defaults to 200.

        Returns:
            Iterable: A generator of employees.
        """

        def convert_item(item: TDataItem) -> TDataItem:
            """Converts an employee item."""
            attributes = item.get("attributes", {})
            output = {}
            for value in attributes.values():
                name = value["universal_id"]
                if not name:
                    label: str = value["label"].replace(" ", "_")
                    name = label.lower()

                if value["type"] == "date" and value["value"]:
                    output[name] = ensure_pendulum_datetime(value["value"])
                else:
                    output[name] = value["value"]
            return output

        if updated_at.last_value:
            last_value = updated_at.last_value.format("YYYY-MM-DDTHH:mm:ss")
        else:
            last_value = None

        params = {"limit": items_per_page, "updated_since": last_value}

        pages = client.get_pages("company/employees", params=params)
        for page in pages:
            yield [convert_item(item) for item in page]

    @dlt.resource(primary_key="id", write_disposition="replace", max_table_nesting=0)
    def absence_types(items_per_page: int = items_per_page) -> Iterable[TDataItem]:
        """
        The resource for absence types (time-off-types), supports pagination.

        Args:
            items_per_page: The max number of items to fetch per page. Defaults to 200.

        Returns:
            Iterable: A generator of absences.
        """

        pages = client.get_pages(
            "company/time-off-types", params={"limit": items_per_page}
        )

        for page in pages:
            yield [item.get("attributes", {}) for item in page]

    @dlt.resource(primary_key="id", write_disposition="merge", max_table_nesting=0)
    def absences(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "updated_at", initial_value=None, allow_external_schedulers=True
        ),
        items_per_page: int = items_per_page,
    ) -> Iterable[TDataItem]:
        """
        The resource for absence (time-offs), supports incremental loading and pagination.

        Args:
            updated_at: The saved state of the last 'updated_at' value.
            items_per_page: The max number of items to fetch per page. Defaults to 200.

        Returns:
            Iterable: A generator of absences.
        """
        if updated_at.last_value:
            updated_iso = updated_at.last_value.format("YYYY-MM-DDTHH:mm:ss")
        else:
            updated_iso = None

        params = {
            "limit": items_per_page,
            "updated_since": updated_iso,
        }

        def convert_item(item: TDataItem) -> TDataItem:
            output = item.get("attributes", {})
            output["created_at"] = ensure_pendulum_datetime(output["created_at"])
            output["updated_at"] = ensure_pendulum_datetime(output["updated_at"])
            return output

        pages = client.get_pages(
            "company/time-offs",
            params=params,
            offset_by_page=True,
        )

        for page in pages:
            yield [convert_item(item) for item in page]

    @dlt.resource(primary_key="id", write_disposition="merge", max_table_nesting=0)
    def attendances(
        start_date: TAnyDateTime = start_date,
        end_date: Optional[TAnyDateTime] = end_date,
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "updated_at", initial_value=None, allow_external_schedulers=True
        ),
        items_per_page: int = items_per_page,
    ) -> Iterable[TDataItem]:
        """
        The resource for attendances, supports incremental loading and pagination.

        Args:
            start_date: The start date to fetch attendances from.
            end_date: The end date to fetch attendances from. Defaults to now.
            updated_at: The saved state of the last 'updated_at' value.
            items_per_page: The max number of items to fetch per page. Defaults to 200.

        Returns:
            Iterable: A generator of attendances.
        """

        end_date = end_date or pendulum.now()
        if updated_at.last_value:
            updated_iso = updated_at.last_value.format("YYYY-MM-DDTHH:mm:ss")
        else:
            updated_iso = None

        params = {
            "limit": items_per_page,
            "start_date": ensure_pendulum_datetime(start_date).to_date_string(),
            "end_date": ensure_pendulum_datetime(end_date).to_date_string(),
            "updated_from": updated_iso,
            "includePending": True,
        }
        pages = client.get_pages(
            "company/attendances",
            params=params,
        )

        def convert_item(item: TDataItem) -> TDataItem:
            """Converts an attendance item."""
            output = dict(id=item["id"], **item.get("attributes"))
            output["date"] = ensure_pendulum_datetime(output["date"]).date()
            output["updated_at"] = ensure_pendulum_datetime(output["updated_at"])
            return output

        for page in pages:
            yield [convert_item(item) for item in page]

    @dlt.resource(primary_key="id", write_disposition="replace", max_table_nesting=0)
    def projects() -> Iterable[TDataItem]:
        """
        The resource for projects.

        Returns:
            Iterable: A generator of projects.
        """

        pages = client.get_pages("company/attendances/projects")

        def convert_item(item: TDataItem) -> TDataItem:
            """Converts an attendance item."""
            output = dict(id=item["id"], **item.get("attributes"))
            output["created_at"] = ensure_pendulum_datetime(output["created_at"])
            output["updated_at"] = ensure_pendulum_datetime(output["updated_at"])
            return output

        for page in pages:
            yield [convert_item(item) for item in page]

    @dlt.resource(primary_key="id", write_disposition="replace", max_table_nesting=0)
    def document_categories() -> Iterable[TDataItem]:
        """
        The resource for document_categories.

        Returns:
            Iterable: A generator of document_categories.
        """

        pages = client.get_pages("company/document-categories")

        def convert_item(item: TDataItem) -> TDataItem:
            """Converts an document_categories item."""
            output = dict(id=item["id"], **item.get("attributes"))
            return output

        for page in pages:
            yield [convert_item(item) for item in page]

    @dlt.resource(primary_key="id", write_disposition="replace", max_table_nesting=0)
    def custom_reports_list() -> Iterable[TDataItem]:
        """
        The resource for custom_reports.

        Returns:
            Iterable: A generator of custom_reports.
        """

        pages = client.get_pages("company/custom-reports/reports")

        for page in pages:
            yield [item.get("attributes", {}) for item in page]

    @dlt.transformer(
        data_from=employees,
        write_disposition="merge",
        primary_key=["employee_id", "id"],
    )
    @dlt.defer
    def employees_absences_balance(employees_item: TDataItem) -> Iterable[TDataItem]:
        """
        The transformer for employees_absences_balance.

        Args:
            employees_item: The employee data.

        Returns:
            Iterable: A generator of employees_absences_balance for each employee.
        """
        for employee in employees_item:
            employee_id = employee["id"]
            pages = client.get_pages(
                f"company/employees/{employee_id}/absences/balance",
            )

            for page in pages:
                yield [dict(employee_id=employee_id, **i) for i in page]

    @dlt.transformer(
        data_from=custom_reports_list,
        write_disposition="merge",
        primary_key=["report_id", "item_id"],
    )
    @dlt.defer
    def custom_reports(
        custom_reports_item: TDataItem, items_per_page: int = items_per_page
    ) -> Iterable[TDataItem]:
        """
        The transformer for custom reports, supports pagination.

        Args:
            custom_reports_item: The custom_report data.
            items_per_page: The max number of items to fetch per page. Defaults to 200.

        Returns:
            Iterable: A generator of employees_absences_balance for each employee.
        """

        def convert_item(item: TDataItem, report_id: str) -> TDataItem:
            """Converts an employee item."""
            attributes = item.pop("attributes")
            output = dict(report_id=report_id, item_id=list(item.values())[0])
            for value in attributes:
                name = value["attribute_id"]
                if value["data_type"] == "date" and value["value"]:
                    output[name] = ensure_pendulum_datetime(value["value"])
                else:
                    output[name] = value["value"]
            return output

        for custom_report in custom_reports_item:
            report_id = custom_report["id"]
            pages = client.get_pages(
                f"company/custom-reports/reports/{report_id}",
                params={"limit": items_per_page},
                offset_by_page=True,
            )

            for page in pages:
                for report in page:
                    report_items = report.get("attributes", {}).get("items", [])
                    yield [convert_item(item, report_id) for item in report_items]

    return (
        employees,
        absence_types,
        absences,
        attendances,
        projects,
        document_categories,
        employees_absences_balance,
        custom_reports_list,
        custom_reports,
    )
