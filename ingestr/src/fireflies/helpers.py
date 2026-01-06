from typing import Any, Dict, Iterator, List, Literal, Optional

import requests
from dlt.sources.helpers.requests import Client

GRAPHQL_API_BASE_URL = "https://api.fireflies.ai/graphql"


def retry_on_limit(
    response: requests.Response | None, exception: BaseException | None
) -> bool:
    if response is None:
        return False
    return response.status_code == 429


def create_client() -> requests.Session:
    return Client(
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
    ).session


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

CHANNEL_QUERY = """
query Channel($channelId: ID!) {
  channel(id: $channelId) {
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

USER_QUERY = """
query User($userId: String) {
  user(id: $userId) {
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

TRANSCRIPT_QUERY = """
query Transcript($transcriptId: String!) {
  transcript(id: $transcriptId) {
    id
    dateString
    privacy
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
        word_count
        longest_monologue
        monologues_count
        filler_words
        questions
        duration_pct
        words_per_minute
      }
    }
    speakers {
      id
      name
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
    title
    host_email
    organizer_email
    calendar_id
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
    fireflies_users
    participants
    date
    transcript_url
    audio_url
    video_url
    duration
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
    cal_id
    calendar_type
    meeting_info {
      fred_joined
      silent_meeting
      summary_status
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
    meeting_link
    channels {
      id
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

BITE_QUERY = """
query Bite($biteId: ID!) {
  bite(id: $biteId) {
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
query Transcripts($limit: Int, $skip: Int) {
  transcripts(limit: $limit, skip: $skip) {
    id
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
        word_count
        longest_monologue
        monologues_count
        filler_words
        questions
        duration_pct
        words_per_minute
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
    title
    speakers {
      id
      name
    }
    host_email
    organizer_email
    meeting_info {
      fred_joined
      silent_meeting
      summary_status
    }
    calendar_id
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
    fireflies_users
    participants
    date
    transcript_url
    audio_url
    video_url
    duration
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
    cal_id
    calendar_type
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
    meeting_link
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

    def fetch_analytics(self, start_time: str, end_time: str) -> Iterator[List[dict]]:
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
            raise ValueError(f"Fireflies GraphQL Error: {', '.join(error_messages)}")

        analytics = result.get("data", {}).get("analytics", {})

        if analytics:
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

    def fetch_channel(self, channel_id: str) -> Iterator[List[dict]]:
        variables = {
            "channelId": channel_id,
        }

        response = self.client.post(
            url=GRAPHQL_API_BASE_URL,
            json={"query": CHANNEL_QUERY, "variables": variables},
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

        channel = result.get("data", {}).get("channel")

        if channel:
            yield [channel]

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

    def fetch_user(self, user_id: str) -> Iterator[List[dict]]:
        variables = {
            "userId": user_id,
        }

        response = self.client.post(
            url=GRAPHQL_API_BASE_URL,
            json={"query": USER_QUERY, "variables": variables},
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

        user = result.get("data", {}).get("user")

        if user:
            yield [user]

    def fetch_transcript(self, transcript_id: str) -> Iterator[List[dict]]:
        variables = {
            "transcriptId": transcript_id,
        }

        response = self.client.post(
            url=GRAPHQL_API_BASE_URL,
            json={"query": TRANSCRIPT_QUERY, "variables": variables},
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

        transcript = result.get("data", {}).get("transcript")

        if transcript:
            yield [transcript]

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
        """
        Fetch all bites from Fireflies API with automatic pagination.
        API maximum limit is 50 per request.
        """
        MAX_LIMIT = 10
        skip_offset = 0
        
        while True:
            variables: Dict[str, Any] = {
                "my_team": True,
                "limit": MAX_LIMIT,
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

            if "errors" in result:
                error_messages = [
                    error.get("message", "Unknown error") for error in result["errors"]
                ]
                raise ValueError(f"Fireflies GraphQL Error: {', '.join(error_messages)}")

            bites = result.get("data", {}).get("bites", [])

            if not bites:
                break

            yield bites

            # If we got fewer results than requested, we've reached the end
            if len(bites) < MAX_LIMIT:
                break

            # Increment skip for next page
            skip_offset += len(bites)


    def fetch_bite(self, bite_id: str) -> Iterator[List[dict]]:
        variables = {
            "biteId": bite_id,
        }

        response = self.client.post(
            url=GRAPHQL_API_BASE_URL,
            json={"query": BITE_QUERY, "variables": variables},
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

        bite = result.get("data", {}).get("bite")

        if bite:
            yield [bite]

    def fetch_transcripts(self) -> Iterator[List[dict]]:
        """
        Fetch all transcripts from Fireflies API with automatic pagination.
        API maximum limit is 50 per request.
        """
        MAX_LIMIT = 10
        skip_offset = 0
        
        while True:
            variables: Dict[str, Any] = {
                "limit": MAX_LIMIT,
            }
            
            if skip_offset > 0:
                variables["skip"] = skip_offset

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

            if "errors" in result:
                error_messages = [
                    error.get("message", "Unknown error") for error in result["errors"]
                ]
                raise ValueError(f"Fireflies GraphQL Error: {', '.join(error_messages)}")

            transcripts = result.get("data", {}).get("transcripts", [])

            if not transcripts:
                break

            yield transcripts

            # If we got fewer results than requested, we've reached the end
            if len(transcripts) < MAX_LIMIT:
                break

            # Increment skip for next page
            skip_offset += len(transcripts)
