"""Docebo source for ingestr."""

import json
from typing import Any, Dict, Iterator, Optional

import dlt
from dlt.sources import DltResource

from .client import DoceboClient
from .helpers import normalize_date_field, normalize_docebo_dates


@dlt.source(name="docebo", max_table_nesting=0)
def docebo_source(
    base_url: str,
    client_id: str,
    client_secret: str,
    username: Optional[str] = None,
    password: Optional[str] = None,
) -> list[DltResource]:
    """
    Docebo source for fetching data from Docebo LMS API.

    Args:
        base_url: The base URL of your Docebo instance (e.g., https://yourcompany.docebosaas.com)
        client_id: OAuth2 client ID
        client_secret: OAuth2 client secret
        username: Username for authentication
        password: Password for authentication

    Yields:
        DltResource: Resources available from Docebo API
    """

    # Initialize client once for all resources
    client = DoceboClient(
        base_url=base_url,
        client_id=client_id,
        client_secret=client_secret,
        username=username,
        password=password,
    )

    @dlt.resource(
        name="users",
        write_disposition="replace",
        columns={
            "user_id": {"data_type": "text", "nullable": True},
            "username": {"data_type": "text", "nullable": True},
            "first_name": {"data_type": "text", "nullable": True},
            "last_name": {"data_type": "text", "nullable": True},
            "email": {"data_type": "text", "nullable": True},
            "uuid": {"data_type": "text", "nullable": True},
            "is_manager": {"data_type": "bool", "nullable": True},
            "fullname": {"data_type": "text", "nullable": True},
            "last_access_date": {"data_type": "timestamp", "nullable": True},
            "last_update": {"data_type": "timestamp", "nullable": True},
            "creation_date": {"data_type": "timestamp", "nullable": True},
            "status": {"data_type": "text", "nullable": True},
            "avatar": {"data_type": "text", "nullable": True},
            "language": {"data_type": "text", "nullable": True},
            "lang_code": {"data_type": "text", "nullable": True},
            "level": {"data_type": "text", "nullable": True},
            "email_validation_status": {"data_type": "text", "nullable": True},
            "send_notification": {"data_type": "text", "nullable": True},
            "newsletter_optout": {"data_type": "text", "nullable": True},
            "encoded_username": {"data_type": "text", "nullable": True},
            "timezone": {"data_type": "text", "nullable": True},
            "active_subordinates_count": {"data_type": "bigint", "nullable": True},
            "expired": {"data_type": "bool", "nullable": True},
            "multidomains": {"data_type": "json", "nullable": True},
            "manager_names": {"data_type": "json", "nullable": True},
            "managers": {"data_type": "json", "nullable": True},
            "actions": {"data_type": "json", "nullable": True},
        },
    )
    def users() -> Iterator[Dict[str, Any]]:
        """Fetch all users from Docebo."""
        for users_batch in client.fetch_users():
            # Apply normalizer to each user and yield individually
            for user in users_batch:
                yield normalize_docebo_dates(user)

    @dlt.resource(
        name="courses",
        write_disposition="replace",
        parallelized=True,
        columns={
            "id_course": {"data_type": "bigint", "nullable": True},
            "name": {"data_type": "text", "nullable": True},
            "uidCourse": {"data_type": "text", "nullable": True},
            "description": {"data_type": "text", "nullable": True},
            "date_last_updated": {"data_type": "date", "nullable": True},
            "course_type": {"data_type": "text", "nullable": True},
            "selling": {"data_type": "bool", "nullable": True},
            "code": {"data_type": "text", "nullable": True},
            "slug_name": {"data_type": "text", "nullable": True},
            "image": {"data_type": "text", "nullable": True},
            "duration": {"data_type": "bigint", "nullable": True},
            "language": {"data_type": "text", "nullable": True},
            "language_label": {"data_type": "text", "nullable": True},
            "multi_languages": {"data_type": "json", "nullable": True},
            "price": {"data_type": "text", "nullable": True},
            "is_new": {"data_type": "text", "nullable": True},
            "is_opened": {"data_type": "text", "nullable": True},
            "rating_option": {"data_type": "text", "nullable": True},
            "current_rating": {"data_type": "bigint", "nullable": True},
            "credits": {"data_type": "bigint", "nullable": True},
            "img_url": {"data_type": "text", "nullable": True},
            "can_rate": {"data_type": "bool", "nullable": True},
            "can_self_unenroll": {"data_type": "bool", "nullable": True},
            "start_date": {"data_type": "date", "nullable": True},
            "end_date": {"data_type": "date", "nullable": True},
            "category": {"data_type": "json", "nullable": True},
            "enrollment_policy": {"data_type": "bigint", "nullable": True},
            "max_attempts": {"data_type": "bigint", "nullable": True},
            "available_seats": {"data_type": "json", "nullable": True},
            "is_affiliate": {"data_type": "bool", "nullable": True},
            "partner_fields": {"data_type": "text", "nullable": True},
            "partner_data": {"data_type": "json", "nullable": True},
            "affiliate_price": {"data_type": "text", "nullable": True},
        },
    )
    def courses() -> Iterator[Dict[str, Any]]:
        print("running courses transformer")
        """Fetch all courses from Docebo."""
        for courses_batch in client.fetch_courses(page_size=1000):
            for course in courses_batch:
                yield normalize_docebo_dates(course)

    @dlt.resource(
        name="user_fields",
        write_disposition="replace",
        primary_key="id",
        columns={
            "id": {"data_type": "bigint", "nullable": True},
            "name": {"data_type": "text", "nullable": True},
            "type": {"data_type": "text", "nullable": True},
            "mandatory": {"data_type": "bool", "nullable": True},
            "show_on_detail": {"data_type": "bool", "nullable": True},
            "show_in_filter": {"data_type": "bool", "nullable": True},
            "options": {"data_type": "json", "nullable": True},
            "ref_area": {"data_type": "bigint", "nullable": True},
            "is_valid": {"data_type": "bool", "nullable": True},
            "sequence": {"data_type": "bigint", "nullable": True},
        },
    )
    def user_fields() -> Iterator[Dict[str, Any]]:
        """Fetch all user field definitions from Docebo."""
        for fields_batch in client.fetch_user_fields():
            for field in fields_batch:
                yield normalize_docebo_dates(field)

    @dlt.resource(
        name="branches",
        write_disposition="replace",
        columns={
            "id_org": {"data_type": "bigint", "nullable": True},
            "id_parent": {"data_type": "bigint", "nullable": True},
            "lft": {"data_type": "bigint", "nullable": True},
            "rgt": {"data_type": "bigint", "nullable": True},
            "code": {"data_type": "text", "nullable": True},
            "translation": {"data_type": "json", "nullable": True},
            "external_id": {"data_type": "text", "nullable": True},
            "actions": {"data_type": "json", "nullable": True},
        },
    )
    def branches() -> Iterator[Dict[str, Any]]:
        """Fetch all branches/organizational units from Docebo."""
        for branches_batch in client.fetch_branches():
            for branch in branches_batch:
                yield normalize_docebo_dates(branch)

    # Phase 2: Group Management
    @dlt.resource(
        name="groups",
        write_disposition="replace",
        primary_key="group_id",
        columns={
            "group_id": {"data_type": "bigint", "nullable": True},
            "name": {"data_type": "text", "nullable": True},
            "description": {"data_type": "text", "nullable": True},
            "language": {"data_type": "text", "nullable": True},
            "total_members": {"data_type": "bigint", "nullable": True},
            "id_branch": {"data_type": "bigint", "nullable": True},
            "enrollment_rules": {"data_type": "json", "nullable": True},
            "enrollment_rules_options": {"data_type": "json", "nullable": True},
            "member_fields": {"data_type": "json", "nullable": True},
            "is_default": {"data_type": "bool", "nullable": True},
            "creation_date": {"data_type": "timestamp", "nullable": True},
            "last_update": {"data_type": "timestamp", "nullable": True},
        },
    )
    def groups() -> Iterator[Dict[str, Any]]:
        """Fetch all groups/audiences from Docebo."""
        for groups_batch in client.fetch_groups():
            for group in groups_batch:
                yield normalize_docebo_dates(group)

    @dlt.resource(
        name="group_members",
        write_disposition="replace",
        primary_key=["group_id", "user_id"],
        columns={
            "group_id": {"data_type": "bigint", "nullable": True},
            "user_id": {"data_type": "text", "nullable": True},
            "username": {"data_type": "text", "nullable": True},
            "first_name": {"data_type": "text", "nullable": True},
            "last_name": {"data_type": "text", "nullable": True},
            "email": {"data_type": "text", "nullable": True},
            "level": {"data_type": "text", "nullable": True},
            "enrollment_date": {"data_type": "timestamp", "nullable": True},
        },
    )
    def group_members() -> Iterator[Dict[str, Any]]:
        """Fetch all group members for all groups."""
        for members_batch in client.fetch_all_group_members():
            for member in members_batch:
                yield normalize_docebo_dates(member)

    # Phase 3: Advanced Course Resources
    @dlt.resource(
        name="course_fields",
        write_disposition="replace",
        primary_key="field_id",
        columns={
            "field_id": {"data_type": "bigint", "nullable": True},
            "type_field": {"data_type": "text", "nullable": True},
            "name_field": {"data_type": "text", "nullable": True},
            "is_mandatory": {"data_type": "bool", "nullable": True},
            "show_on_course_details": {"data_type": "bool", "nullable": True},
            "show_on_course_filter": {"data_type": "bool", "nullable": True},
            "options": {"data_type": "json", "nullable": True},
            "sequence": {"data_type": "bigint", "nullable": True},
        },
    )
    def course_fields() -> Iterator[Dict[str, Any]]:
        """Fetch all course field definitions from Docebo."""
        for fields_batch in client.fetch_course_fields():
            for field in fields_batch:
                yield normalize_docebo_dates(field)

    @dlt.transformer(
        name="learning_objects",
        data_from=courses,
        write_disposition="replace",
        parallelized=True,
        columns={
            "course_id": {"data_type": "bigint", "nullable": True},
            "id_org": {"data_type": "bigint", "nullable": True},
            "object_id": {"data_type": "bigint", "nullable": True},
            "lo_code": {"data_type": "text", "nullable": True},
            "lo_name": {"data_type": "text", "nullable": True},
            "lo_type": {"data_type": "text", "nullable": True},
            "lo_visibility": {"data_type": "text", "nullable": True},
            "lo_link": {"data_type": "text", "nullable": True},
            "lo_thumbnail": {"data_type": "text", "nullable": True},
            "mobile_compatibility": {"data_type": "text", "nullable": True},
            "lo_external_source_url": {"data_type": "text", "nullable": True},
            "created_by": {"data_type": "text", "nullable": True},
            "creation_date": {"data_type": "timestamp", "nullable": True},
            "duration": {"data_type": "bigint", "nullable": True},
        },
    )
    def learning_objects(course_item: Dict[str, Any]) -> Iterator[Dict[str, Any]]:
        course_id = course_item.get("id_course")
        if course_id:
            los_endpoint = f"learn/v1/courses/{course_id}/los"
            for lo_batch in client.get_paginated_data(los_endpoint):
                for lo in lo_batch:
                    # Add course_id to learning object if not present
                    if "course_id" not in lo:
                        lo["course_id"] = course_id
                    yield normalize_docebo_dates(lo)

    # Phase 4: Learning Plans
    @dlt.resource(
        name="learning_plans",
        write_disposition="replace",
        columns={
            "learning_plan_id": {"data_type": "bigint", "nullable": True},
            "uuid": {"data_type": "text", "nullable": True},
            "code": {"data_type": "text", "nullable": True},
            "title": {"data_type": "text", "nullable": True},
            "thumbnail_url": {"data_type": "text", "nullable": True},
            "price": {"data_type": "text", "nullable": True},
            "credits": {"data_type": "bigint", "nullable": True},
            "is_published": {"data_type": "bool", "nullable": True},
            "is_publishable": {"data_type": "bool", "nullable": True},
            "assigned_courses_count": {"data_type": "bigint", "nullable": True},
            "assigned_enrollments_count": {"data_type": "bigint", "nullable": True},
            "assigned_catalogs_count": {"data_type": "bigint", "nullable": True},
            "assigned_channels_count": {"data_type": "bigint", "nullable": True},
            "created_on": {"data_type": "timestamp", "nullable": True},
            "created_by": {"data_type": "json", "nullable": True},
            "updated_on": {"data_type": "timestamp", "nullable": True},
            "updated_by": {"data_type": "json", "nullable": True},
        },
    )
    def learning_plans() -> Iterator[Dict[str, Any]]:
        """Fetch all learning plans from Docebo."""
        for plans_batch in client.fetch_learning_plans():
            for plan in plans_batch:
                yield normalize_docebo_dates(plan)

    @dlt.resource(
        name="learning_plan_enrollments",
        write_disposition="replace",
        columns={
            "id_path": {"data_type": "bigint", "nullable": True},
            "id_user": {"data_type": "text", "nullable": True},
            "enrollment_date": {"data_type": "timestamp", "nullable": True},
            "completion_date": {"data_type": "timestamp", "nullable": True},
            "enrollment_status": {"data_type": "text", "nullable": True},
            "score_given": {"data_type": "double", "nullable": True},
            "total_credits": {"data_type": "bigint", "nullable": True},
            "total_time": {"data_type": "bigint", "nullable": True},
            "completed_courses": {"data_type": "bigint", "nullable": True},
            "total_courses": {"data_type": "bigint", "nullable": True},
        },
    )
    def learning_plan_enrollments() -> Iterator[Dict[str, Any]]:
        """Fetch all learning plan enrollments."""
        for enrollments_batch in client.fetch_learning_plan_enrollments():
            for enrollment in enrollments_batch:
                yield normalize_docebo_dates(enrollment)

    @dlt.resource(
        name="learning_plan_course_enrollments",
        write_disposition="replace",
        columns={
            "learning_plan_id": {"data_type": "bigint", "nullable": True},
            "course_id": {"data_type": "bigint", "nullable": True},
            "user_id": {"data_type": "text", "nullable": True},
            "enrollment_date": {"data_type": "timestamp", "nullable": True},
            "completion_date": {"data_type": "timestamp", "nullable": True},
            "status": {"data_type": "text", "nullable": True},
            "score": {"data_type": "double", "nullable": True},
            "credits": {"data_type": "bigint", "nullable": True},
            "total_time": {"data_type": "bigint", "nullable": True},
        },
    )
    def learning_plan_course_enrollments() -> Iterator[Dict[str, Any]]:
        """Fetch course enrollments for all learning plans."""
        for enrollments_batch in client.fetch_all_learning_plan_course_enrollments():
            for enrollment in enrollments_batch:
                yield normalize_docebo_dates(enrollment)

    # Phase 5: Enrollments
    @dlt.resource(
        name="course_enrollments",
        write_disposition="replace",
        columns={
            "course_id": {"data_type": "bigint", "nullable": True},
            "user_id": {"data_type": "text", "nullable": True},
            "enrollment_date": {"data_type": "timestamp", "nullable": True},
            "completion_date": {"data_type": "timestamp", "nullable": True},
            "status": {"data_type": "text", "nullable": True},
            "level": {"data_type": "text", "nullable": True},
            "score_given": {"data_type": "double", "nullable": True},
            "score_total": {"data_type": "double", "nullable": True},
            "credits": {"data_type": "bigint", "nullable": True},
            "total_time": {"data_type": "bigint", "nullable": True},
            "expire_date": {"data_type": "timestamp", "nullable": True},
            "certificate_id": {"data_type": "text", "nullable": True},
        },
    )
    def course_enrollments() -> Iterator[Dict[str, Any]]:
        """Fetch enrollments for all courses."""
        for enrollments_batch in client.fetch_all_course_enrollments():
            for enrollment in enrollments_batch:
                yield normalize_docebo_dates(enrollment)

    # Additional Resources
    @dlt.resource(
        name="sessions",
        write_disposition="replace",
        columns={
            "course_id": {"data_type": "bigint", "nullable": True},
            "session_id": {"data_type": "bigint", "nullable": True},
            "name": {"data_type": "text", "nullable": True},
            "code": {"data_type": "text", "nullable": True},
            "date_start": {"data_type": "timestamp", "nullable": True},
            "date_end": {"data_type": "timestamp", "nullable": True},
            "instructor": {"data_type": "text", "nullable": True},
            "location": {"data_type": "text", "nullable": True},
            "classroom": {"data_type": "text", "nullable": True},
            "max_participants": {"data_type": "bigint", "nullable": True},
            "enrolled_users": {"data_type": "bigint", "nullable": True},
            "waiting_users": {"data_type": "bigint", "nullable": True},
            "session_type": {"data_type": "text", "nullable": True},
            "timezone": {"data_type": "text", "nullable": True},
            "attendance_type": {"data_type": "text", "nullable": True},
        },
    )
    def sessions() -> Iterator[Dict[str, Any]]:
        """Fetch all ILT/classroom sessions."""
        for sessions_batch in client.fetch_sessions():
            for session in sessions_batch:
                yield normalize_docebo_dates(session)

    @dlt.resource(
        name="categories",
        write_disposition="replace",
        columns={
            "id_cat": {"data_type": "bigint", "nullable": True},
            "code": {"data_type": "text", "nullable": True},
            "description": {"data_type": "text", "nullable": True},
            "id_parent": {"data_type": "bigint", "nullable": True},
            "lft": {"data_type": "bigint", "nullable": True},
            "rgt": {"data_type": "bigint", "nullable": True},
            "is_active": {"data_type": "bool", "nullable": True},
            "translations": {"data_type": "json", "nullable": True},
        },
    )
    def categories() -> Iterator[Dict[str, Any]]:
        """Fetch all course categories."""
        for categories_batch in client.fetch_categories():
            for category in categories_batch:
                yield normalize_docebo_dates(category)

    @dlt.resource(
        name="certifications",
        write_disposition="replace",
        columns={
            "id_cert": {"data_type": "bigint", "nullable": True},
            "code": {"data_type": "text", "nullable": True},
            "title": {"data_type": "text", "nullable": True},
            "description": {"data_type": "text", "nullable": True},
            "type": {"data_type": "text", "nullable": True},
            "validity_type": {"data_type": "text", "nullable": True},
            "validity_days": {"data_type": "bigint", "nullable": True},
            "renewal_available": {"data_type": "bool", "nullable": True},
            "renewal_days_before": {"data_type": "bigint", "nullable": True},
            "meta_language": {"data_type": "text", "nullable": True},
            "meta_language_label": {"data_type": "text", "nullable": True},
            "created_on": {"data_type": "timestamp", "nullable": True},
            "updated_on": {"data_type": "timestamp", "nullable": True},
        },
    )
    def certifications() -> Iterator[Dict[str, Any]]:
        """Fetch all certifications."""
        for certifications_batch in client.fetch_certifications():
            for cert in certifications_batch:
                yield normalize_docebo_dates(cert)

    @dlt.resource(
        name="external_training",
        write_disposition="replace",
        columns={
            "external_training_id": {"data_type": "bigint", "nullable": True},
            "user_id": {"data_type": "text", "nullable": True},
            "title": {"data_type": "text", "nullable": True},
            "description": {"data_type": "text", "nullable": True},
            "training_type": {"data_type": "text", "nullable": True},
            "provider": {"data_type": "text", "nullable": True},
            "date_from": {"data_type": "date", "nullable": True},
            "date_to": {"data_type": "date", "nullable": True},
            "credits": {"data_type": "bigint", "nullable": True},
            "score": {"data_type": "double", "nullable": True},
            "status": {"data_type": "text", "nullable": True},
            "certificate_file": {"data_type": "text", "nullable": True},
            "created_on": {"data_type": "timestamp", "nullable": True},
            "updated_on": {"data_type": "timestamp", "nullable": True},
        },
    )
    def external_training() -> Iterator[Dict[str, Any]]:
        """Fetch all external training records."""
        for training_batch in client.fetch_external_training():
            for training in training_batch:
                yield normalize_docebo_dates(training)

    # Survey Resources - Using transformer chain for parallelization

    # Transformer that filters learning_objects for polls only
    @dlt.transformer(
        data_from=learning_objects,
        write_disposition="replace",
        name="polls",
        parallelized=True,
        columns={
            "poll_id": {"data_type": "bigint", "nullable": True},
            "course_id": {"data_type": "bigint", "nullable": True},
            "poll_title": {"data_type": "text", "nullable": True},
            "object_type": {"data_type": "text", "nullable": True},
            "lo_type": {"data_type": "text", "nullable": True},
        },
    )
    def polls(lo_item: Dict[str, Any]) -> Iterator[Dict[str, Any]]:
        # print("running polls transformer")
        """Filter learning objects to get only polls."""
        # Check if this learning object is a poll
        if lo_item.get("object_type") == "poll" or lo_item.get("lo_type") == "poll":
            # print(f"polls transformer: {lo_item}")
            poll_id = lo_item["id_resource"]
            course_id = lo_item.get("course_id")

            if poll_id and course_id:
                yield normalize_docebo_dates(
                    {
                        "poll_id": poll_id,
                        "course_id": course_id,
                        "poll_title": lo_item.get("title")
                        or lo_item.get("lo_name")
                        or "",
                        "object_type": lo_item.get("object_type"),
                        "lo_type": lo_item.get("lo_type"),
                    }
                )

    # Transformer that fetches survey answers for each poll
    @dlt.transformer(
        data_from=polls,
        write_disposition="replace",
        parallelized=True,
        name="survey_answers",
        columns={
            "course_id": {"data_type": "bigint", "nullable": True},
            "poll_id": {"data_type": "bigint", "nullable": True},
            "poll_title": {"data_type": "text", "nullable": True},
            "question_id": {"data_type": "bigint", "nullable": True},
            "question_type": {"data_type": "text", "nullable": True},
            "question_title": {"data_type": "text", "nullable": True},
            "answer": {"data_type": "text", "nullable": True},
            "date": {"data_type": "timestamp", "nullable": True},
        },
    )
    def survey_answers(poll_item: Dict[str, Any]) -> Iterator[Dict[str, Any]]:
        """Fetch all survey answers for a specific poll."""
        poll_id = poll_item["poll_id"]
        course_id = poll_item["course_id"]
        poll_title = poll_item["poll_title"]

        if not poll_id or not course_id:
            return

        survey_data = client.fetch_survey_answers_for_poll(poll_id, course_id)
        if not survey_data:
            return

        assert "answers" in survey_data, "no answers in survey data " + json.dumps(
            survey_data
        )
        assert isinstance(survey_data["answers"], list), "answers is not a list"
        assert "questions" in survey_data, "no questions in survey data"
        assert isinstance(survey_data["questions"], dict), "questions is not a dict"

        questions = survey_data["questions"]
        answers = survey_data["answers"]

        for answer in answers:
            if "answers" not in answer:
                continue
            date = normalize_date_field(answer.get("date"))

            answer_data = answer.get("answers", {})
            for question_id, answer_list in answer_data.items():
                for answer in answer_list:
                    yield {
                        "course_id": course_id,
                        "poll_id": poll_id,
                        "poll_title": poll_title,
                        "question_id": question_id,
                        "question_type": questions[question_id].get("type_quest"),
                        "question_title": questions[question_id].get("title_quest"),
                        "answer": answer,
                        "date": date,
                    }

    return [
        users,
        courses,
        user_fields,
        branches,
        groups,
        group_members,
        course_fields,
        learning_objects,
        learning_plans,
        learning_plan_enrollments,
        learning_plan_course_enrollments,
        course_enrollments,
        sessions,
        categories,
        certifications,
        external_training,
        polls,
        survey_answers,  # Standalone survey resource
    ]
