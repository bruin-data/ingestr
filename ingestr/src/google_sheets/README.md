# Google Sheets

## Prepare your data

We recommend to to use [Named Ranges](link to gsheets) to indicate which data should be extracted from a particular spreadsheet and this is how this source
will work by default - when called with without setting any other options. All the named ranges will be converted into tables named after them and stored in the
destination.
* You can let the spreadsheet users to add and remove tables by just adding/removing the ranges, you do not need to configure the pipeline again.
* You can indicate exactly the fragments of interest and only this data will be retrieved so it is the fastest.
* You can name database tables by changing the range names.

If you are not happy with the workflow above, you can:
* Disable it by setting `get_named_ranges` option to False
* Enable retrieving all sheets/tabs with `get_sheets` option set to True
* Pass a list of ranges as supported by Google Sheets in `range_names`

Note that hidden columns will be extracted.

> 💡 You can load data from many spreadsheets and also rename the tables to which data is loaded. This is standard part of `dlt`, see `load_with_table_rename_and_multiple_spreadsheets` demo in `google_sheets_pipeline.py`

### Make sure your data has headers and is a proper table
**First row of any extracted range should contain headers**. Please make sure:
1. The header names are strings and are unique.
2. That all the columns that you intend to extract have a header.
3. That data starts exactly at the origin of the range - otherwise source will remove padding but it is a waste of resources!

When source detects any problems with headers or table layout **it will issue a WARNING in the log** so it makes sense to run your pipeline script manually/locally and fix all the problems.
1. Columns without headers will be removed and not extracted!
2. Columns with headers that does not contain any data will be removed.
2. If there's any problems with reading headers (ie. header is not string or is empty or not unique): **the headers row will be extracted as data** and automatic header names will be used.
3. Empty rows are ignored
4. `dlt` will normalize range names and headers into table and column names - so they may be different in the database than in google sheets. Prefer small cap names without special characters!

### Data Types
`dlt` normalizer will use first row of data to infer types and will try to coerce following rows - creating variant columns if that is not possible. This is a standard behavior.
**date time** and **date** types are also recognized and this happens via additional metadata that is retrieved for the first row.

## Passing the spreadsheet id/url and explicit range names
You can use both url of your spreadsheet that you can copy from the browser ie.
```
https://docs.google.com/spreadsheets/d/1VTtCiYgxjAwcIw7UM1_BSaxC3rzIpr0HwXZwd2OlPD4/edit?usp=sharing
```
or spreadsheet id (which is a part of the url)
```
1VTtCiYgxjAwcIw7UM1_BSaxC3rzIpr0HwXZwd2OlPD4
```
typically you pass it directly to the `google_spreadsheet` function

**passing ranges**

You can pass explicit ranges to the `google_spreadsheet`:
1. sheet names
2. named ranges
3. any range in Google Sheet format ie. **sheet 1!A1:B7**


## The `spreadsheet_info` table
This table is repopulated after every load and keeps the information on loaded ranges:
* id and title of the spreadsheet
* name of the range as passed to the source
* string representation of the loaded range
* range above in parsed representation

## Running on Airflow (and some under the hood information)
Internally, the source loads all the data immediately in the `google_spreadsheet` before execution of the pipeline in `run`. No matter how many ranges you request, we make just two calls to the API to retrieve data. This works very well with typical scripts that create a dlt source with `google_spreadsheet` and then run it with `pipeline.run`.

In case of Airflow, the source is created and executed separately. In typical configuration where runner is a separate machine, **this will load data twice**.

**Moreover, you should not use `scc` decomposition in our Airflow helper**. It will create an instance of the source for each requested range in order to run a task that corresponds to it! Following our [Airflow deployment guide](https://dlthub.com/docs/walkthroughs/deploy-a-pipeline/deploy-with-airflow-composer#2-modify-dag-file), this is how you should use `tasks.add_run` on `PipelineTasksGroup`:
```python
@dag(
    schedule_interval='@daily',
    start_date=pendulum.datetime(2023, 2, 1),
    catchup=False,
    max_active_runs=1,
    default_args=default_task_args
)
def get_named_ranges():
    tasks = PipelineTasksGroup("get_named_ranges", use_data_folder=False, wipe_local_data=True)

    # import your source from pipeline script
    from google_sheets import google_spreadsheet

    pipeline = dlt.pipeline(
        pipeline_name="get_named_ranges",
        dataset_name="named_ranges_data",
        destination='bigquery',
    )

    # do not use decompose to run `google_spreadsheet` in single task
    tasks.add_run(pipeline, google_spreadsheet("1HhWHjqouQnnCIZAFa2rL6vT91YRN8aIhts22SUUR580"), decompose="none", trigger_rule="all_done", retries=0, provide_context=True)
```

## Setup credentials
[We recommend to use service account for any production deployments](https://dlthub.com/docs/dlt-ecosystem/verified-sources/google_sheets#google-sheets-api-authentication)
