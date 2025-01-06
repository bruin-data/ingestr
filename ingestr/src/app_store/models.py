from dataclasses import dataclass
from dataclasses_json import dataclass_json
from typing import List, Optional

@dataclass_json
@dataclass
class Link:
    self: str
    related: Optional[str] = None

@dataclass_json
@dataclass
class ReportsRelationship:
    links: Link

@dataclass_json
@dataclass
class Relationships:
    reports: ReportsRelationship

@dataclass_json
@dataclass
class Attributes:
    accessType: str
    stoppedDueToInactivity: bool

@dataclass_json
@dataclass
class DataItem:
    type: str
    id: str
    attributes: Attributes
    relationships: Relationships
    links: Link

@dataclass_json
@dataclass
class PagingMeta:
    total: int
    limit: int

@dataclass_json
@dataclass
class Meta:
    paging: PagingMeta

@dataclass_json
@dataclass
class AnalyticsReportRequestsResponse:
    data: List[DataItem]
    links: Link
    meta: Meta