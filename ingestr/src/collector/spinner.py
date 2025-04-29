from typing import Optional

from dlt.common.runtime.collector import Collector
from rich.status import Status


class SpinnerCollector(Collector):
    status: Status
    current_step: str
    started: bool

    def __init__(self) -> None:
        self.status = Status("Ingesting data...", spinner="dots")
        self.started = False

    def update(
        self,
        name: str,
        inc: int = 1,
        total: Optional[int] = None,
        message: Optional[str] = None,  # type: ignore
        label: str = "",
        **kwargs,
    ) -> None:
        self.status.update(self.current_step)

    def _start(self, step: str) -> None:
        self.current_step = self.__step_to_label(step)
        self.status.start()

    def __step_to_label(self, step: str) -> str:
        verb = step.split(" ")[0].lower()
        if verb.startswith("normalize"):
            return "Normalizing the data"
        elif verb.startswith("load"):
            return "Loading the data to the destination"
        elif verb.startswith("extract"):
            return "Extracting the data from the source"

        return f"{verb.capitalize()} the data"

    def _stop(self) -> None:
        self.status.stop()
