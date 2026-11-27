#!/usr/bin/env python3
"""Offline coverage for the discussion reader emitted by chat and read_reference."""

import importlib.util
import io
import json
import os
import shutil
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch
from urllib.error import HTTPError

SCRIPTS = Path(__file__).resolve().parents[1] / "sandbox/scripts"
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("discussion_reader", SCRIPTS / "read-github-discussion.py")
reader = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reader)
URL = "https://github.com/acme/second/discussions/42"


def read():
    out = io.StringIO()
    with redirect_stdout(out):
        reader.main(URL)
    return [json.loads(line) for line in out.getvalue().splitlines()]


def connection(nodes, next_cursor=None):
    return {"nodes": nodes, "pageInfo": {"hasNextPage": bool(next_cursor), "endCursor": next_cursor}}


def post(link, body):
    return {"author": {"login": "contributor"}, "body": body, "url": URL + "#" + link}


class ReaderTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        env = patch.dict(os.environ, {"HOME": tmp.name, "PATH": tmp.name + os.pathsep + os.environ["PATH"],
                                      "GH_TOKEN": "ambient-fixture"})
        env.start()
        self.addCleanup(env.stop)

    def test_private_thread_uses_exact_repo_for_answer_comments_and_later_replies(self):
        directory = self.root / ".grasp/git-credentials"
        directory.mkdir(parents=True)
        shutil.copyfile(SCRIPTS / "git-credentials.py", directory / "helper.py")
        entries = []
        for i, repo in enumerate(["first", "second"]):
            entries.append({"index": i, "id": repo, "kind": "github_https", "preferred": i == 0,
                            "scope": f"https://github.com/acme/{repo}"})
            (directory / f"{i}.secret").write_text(f"fixture-{repo}")
        (directory / "manifest.json").write_text(json.dumps(entries))

        answer = post("reply-2", "Adopted answer")
        parent = dict(post("comment-1", "First comment"), id="commentId",
                      replies=connection([post("reply-1", "Earlier reply")], "replies-next"))
        later = dict(post("comment-2", "Later comment"), id="later", replies=connection([]))

        def discussion(comments):
            return {"repository": {"discussion": {"url": URL, "title": "Decision", "body": "Question",
                                                  "answer": answer, "comments": comments}}}

        fixtures = {"start": discussion(connection([parent], "comments-next")),
                    "comments-next": discussion(connection([later])),
                    "replies-next": {"node": {"replies": connection([answer])}}}
        (self.root / "responses.json").write_text(json.dumps(fixtures))
        gh = self.root / "gh"
        gh.write_text("""#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
assert os.environ["GH_TOKEN"] == "fixture-second"
assert sys.argv[1:] == ["api", "--hostname", "github.com", "graphql", "--input", "-"]
request = json.load(sys.stdin)
query, variables = request["query"], request["variables"]
cursor = variables["after"] or "start"
if "id" in variables:
    assert variables["id"] == "commentId" and "replies(first: 50, after: $after)" in query
else:
    assert variables["owner"] == "acme" and variables["repo"] == "second" and variables["number"] == 42
    assert "answer {" in query and "comments(first: 50, after: $after)" in query and "replies(first: 50)" in query
print(json.dumps({"data": json.loads((Path.home() / "responses.json").read_text())[cursor]}))
""")
        gh.chmod(0o755)
        with patch.object(reader, "urlopen", side_effect=AssertionError("unexpected public fallback")):
            rows = read()
        self.assertEqual([r["body"] for r in rows], ["Question", "Adopted answer", "First comment", "Earlier reply",
                                                      "Adopted answer", "Later comment"])
        self.assertEqual(rows[1]["kind"], "accepted_answer")
        self.assertEqual(rows[4]["inReplyTo"], parent["url"])

    def test_public_pages_work_without_gh_and_keep_nested_answer(self):
        first = """
<div class="js-comment-body"><p>Question</p></div>
<a class="js-discussions-goto-answer-button" href="#discussioncomment-2">Answered</a>
<div id="discussioncomment-1"><div class="js-comment-body">Parent</div>
  <div id="child-comments-discussioncomment-1"><div id="discussioncomment-2">
    <div class="js-comment-body">Accepted reply</div></div></div></div>
<a rel="next" href="?page=2">Next</a>
"""
        second = '<div id="discussioncomment-3"><div class="js-comment-body">More detail</div></div>'

        def open_public(req, timeout):
            self.assertFalse(req.has_header("Authorization"))
            self.assertIn(req.full_url, [URL, URL + "?page=2"])
            response = io.BytesIO((first if req.full_url == URL else second).encode())
            response.geturl = lambda: req.full_url
            return response

        with patch.object(reader, "urlopen", side_effect=open_public) as fetch, patch.object(
                reader, "graphql", side_effect=AssertionError("should be signed out")):
            rows = read()
        self.assertEqual(fetch.call_count, 2)
        self.assertEqual(rows[0]["source"], "public_page")
        self.assertEqual([r["kind"] for r in rows], ["discussion", "accepted_answer", "comment", "reply", "comment"])
        self.assertEqual(rows[1]["body"], "Accepted reply")
        self.assertEqual(rows[3]["inReplyTo"], URL + "#discussioncomment-1")
        self.assertEqual(rows[-1]["body"], "More detail")

    def test_no_matching_credential_uses_only_the_public_page(self):
        directory = self.root / ".grasp/git-credentials"
        directory.mkdir(parents=True)
        shutil.copyfile(SCRIPTS / "git-credentials.py", directory / "helper.py")
        (directory / "manifest.json").write_text("[]")
        gh = self.root / "gh"
        gh.write_text('#!/bin/sh\ntouch "$HOME/wrong-credential"\nexit 1\n')
        gh.chmod(0o755)
        with patch.object(reader, "urlopen", side_effect=HTTPError(URL, 404, "Not found", {}, None)):
            with self.assertRaises(HTTPError):
                read()
        self.assertFalse((self.root / "wrong-credential").exists())

    def test_public_answer_outside_the_rendered_page_keeps_its_body(self):
        answer_url = URL + "#discussioncomment-200"
        schema = {"@type": "QAPage", "mainEntity": {"acceptedAnswer": {"url": answer_url, "text": "<p>Answer</p>"}}}
        html = '<script type="application/ld+json">' + json.dumps(schema) + '</script><div class="js-comment-body">Question</div>'
        response = io.BytesIO(html.encode())
        response.geturl = lambda: URL
        with patch.object(reader, "urlopen", return_value=response):
            rows = read()
        self.assertEqual(rows[1], {"kind": "accepted_answer", "url": answer_url, "bodyHTML": "<p>Answer</p>"})


if __name__ == "__main__":
    unittest.main()
