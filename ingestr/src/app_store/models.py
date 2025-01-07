import re
from dataclasses import dataclass
from dataclasses_json import dataclass_json
from typing import List, Optional

@dataclass_json
@dataclass
class ReportRequestAttributes:
    accessType: str
    stoppedDueToInactivity: bool

@dataclass_json
@dataclass
class ReportAttributes:
    name: str
    category: str

@dataclass_json
@dataclass
class ReportInstanceAttributes:
    granularity: str
    processingDate: str

@dataclass_json
@dataclass
class ReportSegmentAttributes:
    checksum: str
    url: str
    sizeInBytes: int

@dataclass_json
@dataclass
class ReportRequest:
    type: str
    id: str
    attributes: ReportRequestAttributes

@dataclass_json
@dataclass
class Report:
    type: str
    id: str
    attributes: ReportAttributes

@dataclass_json
@dataclass
class ReportInstance:
    type: str
    id: str
    attributes: ReportInstanceAttributes

@dataclass_json
@dataclass
class ReportSegment:
    type: str
    id: str
    attributes: ReportSegmentAttributes


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
    data: List[ReportRequest]
    meta: Meta

@dataclass_json
@dataclass
class AnalyticsReportResponse:
    data: List[Report]
    meta: Meta

@dataclass_json
@dataclass
class AnalyticsReportInstancesResponse:
    data: List[ReportInstance]
    meta: Meta

@dataclass_json
@dataclass
class AnalyticsReportSegmentsResponse:
    data: List[ReportSegment]
    meta: Meta