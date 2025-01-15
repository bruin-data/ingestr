import dlt
from chess import source


def load_players_games_example(start_month: str, end_month: str) -> None:
    """Constructs a pipeline that will load chess games of specific players for a range of months."""

    # configure the pipeline: provide the destination and dataset name to which the data should go
    pipeline = dlt.pipeline(
        pipeline_name="chess_pipeline",
        destination='clickhouse',
        dataset_name="chess_players_games_data",
    )
    # create the data source by providing a list of players and start/end month in YYYY/MM format
    data = source(
        ["magnuscarlsen", "vincentkeymer", "dommarajugukesh", "rpragchess"],
        start_month=start_month,
        end_month=end_month,
    )
    # load the "players_games" and "players_profiles" out of all the possible resources
    info = pipeline.run(data.with_resources("players_games", "players_profiles"))
    print(info)


def load_players_online_status() -> None:
    """Constructs a pipeline that will append online status of selected players"""

    pipeline = dlt.pipeline(
        pipeline_name="chess_pipeline",
        destination='clickhouse',
        dataset_name="chess_players_games_data",
    )
    data = source(["magnuscarlsen", "vincentkeymer", "dommarajugukesh", "rpragchess"])
    info = pipeline.run(data.with_resources("players_online_status"))
    print(info)


def load_players_games_incrementally() -> None:
    """Pipeline will not load the same game archive twice"""
    # loads games for 11.2022
    load_players_games_example("2022/11", "2022/11")
    # second load skips games for 11.2022 but will load for 12.2022
    load_players_games_example("2022/11", "2022/12")


if __name__ == "__main__":
    # run our main example
    load_players_games_example("2022/11", "2022/12")
    load_players_online_status()
