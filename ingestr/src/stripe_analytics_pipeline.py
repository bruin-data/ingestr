from typing import Optional, Tuple

import dlt
from pendulum import DateTime
from stripe_analytics import (
    ENDPOINTS,
    INCREMENTAL_ENDPOINTS,
    incremental_stripe_source,
    stripe_source,
)


def load_data(
    endpoints: Tuple[str, ...] = ENDPOINTS + INCREMENTAL_ENDPOINTS,
    start_date: Optional[DateTime] = None,
    end_date: Optional[DateTime] = None,
) -> None:
    """
    This demo script uses the resources with non-incremental
    loading based on "replace" mode to load all data from provided endpoints.

    Args:
        endpoints: A tuple of endpoint names to retrieve data from. Defaults to most popular Stripe API endpoints.
        start_date: An optional start date to limit the data retrieved. Defaults to None.
        end_date: An optional end date to limit the data retrieved. Defaults to None.
    """
    pipeline = dlt.pipeline(
        pipeline_name="stripe_analytics",
        destination='duckdb',
        dataset_name="stripe_updated",
    )
    source = stripe_source(
        endpoints=endpoints, start_date=start_date, end_date=end_date
    )
    load_info = pipeline.run(source)
    print(load_info)


def load_incremental_endpoints(
    endpoints: Tuple[str, ...] = INCREMENTAL_ENDPOINTS,
    initial_start_date: Optional[DateTime] = None,
    end_date: Optional[DateTime] = None,
) -> None:
    """
    This demo script demonstrates the use of resources with incremental loading, based on the "append" mode.
    This approach enables us to load all the data
    for the first time and only retrieve the newest data later,
    without duplicating and downloading a massive amount of data.

    Make sure you're loading objects that don't change over time.

    Args:
        endpoints: A tuple of incremental endpoint names to retrieve data from.
                   Defaults to Stripe API endpoints with uneditable data.
        initial_start_date: An optional parameter that specifies the initial value for dlt.sources.incremental.
                            If parameter is not None, then load only data that were created after initial_start_date on the first run.
                            Defaults to None. Format: datetime(YYYY, MM, DD).
        end_date: An optional end date to limit the data retrieved.
                  Defaults to None. Format: datetime(YYYY, MM, DD).
    """
    pipeline = dlt.pipeline(
        pipeline_name="stripe_analytics",
        destination='duckdb',
        dataset_name="stripe_incremental",
    )
    # load all data on the first run that created before end_date
    source = incremental_stripe_source(
        endpoints=endpoints,
        initial_start_date=initial_start_date,
        end_date=end_date,
    )
    load_info = pipeline.run(source)
    print(load_info)

    # # load nothing, because incremental loading and end date limit
    # source = incremental_stripe_source(
    #     endpoints=endpoints,
    #     initial_start_date=initial_start_date,
    #     end_date=end_date,
    # )
    # load_info = pipeline.run(source)
    # print(load_info)
    #
    # # load only the new data that created after end_date
    # source = incremental_stripe_source(
    #     endpoints=endpoints,
    #     initial_start_date=initial_start_date,
    # )
    # load_info = pipeline.run(source)
    # print(load_info)


if __name__ == "__main__":
    load_data()
    # # load only data that was created during the period between the Jan 1, 2024 (incl.), and the Feb 1, 2024 (not incl.).
    # from pendulum import datetime
    # load_data(start_date=datetime(2024, 1, 1), end_date=datetime(2024, 2, 1))
    # # load only data that was created during the period between the May 3, 2023 (incl.), and the March 1, 2024 (not incl.).
    # load_incremental_endpoints(
    #     endpoints=("Event",),
    #     initial_start_date=datetime(2023, 5, 3),
    #     end_date=datetime(2024, 3, 1),
    # )
