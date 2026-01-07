import time
from typing import Any, Dict, Iterator, List, Optional

import pendulum
import requests

from ingestr.src.http_client import create_client as create_http_client

GRAPHQL_API_BASE_URL = "https://api.fireflies.ai/graphql"


def create_client() -> requests.Session:
    # - 429: Rate limit exceeded
    # - 500: Internal server error (INTERNAL_SERVER_ERROR)
    return create_http_client(retry_status_codes=[429, 500])


ACTIVE_MEETINGS_QUERY = """
query ActiveMeetings {
  active_meetings {
    id
    title
    organizer_email
    meeting_link
    start_time
    end_time
    privacy
    state
  }
}
"""

CHANNELS_QUERY = """
query Channels {
  channels {
    id
    title
    is_private
    created_by
    created_at
    updated_at
    members {
      user_id
      email
      name
    }
  }
}
"""

USERS_QUERY = """
query Users {
  users {
    user_id
    email
    name
    num_transcripts
    recent_transcript
    recent_meeting
    minutes_consumed
    is_admin
    integrations
    user_groups {
      id
      name
      handle
      members {
        user_id
        first_name
        last_name
        email
      }
    }
  }
}
"""

USER_GROUPS_QUERY = """
query UserGroups {
  user_groups {
    id
    name
    handle
    members {
      user_id
      first_name
      last_name
      email
    }
  }
}
"""

CONTACTS_QUERY = """
query Contacts {
  contacts {
    email
    name
    picture
    last_meeting_date
  }
}
"""

BITES_QUERY = """
query Bites($my_team: Boolean, $limit: Int, $skip: Int) {
  bites(my_team: $my_team, limit: $limit, skip: $skip){
    transcript_id
    name
    id
    thumbnail
    preview
    status
    summary
    user_id
    start_time
    end_time
    summary_status
    media_type
    created_at
    created_from {
      description
      duration
      id
      name
      type
    }
    captions {
      end_time
      index
      speaker_id
      speaker_name
      start_time
      text
    }
    sources {
      src
      type
    }
    privacies
    user {
      first_name
      last_name
      picture
      name
      id
    }
  }
}
"""

ANALYTICS_QUERY = """
query Analytics($startTime: String!, $endTime: String!) {
  analytics(start_time: $startTime, end_time: $endTime) {
    team {
      conversation {
        average_filler_words
        average_filler_words_diff_pct
        average_monologues_count
        average_monologues_count_diff_pct
        average_questions
        average_questions_diff_pct
        average_sentiments {
          negative_pct
          neutral_pct
          positive_pct
        }
        average_silence_duration
        average_silence_duration_diff_pct
        average_talk_listen_ratio
        average_words_per_minute
        longest_monologue_duration_sec
        longest_monologue_duration_diff_pct
        total_filler_words
        total_filler_words_diff_pct
        total_meeting_notes_count
        total_meetings_count
        total_monologues_count
        total_monologues_diff_pct
        teammates_count
        total_questions
        total_questions_diff_pct
        total_silence_duration
        total_silence_duration_diff_pct
      }
      meeting {
        count
        count_diff_pct
        duration
        duration_diff_pct
        average_count
        average_count_diff_pct
        average_duration
        average_duration_diff_pct
      }
    }
    users {
      user_id
      user_name
      user_email
      conversation {
        talk_listen_pct
        talk_listen_ratio
        total_silence_duration
        total_silence_duration_compare_to
        total_silence_pct
        total_silence_ratio
        total_speak_duration
        total_speak_duration_with_user
        total_word_count
        user_filler_words
        user_filler_words_compare_to
        user_filler_words_diff_pct
        user_longest_monologue_sec
        user_longest_monologue_compare_to
        user_longest_monologue_diff_pct
        user_monologues_count
        user_monologues_count_compare_to
        user_monologues_count_diff_pct
        user_questions
        user_questions_compare_to
        user_questions_diff_pct
        user_speak_duration
        user_word_count
        user_words_per_minute
        user_words_per_minute_compare_to
        user_words_per_minute_diff_pct
      }
      meeting {
        count
        count_diff
        count_diff_compared_to
        count_diff_pct
        duration
        duration_diff
        duration_diff_compared_to
        duration_diff_pct
      }
    }
  }
}
"""

TRANSCRIPTS_QUERY = """
query Transcripts(
  $limit: Int
  $skip: Int
  $fromDate: DateTime
  $toDate: DateTime
) {
  transcripts(
    limit: $limit
    skip: $skip
    fromDate: $fromDate
    toDate: $toDate
  ) {
    id
    title
    date
    duration
    transcript_url
    audio_url
    video_url
    meeting_link
    host_email
    organizer_email
    participants
    fireflies_users
    calendar_id
    cal_id
    calendar_type
    channels {
      id
    }
    speakers {
      id
      name
    }
    analytics {
      sentiments {
        negative_pct
        neutral_pct
        positive_pct
      }
      categories {
        questions
        date_times
        metrics
        tasks
      }
      speakers {
        speaker_id
        name
        duration
        duration_pct
        word_count
        words_per_minute
        longest_monologue
        monologues_count
        filler_words
        questions
      }
    }
    sentences {
      index
      speaker_name
      speaker_id
      text
      raw_text
      start_time
      end_time
      ai_filters {
        task
        pricing
        metric
        question
        date_and_time
        text_cleanup
        sentiment
      }
    }
    meeting_info {
      fred_joined
      silent_meeting
      summary_status
    }
    meeting_attendees {
      displayName
      email
      phoneNumber
      name
      location
    }
    meeting_attendance {
      name
      join_time
      leave_time
    }
    summary {
      keywords
      action_items
      outline
      shorthand_bullet
      overview
      bullet_gist
      gist
      short_summary
      short_overview
      meeting_type
      topics_discussed
      transcript_chapters
    }
    user {
      user_id
      email
      name
      num_transcripts
      recent_meeting
      minutes_consumed
      is_admin
      integrations
    }
    apps_preview {
      outputs {
        transcript_id
        user_id
        app_id
        created_at
        title
        prompt
        response
      }
    }
    channels {
      id
    }
  }
}
"""


class FirefliesAPI:
    def __init__(self, api_key: str):
        self.api_key = api_key
        self.headers = {
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        }
        self.client = create_client()

    def fetch_active_meetings(self) -> Iterator[List[dict]]:
        response = self.client.post(
            url=GRAPHQL_API_BASE_URL,
            json={"query": ACTIVE_MEETINGS_QUERY},
            headers=self.headers,
        )

        if response.status_code != 200:
            error_data = response.json() if response.content else {}
            raise ValueError(
                f"Fireflies API Error: {error_data.get('message', 'Unknown error')}"
            )

        result = response.json()

        if "errors" in result:
            error_messages = [
                error.get("message", "Unknown error") for error in result["errors"]
            ]
            raise ValueError(f"Fireflies GraphQL Error: {', '.join(error_messages)}")

        active_meetings = result.get("data", {}).get("active_meetings", [])

        if active_meetings:
            yield active_meetings

    def fetch_analytics(
        self,
        from_date: Optional[str] = None,
        to_date: Optional[str] = None,
    ) -> Iterator[List[dict]]:
        MAX_DAYS = 30

        start: pendulum.DateTime = (
            pendulum.parse(from_date)  # type: ignore[assignment]
            if from_date
            else pendulum.datetime(1970, 1, 1, tz="UTC")
        )
        end: pendulum.DateTime = (
            pendulum.parse(to_date)  # type: ignore[assignment]
            if to_date
            else pendulum.now(tz="UTC")
        )

        total_days = (end - start).days

        if total_days <= MAX_DAYS:
            yield from self._fetch_analytics_chunk(
                start.to_iso8601_string(), end.to_iso8601_string()
            )
        else:
            current_start: pendulum.DateTime = start

            while current_start < end:
                chunk_end: pendulum.DateTime = current_start.add(days=MAX_DAYS)
                if chunk_end > end:
                    chunk_end = end

                chunk_start_str = current_start.to_iso8601_string()
                chunk_end_str = chunk_end.to_iso8601_string()

                yield from self._fetch_analytics_chunk(chunk_start_str, chunk_end_str)

                current_start = chunk_end.add(days=1)

                time.sleep(0.5)

    def _fetch_analytics_chunk(
        self, start_time: str, end_time: str
    ) -> Iterator[List[dict]]:
        variables = {
            "startTime": start_time,
            "endTime": end_time,
        }

        response = self.client.post(
            url=GRAPHQL_API_BASE_URL,
            json={"query": ANALYTICS_QUERY, "variables": variables},
            headers=self.headers,
        )

        if response.status_code != 200:
            error_data = response.json() if response.content else {}
            raise ValueError(
                f"Fireflies API Error: {error_data.get('message', 'Unknown error')}"
            )

        result = response.json()

        if "errors" in result:
            error_messages = [
                error.get("message", "Unknown error") for error in result["errors"]
            ]

            data = result.get("data", {})
            if data and data.get("analytics"):
                print(
                    f"Warning: GraphQL errors encountered but data available: {', '.join(error_messages)}"
                )
            else:
                error_details = [str(error) for error in result["errors"]]
                raise ValueError(
                    f"Fireflies GraphQL Error: {', '.join(error_messages)}\n"
                    f"Error details: {', '.join(error_details)}"
                )

        analytics = result.get("data", {}).get("analytics", {})

        if analytics:
            analytics["start_time"] = start_time
            analytics["end_time"] = end_time
            yield [analytics]

    def fetch_channels(self) -> Iterator[List[dict]]:
        response = self.client.post(
            url=GRAPHQL_API_BASE_URL,
            json={"query": CHANNELS_QUERY},
            headers=self.headers,
        )

        if response.status_code != 200:
            error_data = response.json() if response.content else {}
            raise ValueError(
                f"Fireflies API Error: {error_data.get('message', 'Unknown error')}"
            )

        result = response.json()

        if "errors" in result:
            error_messages = [
                error.get("message", "Unknown error") for error in result["errors"]
            ]
            raise ValueError(f"Fireflies GraphQL Error: {', '.join(error_messages)}")

        channels = result.get("data", {}).get("channels", [])

        if channels:
            yield channels

    def fetch_users(self) -> Iterator[List[dict]]:
        response = self.client.post(
            url=GRAPHQL_API_BASE_URL,
            json={"query": USERS_QUERY},
            headers=self.headers,
        )

        if response.status_code != 200:
            error_data = response.json() if response.content else {}
            raise ValueError(
                f"Fireflies API Error: {error_data.get('message', 'Unknown error')}"
            )

        result = response.json()

        if "errors" in result:
            error_messages = [
                error.get("message", "Unknown error") for error in result["errors"]
            ]
            raise ValueError(f"Fireflies GraphQL Error: {', '.join(error_messages)}")

        users = result.get("data", {}).get("users", [])

        if users:
            yield users

    def fetch_user_groups(self) -> Iterator[List[dict]]:
        response = self.client.post(
            url=GRAPHQL_API_BASE_URL,
            json={"query": USER_GROUPS_QUERY},
            headers=self.headers,
        )

        if response.status_code != 200:
            error_data = response.json() if response.content else {}
            raise ValueError(
                f"Fireflies API Error: {error_data.get('message', 'Unknown error')}"
            )

        result = response.json()

        if "errors" in result:
            error_messages = [
                error.get("message", "Unknown error") for error in result["errors"]
            ]
            raise ValueError(f"Fireflies GraphQL Error: {', '.join(error_messages)}")

        user_groups = result.get("data", {}).get("user_groups", [])

        if user_groups:
            yield user_groups

    def fetch_contacts(self) -> Iterator[List[dict]]:
        response = self.client.post(
            url=GRAPHQL_API_BASE_URL,
            json={"query": CONTACTS_QUERY},
            headers=self.headers,
        )

        if response.status_code != 200:
            error_data = response.json() if response.content else {}
            raise ValueError(
                f"Fireflies API Error: {error_data.get('message', 'Unknown error')}"
            )

        result = response.json()

        if "errors" in result:
            error_messages = [
                error.get("message", "Unknown error") for error in result["errors"]
            ]
            raise ValueError(f"Fireflies GraphQL Error: {', '.join(error_messages)}")

        contacts = result.get("data", {}).get("contacts", [])

        if contacts:
            yield contacts

    def fetch_bites(self) -> Iterator[List[dict]]:
        PAGE_LIMIT = 50
        skip_offset = 0
        total_fetched = 0

        while True:
            variables: Dict[str, Any] = {
                "my_team": True,
                "limit": PAGE_LIMIT,
            }

            if skip_offset > 0:
                variables["skip"] = skip_offset

            response = self.client.post(
                url=GRAPHQL_API_BASE_URL,
                json={"query": BITES_QUERY, "variables": variables},
                headers=self.headers,
            )

            if response.status_code != 200:
                error_data = response.json() if response.content else {}
                raise ValueError(
                    f"Fireflies API Error: {error_data.get('message', 'Unknown error')}"
                )

            result = response.json()

            bites = result.get("data", {}).get("bites", [])

            bites_with_errors: Dict[int, List[str]] = {}
            if "errors" in result:
                if "data" in result and bites:
                    for error in result["errors"]:
                        error_path = error.get("path", [])
                        if len(error_path) >= 2 and error_path[0] == "bites":
                            bite_idx = error_path[1]
                            if isinstance(bite_idx, int) and bite_idx < len(bites):
                                field_name = (
                                    error_path[2] if len(error_path) > 2 else "unknown"
                                )
                                if bite_idx not in bites_with_errors:
                                    bites_with_errors[bite_idx] = []
                                bites_with_errors[bite_idx].append(field_name)
                else:
                    error_messages = [
                        error.get("message", "Unknown error")
                        for error in result["errors"]
                    ]
                    error_details = [str(error) for error in result["errors"]]
                    raise ValueError(
                        f"Fireflies GraphQL Error: {', '.join(error_messages)}\n"
                        f"Error details: {', '.join(error_details)}"
                    )

            for idx, bite in enumerate(bites):
                if idx in bites_with_errors:
                    error_fields = ", ".join(bites_with_errors[idx])
                    bite["error"] = error_fields
                else:
                    bite["error"] = None

            fetched_count = len(bites)
            total_fetched += fetched_count

            if not bites:
                break

            yield bites

            if fetched_count < PAGE_LIMIT:
                break

            time.sleep(0.5)

            skip_offset += fetched_count

    def fetch_transcripts(
        self,
        from_date: Optional[str] = None,
        to_date: Optional[str] = None,
    ) -> Iterator[List[dict]]:
        PAGE_LIMIT = 50
        skip_offset = 0
        total_fetched = 0

        while True:
            variables: Dict[str, Any] = {
                "skip": skip_offset,
                "limit": PAGE_LIMIT,
            }

            if from_date is not None:
                variables["fromDate"] = from_date
            if to_date is not None:
                variables["toDate"] = to_date

            response = self.client.post(
                url=GRAPHQL_API_BASE_URL,
                json={"query": TRANSCRIPTS_QUERY, "variables": variables},
                headers=self.headers,
            )

            if response.status_code != 200:
                error_data = response.json() if response.content else {}
                raise ValueError(
                    f"Fireflies API Error: {error_data.get('message', 'Unknown error')}"
                )

            result = response.json()

            transcripts = result.get("data", {}).get("transcripts", [])

            transcripts_with_errors: Dict[int, List[str]] = {}
            if "errors" in result:
                if "data" in result and transcripts:
                    for error in result["errors"]:
                        error_path = error.get("path", [])
                        if len(error_path) >= 2 and error_path[0] == "transcripts":
                            transcript_idx = error_path[1]
                            if isinstance(transcript_idx, int) and transcript_idx < len(
                                transcripts
                            ):
                                field_name = (
                                    error_path[2] if len(error_path) > 2 else "unknown"
                                )
                                if transcript_idx not in transcripts_with_errors:
                                    transcripts_with_errors[transcript_idx] = []
                                transcripts_with_errors[transcript_idx].append(
                                    field_name
                                )
                else:
                    error_messages = [
                        error.get("message", "Unknown error")
                        for error in result["errors"]
                    ]
                    error_details = [str(error) for error in result["errors"]]
                    raise ValueError(
                        f"Fireflies GraphQL Error: {', '.join(error_messages)}\n"
                        f"Error details: {', '.join(error_details)}"
                    )

            for idx, transcript in enumerate(transcripts):
                if idx in transcripts_with_errors:
                    error_fields = ", ".join(transcripts_with_errors[idx])
                    transcript["error"] = error_fields
                else:
                    transcript["error"] = None

            fetched_count = len(transcripts)
            total_fetched += fetched_count

            if not transcripts:
                break

            yield transcripts

            if fetched_count < PAGE_LIMIT:
                break

            time.sleep(0.5)

            skip_offset += fetched_count
