#!/usr/bin/env python3
"""Smoke client for grok-web-to-api OpenAI-compatible surface."""

import json
import os
import urllib.request

BASE = os.environ.get("GROK_API_BASE", "http://127.0.0.1:4982")


def get(path: str):
    with urllib.request.urlopen(BASE + path) as r:
        return json.load(r)


def post(path: str, body: dict):
    data = json.dumps(body).encode()
    req = urllib.request.Request(
        BASE + path,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req) as r:
            return r.status, json.load(r)
    except urllib.error.HTTPError as e:
        return e.code, json.load(e)


def main():
    print("health:", get("/health"))
    print("models:", get("/openai/v1/models"))
    code, resp = post(
        "/openai/v1/chat/completions",
        {
            "model": "grok-3",
            "messages": [{"role": "user", "content": "hello"}],
            "stream": False,
        },
    )
    print("chat status:", code)
    print("chat body:", json.dumps(resp, indent=2))


if __name__ == "__main__":
    main()
