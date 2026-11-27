#!/usr/bin/env python3
"""Tiny Jira/GitLab API mocks for the hop lab.

The endpoints cover what hop uses: search, get and create. State is in-memory
and resets when the container restarts. Authentication is checked but any
non-empty token is accepted, so the lab never needs real credentials.
"""

import argparse
import json
import re
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, unquote, urlparse

LOCK = threading.Lock()
JIRA_ISSUES = {
    "LAB-1": {
        "key": "LAB-1",
        "summary": "Checkout timeout in lab",
        "status": "Open",
        "updated": "2026-11-25T10:00:00.000+0000",
    },
    "LAB-2": {
        "key": "LAB-2",
        "summary": "SSH lab host unhealthy",
        "status": "In Progress",
        "updated": "2026-11-26T09:30:00.000+0000",
    },
}
GITLAB_ISSUES = [
    {
        "iid": 1,
        "title": "Checkout timeout in lab",
        "state": "opened",
        "description": "Synthetic GitLab issue for hop.",
        "updated_at": "2026-11-25T10:00:00Z",
    },
    {
        "iid": 2,
        "title": "Container logs missing",
        "state": "opened",
        "description": "Synthetic GitLab issue for hop.",
        "updated_at": "2026-11-26T09:30:00Z",
    },
]


def compact(value):
    return re.sub(r"\s+", " ", str(value or "")).strip().lower()


def jira_issue(key):
    issue = JIRA_ISSUES[key]
    return {
        "key": issue["key"],
        "fields": {
            "summary": issue["summary"],
            "status": {"name": issue["status"]},
            "updated": issue["updated"],
        },
    }


def gitlab_issue(item, project):
    return {
        "iid": item["iid"],
        "project_id": 1,
        "title": item["title"],
        "description": item.get("description", ""),
        "state": item["state"],
        "web_url": f"{PUBLIC_URL}/{project}/-/issues/{item['iid']}",
        "updated_at": item.get("updated_at", ""),
        "references": {
            "short": f"#{item['iid']}",
            "relative": f"#{item['iid']}",
            "full": f"{project}#{item['iid']}",
        },
    }


class Handler(BaseHTTPRequestHandler):
    server_version = "hop-lab/1.0"

    def log_message(self, fmt, *args):
        return

    def send_json(self, status, payload):
        data = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def send_text(self, status, body):
        data = body.encode()
        self.send_response(status)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def authorized(self):
        if KIND == "jira":
            return bool(self.headers.get("Authorization", "").strip())
        return bool(
            self.headers.get("PRIVATE-TOKEN", "").strip()
            or self.headers.get("Authorization", "").strip()
        )

    def body(self):
        length = int(self.headers.get("Content-Length", "0") or 0)
        if length <= 0:
            return {}
        try:
            return json.loads(self.rfile.read(length))
        except Exception:
            return {}

    def do_GET(self):
        if self.path.split("?", 1)[0] in ("/healthz", "/-/health", "/health"):
            self.send_text(200, "ok\n")
            return
        if not self.authorized():
            self.send_json(401, {"message": "missing token"})
            return
        parsed = urlparse(self.path)
        path = unquote(parsed.path)
        query = parse_qs(parsed.query)
        if KIND == "jira":
            self.jira_get(path, query)
        else:
            self.gitlab_get(path, query)

    def jira_get(self, path, query):
        if path == "/rest/api/2/search":
            jql = compact(query.get("jql", [""])[0])
            match = re.search(r'text ~ "([^"]*)"', jql)
            needle = compact(match.group(1)) if match else ""
            with LOCK:
                issues = [
                    jira_issue(key)
                    for key, issue in sorted(JIRA_ISSUES.items())
                    if not needle
                    or needle in compact(issue["summary"])
                    or needle in compact(issue["key"])
                ]
            self.send_json(200, {"issues": issues, "total": len(issues)})
            return
        match = re.match(r"^/rest/api/2/issue/([^/]+)$", path)
        if match:
            key = unquote(match.group(1)).upper()
            with LOCK:
                if key in JIRA_ISSUES:
                    self.send_json(200, jira_issue(key))
                    return
            self.send_json(404, {"errorMessages": ["issue not found"]})
            return
        match = re.match(r"^/browse/([^/]+)$", path)
        if match:
            self.send_text(200, f"Jira mock issue {match.group(1)}\n")
            return
        self.send_json(404, {"error": "not found"})

    def gitlab_get(self, path, query):
        if path == "/api/v4/issues":
            self.gitlab_search(query, PROJECT)
            return
        match = re.match(r"^/api/v4/projects/(.+)/issues/([0-9]+)$", path)
        if match:
            project = match.group(1).strip("/")
            iid = int(match.group(2))
            with LOCK:
                for item in GITLAB_ISSUES:
                    if item["iid"] == iid:
                        self.send_json(200, gitlab_issue(item, project))
                        return
            self.send_json(404, {"message": "404 Not found"})
            return
        match = re.match(r"^/api/v4/projects/(.+)/issues$", path)
        if match:
            self.gitlab_search(query, match.group(1).strip("/"))
            return
        match = re.match(r"^/(.+)/-/issues/([0-9]+)$", path)
        if match:
            self.send_text(200, f"GitLab mock issue {match.group(1)}#{match.group(2)}\n")
            return
        self.send_json(404, {"message": "404 Not found"})

    def gitlab_search(self, query, project):
        needle = compact(query.get("search", [""])[0])
        with LOCK:
            items = [
                item
                for item in GITLAB_ISSUES
                if not needle
                or needle in compact(item["title"])
                or needle in compact(f"#{item['iid']}")
            ]
            payload = [gitlab_issue(item, project) for item in items]
        self.send_json(200, payload)

    def do_POST(self):
        if not self.authorized():
            self.send_json(401, {"message": "missing token"})
            return
        path = unquote(urlparse(self.path).path)
        payload = self.body()
        if KIND == "jira":
            self.jira_post(path, payload)
        else:
            self.gitlab_post(path, payload)

    def jira_post(self, path, payload):
        if path != "/rest/api/2/issue":
            self.send_json(404, {"error": "not found"})
            return
        fields = payload.get("fields") or {}
        project = str((fields.get("project") or {}).get("key") or "LAB").upper()
        summary = str(fields.get("summary") or "Untitled")
        with LOCK:
            number = 1 + sum(1 for key in JIRA_ISSUES if key.startswith(project + "-"))
            key = f"{project}-{number}"
            JIRA_ISSUES[key] = {
                "key": key,
                "summary": summary,
                "status": "Open",
                "updated": "2026-11-27T08:00:00.000+0000",
            }
        self.send_json(201, {"key": key, "self": f"{PUBLIC_URL}/rest/api/2/issue/{key}"})

    def gitlab_post(self, path, payload):
        match = re.match(r"^/api/v4/projects/(.+)/issues$", path)
        if not match:
            self.send_json(404, {"message": "404 Not found"})
            return
        project = match.group(1).strip("/")
        title = str(payload.get("title") or "Untitled")
        with LOCK:
            iid = max((item["iid"] for item in GITLAB_ISSUES), default=0) + 1
            item = {
                "iid": iid,
                "title": title,
                "state": "opened",
                "description": payload.get("description", ""),
                "updated_at": "2026-11-27T08:00:00Z",
            }
            GITLAB_ISSUES.append(item)
        self.send_json(201, gitlab_issue(item, project))


def main():
    global KIND, PROJECT, PUBLIC_URL
    parser = argparse.ArgumentParser()
    parser.add_argument("--kind", choices=("jira", "gitlab"), required=True)
    parser.add_argument("--port", type=int, default=8080)
    parser.add_argument("--public-url", default="")
    args = parser.parse_args()
    KIND = args.kind
    PROJECT = "lab/hop"
    PUBLIC_URL = (args.public_url or f"http://127.0.0.1:{args.port}").rstrip("/")
    server = ThreadingHTTPServer(("0.0.0.0", args.port), Handler)
    print(f"hop lab {KIND} listening on :{args.port} as {PUBLIC_URL}", flush=True)
    server.serve_forever()


KIND = "jira"
PROJECT = "lab/hop"
PUBLIC_URL = ""

if __name__ == "__main__":
    main()
