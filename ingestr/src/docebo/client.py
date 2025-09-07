"""Docebo API Client for handling authentication and paginated requests."""

from typing import Any, Dict, Iterator, Optional

from ingestr.src.docebo.helpers import normalize_docebo_dates
from ingestr.src.http_client import create_client


class DoceboClient:
    """Client for interacting with Docebo LMS API."""

    def __init__(
        self,
        base_url: str,
        client_id: str,
        client_secret: str,
        username: Optional[str] = None,
        password: Optional[str] = None,
    ):
        """
        Initialize Docebo API client.

        Args:
            base_url: The base URL of your Docebo instance
            client_id: OAuth2 client ID
            client_secret: OAuth2 client secret
            username: Optional username for password grant type
            password: Optional password for password grant type
        """
        self.base_url = base_url.rstrip("/")
        self.client_id = client_id
        self.client_secret = client_secret
        self.username = username
        self.password = password
        self._access_token = None
        # Use shared HTTP client with retry logic
        self.client = create_client(retry_status_codes=[429, 500, 502, 503, 504])

    def get_access_token(self) -> str:
        """
        Get or refresh OAuth2 access token.

        Returns:
            Access token string

        Raises:
            Exception: If authentication fails
        """
        if self._access_token:
            return self._access_token

        auth_endpoint = f"{self.base_url}/oauth2/token"

        # Use client_credentials grant type if no username/password provided
        if not self.username or not self.password:
            data = {
                "client_id": self.client_id,
                "client_secret": self.client_secret,
                "grant_type": "client_credentials",
                "scope": "api",
            }
        else:
            data = {
                "client_id": self.client_id,
                "client_secret": self.client_secret,
                "username": self.username,
                "password": self.password,
                "grant_type": "password",
                "scope": "api",
            }

        response = self.client.post(url=auth_endpoint, data=data)
        response.raise_for_status()
        token_data = response.json()
        self._access_token = token_data.get("access_token")
        if not self._access_token:
            raise Exception("Failed to obtain access token from Docebo")

        return self._access_token

    def get_paginated_data(
        self,
        endpoint: str,
        page_size: int = 200,
        params: Optional[Dict[str, Any]] = None,
    ) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch paginated data from a Docebo API endpoint.

        Args:
            endpoint: API endpoint path (e.g., "manage/v1/user")
            page_size: Number of items per page
            params: Additional query parameters

        Yields:
            Batches of items from the API
        """
        url = f"{self.base_url}/{endpoint}"
        headers = {"authorization": f"Bearer {self.get_access_token()}"}

        page = 1
        has_more_data = True

        while has_more_data:
            request_params = {"page": page, "page_size": page_size}
            if params:
                request_params.update(params)

            response = self.client.get(url=url, headers=headers, params=request_params)
            response.raise_for_status()
            data = response.json()

            # Handle paginated response structure
            if "data" in data:
                # Most Docebo endpoints return data in this structure
                if "items" in data["data"]:
                    items = data["data"]["items"]
                    if items:
                        # Normalize dates for each item before yielding
                        normalized_items = [
                            normalize_docebo_dates(item) for item in items
                        ]
                        yield normalized_items

                    # Check for more pages
                    has_more_data = data["data"].get("has_more_data", False)
                    if has_more_data and "total_page_count" in data["data"]:
                        total_pages = data["data"]["total_page_count"]
                        if page >= total_pages:
                            has_more_data = False
                # Some endpoints might return data directly as a list
                elif isinstance(data["data"], list):
                    items = data["data"]
                    if items:
                        # Normalize dates for each item before yielding
                        normalized_items = [
                            normalize_docebo_dates(item) for item in items
                        ]
                        yield normalized_items
                    # For direct list responses, check if we got a full page
                    has_more_data = len(items) == page_size
                else:
                    has_more_data = False
            # Some endpoints might return items directly
            elif isinstance(data, list):
                if data:
                    # Normalize dates for each item before yielding
                    normalized_items = [normalize_docebo_dates(item) for item in data]
                    yield normalized_items
                has_more_data = len(data) == page_size
            else:
                has_more_data = False

            page += 1

    def fetch_users(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all users from Docebo.

        Yields:
            Batches of user data
        """
        yield from self.get_paginated_data("manage/v1/user")

    def fetch_courses(self, page_size: int = 200) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all courses from Docebo.

        Yields:
            Batches of course data
        """
        yield from self.get_paginated_data("learn/v1/courses", page_size=page_size)

    # Phase 1: Core User and Organization Resources
    def fetch_user_fields(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all user fields from Docebo.

        Yields:
            Batches of user field definitions
        """
        yield from self.get_paginated_data("manage/v1/user_fields")

    def fetch_branches(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all branches/organizational units from Docebo.

        Yields:
            Batches of branch/org chart data
        """
        yield from self.get_paginated_data("manage/v1/orgchart")

    # Phase 2: Group Management
    def fetch_groups(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all groups/audiences from Docebo.

        Yields:
            Batches of group data
        """
        yield from self.get_paginated_data("audiences/v1/audience")

    def fetch_all_group_members(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all group members for all groups.

        Yields:
            Batches of group member data with group_id included
        """
        # First fetch all groups
        all_groups: list[Dict[str, Any]] = []
        for group_batch in self.fetch_groups():
            all_groups.extend(group_batch)

        # Then fetch members for each group
        for group in all_groups:
            group_id = (
                group.get("group_id") or group.get("audience_id") or group.get("id")
            )
            if group_id:
                try:
                    for member_batch in self.get_paginated_data(
                        f"manage/v1/group/{group_id}/members"
                    ):
                        # Add group_id to each member record
                        for member in member_batch:
                            member["group_id"] = group_id
                        yield member_batch
                except Exception as e:
                    print(f"Error fetching members for group {group_id}: {e}")
                    continue

    # Phase 3: Advanced Course Resources
    def fetch_course_fields(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all course field definitions from Docebo.

        Yields:
            Batches of course field data
        """
        yield from self.get_paginated_data("learn/v1/courses/field")

    def fetch_all_course_learning_objects(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch learning objects for all courses.

        Yields:
            Batches of learning object data
        """
        # First fetch all courses
        all_courses: list[Dict[str, Any]] = []
        for course_batch in self.fetch_courses():
            all_courses.extend(course_batch)

        # Then fetch learning objects for each course
        for course in all_courses:
            course_id = course.get("id_course") or course.get("course_id")
            if course_id:
                try:
                    endpoint = f"learn/v1/courses/{course_id}/los"
                    for lo_batch in self.get_paginated_data(endpoint):
                        # Add course_id to each learning object
                        for lo in lo_batch:
                            if "course_id" not in lo:
                                lo["course_id"] = course_id
                        yield lo_batch
                except Exception as e:
                    print(
                        f"Error fetching learning objects for course {course_id}: {e}"
                    )
                    continue

    # Phase 4: Learning Plans
    def fetch_learning_plans(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all learning plans from Docebo.

        Yields:
            Batches of learning plan data
        """
        yield from self.get_paginated_data("learningplan/v1/learningplans")

    def fetch_learning_plan_enrollments(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all learning plan enrollments.

        Yields:
            Batches of learning plan enrollment data
        """
        yield from self.get_paginated_data(
            "learningplan/v1/learningplans/enrollments",
            params={"extra_fields[]": "enrollment_status"},
        )

    def fetch_all_learning_plan_course_enrollments(
        self,
    ) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch course enrollments for all learning plans.

        Yields:
            Batches of learning plan course enrollment data
        """
        # First fetch all learning plans
        all_plans: list[Dict[str, Any]] = []
        for plan_batch in self.fetch_learning_plans():
            all_plans.extend(plan_batch)

        # Then fetch course enrollments for each learning plan
        for plan in all_plans:
            plan_id = (
                plan.get("id_path") or plan.get("learning_plan_id") or plan.get("id")
            )
            if plan_id:
                try:
                    endpoint = (
                        f"learningplan/v1/learningplans/{plan_id}/courses/enrollments"
                    )
                    for enrollment_batch in self.get_paginated_data(
                        endpoint, params={"enrollment_level[]": "student"}
                    ):
                        # Add learning_plan_id to each enrollment
                        for enrollment in enrollment_batch:
                            enrollment["learning_plan_id"] = plan_id
                        yield enrollment_batch
                except Exception as e:
                    print(
                        f"Error fetching course enrollments for learning plan {plan_id}: {e}"
                    )
                    continue

    # Phase 5: Enrollments and Surveys
    def fetch_all_course_enrollments(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch enrollments for all courses.

        Yields:
            Batches of course enrollment data
        """
        # First fetch all courses
        all_courses: list[Dict[str, Any]] = []
        for course_batch in self.fetch_courses():
            all_courses.extend(course_batch)

        # Then fetch enrollments for each course
        for course in all_courses:
            course_id = course.get("id_course") or course.get("course_id")
            if course_id:
                try:
                    endpoint = f"course/v1/courses/{course_id}/enrollments"
                    for enrollment_batch in self.get_paginated_data(
                        endpoint, params={"level[]": "3"}
                    ):
                        # Add course_id to each enrollment
                        for enrollment in enrollment_batch:
                            enrollment["course_id"] = course_id
                        yield enrollment_batch
                except Exception as e:
                    print(f"Error fetching enrollments for course {course_id}: {e}")
                    continue

    # Additional Resources
    def fetch_sessions(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all ILT/classroom sessions for all courses.

        Yields:
            Batches of session data
        """
        # First fetch all courses
        all_courses: list[Dict[str, Any]] = []
        for course_batch in self.fetch_courses():
            all_courses.extend(course_batch)

        # Then fetch sessions for each course
        for course in all_courses:
            course_id = course.get("id_course") or course.get("course_id")
            if course_id:
                try:
                    endpoint = f"learn/v1/courses/{course_id}/sessions"
                    for session_batch in self.get_paginated_data(endpoint):
                        # Add course_id to each session
                        for session in session_batch:
                            session["course_id"] = course_id
                        yield session_batch
                except Exception:
                    # Many courses may not have sessions, so just continue
                    continue

    def fetch_categories(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all course categories.

        Yields:
            Batches of category data
        """
        yield from self.get_paginated_data("learn/v1/categories")

    def fetch_certifications(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all certifications.

        Yields:
            Batches of certification data
        """
        yield from self.get_paginated_data("learn/v1/certification")

    def fetch_external_training(self) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch all external training records.

        Yields:
            Batches of external training data
        """
        yield from self.get_paginated_data("learn/v1/external_training")

    def fetch_survey_answers_for_poll(
        self, poll_id: int, course_id: int
    ) -> Dict[str, Any]:
        """
        Fetch survey answers for a specific poll.

        Args:
            poll_id: The poll/survey ID
            course_id: The course ID containing the poll

        Returns:
            Survey answer data or empty dict if no answers
        """
        url = f"{self.base_url}/learn/v1/survey/{poll_id}/answer"
        headers = {"authorization": f"Bearer {self.get_access_token()}"}
        params = {"id_course": course_id}

        response = self.client.get(url, headers=headers, params=params)
        return normalize_docebo_dates(response.json().get("data", {}))
