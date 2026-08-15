#!/usr/bin/env python3

import json
import re
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse


PRIVATE_SENTINEL = "SMOKE_PRIVATE_RAW_SENTINEL"


def model_output(payload):
    model = str(payload.get("model", ""))
    messages = payload.get("messages", [])
    joined = "\n".join(str(message.get("content", "")) for message in messages)
    if "planner" in model:
        return {
            "decision_summary": "Plan one bounded synthetic Chatlog lookup.",
            "tool_calls": [{
                "tool": "search_chatlog",
                "arguments": {
                    "time_from": "2026-08-13T00:00:00Z",
                    "time_to": "2026-08-13T23:59:59Z",
                    "talker_ref": "smoke-conversation",
                    "keyword": "synthetic",
                    "limit": 10,
                },
            }],
        }
    evidence = re.findall(r"\[evidence:([^\]]+)\]", joined)
    if not evidence:
        raise ValueError("model stage did not receive promoted evidence")
    evidence_id = evidence[0]
    if "extractor" in model:
        return {
            "decision_summary": "Extract one bounded synthetic fact.",
            "facts": [{
                "fact_id": "fact-smoke",
                "kind": "timeline_event",
                "summary": "A synthetic event was observed.",
                "status": "observed",
                "occurred_at": "2026-08-13T00:01:00Z",
                "evidence_ids": [evidence_id],
                "confidence": 1,
                "review_state": "verified",
            }],
            "claims": [],
        }
    if "synthesizer" in model:
        return {
            "decision_summary": "Synthesize one evidence-bounded conclusion.",
            "conclusions": [{
                "conclusion_id": "conclusion-smoke",
                "text": "The synthetic event is supported by the selected message.",
                "support_evidence_ids": [evidence_id],
                "citation_ids": [],
                "confidence": 1,
            }],
        }
    if "verifier" in model:
        return {
            "decision_summary": "Verify the evidence-bounded conclusion.",
            "verdict": "verified",
            "verified_conclusion_ids": ["conclusion-smoke"],
            "gaps": [],
            "warnings": ["case_transfer_limited"],
        }
    raise ValueError("unsupported smoke model")


class Handler(BaseHTTPRequestHandler):
    def log_message(self, _format, *_args):
        return

    def write_json(self, status, value):
        payload = json.dumps(value, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/api/v1/session":
            self.write_json(200, {"items": []})
            return
        if path == "/api/v1/chatlog":
            messages = [
                {
                    "seq": 7300,
                    "time": "2026-08-13T08:00:00+08:00",
                    "talker": "smoke-conversation",
                    "sender": "smoke-peer",
                    "type": 1,
                    "content": "bounded context before the selected message",
                },
                {
                    "seq": 7301,
                    "time": "2026-08-13T08:01:00+08:00",
                    "talker": "smoke-conversation",
                    "sender": "smoke-identity",
                    "type": 1,
                    "content": PRIVATE_SENTINEL + " synthetic bounded selected evidence",
                },
                {
                    "seq": 7302,
                    "time": "2026-08-13T08:02:00+08:00",
                    "talker": "smoke-conversation",
                    "sender": "smoke-peer",
                    "type": 1,
                    "content": "bounded context after the selected message",
                },
            ]
            query = parse_qs(urlparse(self.path).query)
            if query.get("keyword"):
                messages = [messages[1]]
            self.write_json(200, messages)
            return
        self.write_json(404, {"error": "not_found"})

    def do_POST(self):
        if self.path.split("?", 1)[0] != "/v1/chat/completions":
            self.write_json(404, {"error": "not_found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            payload = json.loads(self.rfile.read(length))
            content = json.dumps(model_output(payload), separators=(",", ":"))
        except (ValueError, json.JSONDecodeError) as error:
            self.write_json(400, {"error": str(error)})
            return
        self.write_json(200, {
            "choices": [{"message": {"content": content}}],
            "usage": {"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20, "cost_usd": 0.001},
        })


def main():
    if len(sys.argv) != 2:
        raise SystemExit("usage: research-agent-smoke-fixture.py PORT_FILE")
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    with open(sys.argv[1], "w", encoding="utf-8") as port_file:
        port_file.write(str(server.server_port))
    server.serve_forever()


if __name__ == "__main__":
    main()
