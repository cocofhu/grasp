#!/usr/bin/env python3
"""Read a GitHub discussion using its repository credential or its public page."""

import json
import re
import subprocess
import sys
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import urljoin, urlsplit
from urllib.request import Request, urlopen

COMMENT = "fragment Text on DiscussionComment { url body author { login } }"
DISCUSSION = """
query($owner: String!, $repo: String!, $number: Int!, $after: String) {
  repository(owner: $owner, name: $repo) {
    discussion(number: $number) {
      url title body answer { ...Text }
      comments(first: 50, after: $after) {
        nodes { ...Text id replies(first: 50) { nodes { ...Text } pageInfo { hasNextPage endCursor } } }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}
""" + COMMENT
REPLIES = """
query($id: ID!, $after: String!) {
  node(id: $id) { ... on DiscussionComment {
    replies(first: 50, after: $after) { nodes { ...Text } pageInfo { hasNextPage endCursor } }
  } }
}
""" + COMMENT


def emit(kind, **fields):
    print(json.dumps({"kind": kind, **fields}, ensure_ascii=False))


def graphql(helper, repo_url, query, **variables):
    result = subprocess.run(
        [sys.executable, str(helper), "exec-cli", "gh", repo_url, "--",
         "api", "--hostname", "github.com", "graphql", "--input", "-"],
        input=json.dumps({"query": query, "variables": variables}), text=True,
        capture_output=True, check=True, timeout=60,
    )
    return json.loads(result.stdout)["data"]


def pages(connection, fetch_next):
    previous = None
    while True:
        yield from connection["nodes"]
        info = connection["pageInfo"]
        if not info["hasNextPage"]:
            return
        cursor = info["endCursor"]
        if not cursor or cursor == previous:
            raise ValueError("GitHub did not advance the discussion cursor")
        connection = fetch_next(cursor)
        previous = cursor


def read_thread(discussion, fetch_discussion, fetch_replies):
    emit("discussion", source="graphql", title=discussion["title"], url=discussion["url"], body=discussion["body"])
    if discussion["answer"]:
        emit("accepted_answer", **discussion["answer"])
    for comment in pages(discussion["comments"], lambda cursor: fetch_discussion(cursor)["comments"]):
        emit("comment", url=comment["url"], body=comment["body"], author=comment["author"])
        for reply in pages(comment["replies"], lambda cursor: fetch_replies(comment["id"], cursor)):
            emit("reply", inReplyTo=comment["url"], **reply)


class PublicPage(HTMLParser):
    """Keep the rendered posts and GitHub's link to the accepted post, including a nested reply."""

    def __init__(self):
        super().__init__()
        self.posts = []
        self.answer = self.anchor = self.parent = self.next_page = ""
        self.answer_html = ""
        self.schema = None
        self.parent_depth = 0
        self.body_tag = ""
        self.depth = 0
        self.text = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        classes = (attrs.get("class") or "").split()
        href = attrs.get("href") or ""
        element_id = attrs.get("id") or ""
        if tag == "script" and attrs.get("type") == "application/ld+json":
            self.schema = []
        if tag == "div":
            if self.parent_depth:
                self.parent_depth += 1
            elif re.fullmatch(r"child-comments-discussioncomment-\d+", element_id):
                self.parent = "#" + element_id.removeprefix("child-comments-")
                self.parent_depth = 1
        if re.fullmatch(r"discussioncomment-\d+", element_id):
            self.anchor = "#" + element_id
        if "js-discussions-goto-answer-button" in classes and re.fullmatch(r"#discussioncomment-\d+", href):
            self.answer = href
        if tag == "a" and ("next" in (attrs.get("rel") or "").split() or "next_page" in classes):
            self.next_page = href
        if not self.depth and "js-comment-body" in classes:
            self.body_tag = tag
            self.text = []
        if tag == self.body_tag:
            self.depth += 1
        if self.depth and tag in ("br", "p", "li", "pre", "blockquote"):
            self.text.append("\n")

    def handle_endtag(self, tag):
        if tag == "script" and self.schema is not None:
            try:
                data = json.loads("".join(self.schema))
                if isinstance(data, dict) and data.get("@type") == "QAPage":
                    answer = data["mainEntity"].get("acceptedAnswer") or {}
                    self.answer_html = answer.get("text", "")
                    fragment = urlsplit(answer.get("url", "")).fragment
                    if re.fullmatch(r"discussioncomment-\d+", fragment):
                        self.answer = "#" + fragment
            except (ValueError, KeyError, TypeError, AttributeError):
                pass  # The visible posts also carry the accepted answer link.
            self.schema = None
        if tag == "div" and self.parent_depth:
            self.parent_depth -= 1
            if not self.parent_depth:
                self.parent = ""
        if tag == self.body_tag:
            self.depth -= 1
            if not self.depth:
                self.posts.append((self.anchor, self.parent, "".join(self.text).strip()))
                self.body_tag = ""

    def handle_data(self, data):
        if self.schema is not None:
            self.schema.append(data)
        if self.depth:
            self.text.append(data)


def read_public(url):
    next_url = url
    seen = set()
    while next_url and next_url not in seen:
        seen.add(next_url)
        page = PublicPage()
        # An ordinary logged-out GitHub page. No Git/gh credential is sent.
        with urlopen(Request(next_url, headers={"Accept": "text/html"}), timeout=30) as response:
            canonical = response.geturl().split("?", 1)[0]
            page.feed(response.read().decode("utf-8", "replace"))
        page.close()
        if not page.posts:
            raise ValueError("GitHub did not return a readable public discussion")
        if next_url == url:
            emit("discussion", source="public_page", url=canonical, body=page.posts[0][2])
            if page.answer:
                answer = next((body for anchor, _, body in page.posts if anchor == page.answer), None)
                fields = {"body": answer} if answer else {"bodyHTML": page.answer_html}
                emit("accepted_answer", url=canonical + page.answer, **fields)
        for anchor, parent, body in page.posts:
            if anchor:
                if parent:
                    emit("reply", inReplyTo=canonical + parent, url=canonical + anchor, body=body)
                else:
                    emit("comment", url=canonical + anchor, body=body)
        candidate = urljoin(canonical, page.next_page)
        next_url = candidate if page.next_page and urlsplit(candidate)[:3] == urlsplit(canonical)[:3] else ""


def main(url):
    match = re.fullmatch(r"https://github\.com/([\w.-]+)/([\w.-]+)/discussions/([1-9]\d*)", url, re.ASCII)
    if not match or any(part in (".", "..") for part in match.group(1, 2)):
        raise ValueError("expected a GitHub discussion URL")
    owner, repo, number = match.groups()
    repo_url = f"https://github.com/{owner}/{repo}"
    helper = Path.home() / ".grasp/git-credentials/helper.py"

    def fetch_discussion(cursor=None):
        data = graphql(helper, repo_url, DISCUSSION, owner=owner, repo=repo, number=int(number), after=cursor)
        return (data["repository"] or {}).get("discussion")

    def fetch_replies(comment_id, cursor):
        return graphql(helper, repo_url, REPLIES, id=comment_id, after=cursor)["node"]["replies"]

    if helper.is_file():
        try:
            discussion = fetch_discussion()
        except (OSError, subprocess.SubprocessError, ValueError, KeyError):
            discussion = None
        if discussion:
            read_thread(discussion, fetch_discussion, fetch_replies)
            return
    read_public(url)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("usage: read-github-discussion.py https://github.com/OWNER/REPO/discussions/NUMBER")
    try:
        main(sys.argv[1])
    except (OSError, subprocess.SubprocessError, ValueError, KeyError, TypeError) as error:
        sys.exit(f"Could not read the discussion ({error}). For a private discussion, check the repository credential's Discussions read access.")
