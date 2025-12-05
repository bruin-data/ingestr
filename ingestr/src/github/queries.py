# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

RATE_LIMIT = """
  rateLimit {
    limit
    cost
    remaining
    resetAt
  }
"""

ISSUES_QUERY = """
query($owner: String!, $name: String!, $issues_per_page: Int!, $first_reactions: Int!, $first_comments: Int!, $page_after: String) {
  repository(owner: $owner, name: $name) {
    %s(first: $issues_per_page, orderBy: {field: CREATED_AT, direction: DESC}, after: $page_after) {
      totalCount
      pageInfo {
        endCursor
        startCursor
      }
      nodes {
        # id
        number
        url
        title
        body
        author {login avatarUrl url}
        authorAssociation
        closed
        closedAt
        createdAt
        state
        updatedAt
        reactions(first: $first_reactions) {
          totalCount
          nodes {
            # id
            user {login avatarUrl url}
            content
            createdAt
          }
        }
        comments(first: $first_comments) {
          totalCount
          nodes {
            id
            url
            body
            author {avatarUrl login url}
            authorAssociation
            createdAt
            reactionGroups {content createdAt}
            # reactions(first: 0) {
            #   totalCount
            #   nodes {
            #     # id
            #     user {login avatarUrl url}
            #     content
            #     createdAt
            #   }
            # }
          }
        }
      }
    }
  }
  rateLimit {
    limit
    cost
    remaining
    resetAt
  }
}
"""

COMMENT_REACTIONS_QUERY = """
node_%s: node(id:"%s") {
     ... on IssueComment {
      id
      reactions(first: 100) {
        totalCount
        nodes {
            user {login avatarUrl url}
            content
            createdAt
          }
      }
    }
  }
"""

STARGAZERS_QUERY = """
query($owner: String!, $name: String!, $items_per_page: Int!, $page_after: String) {
  repository(owner: $owner, name: $name) {
    stargazers(first: $items_per_page, orderBy: {field: STARRED_AT, direction: DESC}, after: $page_after) {
      pageInfo {
        endCursor
        startCursor
      }
      edges {
        starredAt
        node {
          login
          avatarUrl
          url
        }
      }
    }
  }
  rateLimit {
    limit
    cost
    remaining
    resetAt
  }
}
"""
