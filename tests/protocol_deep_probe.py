from __future__ import annotations

import argparse
import json
import os
import queue
import socket
import threading
import time
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import httpx
from anthropic import Anthropic
from openai import OpenAI


ROOT = Path(__file__).resolve().parents[1]
CONFIG_PATH = ROOT / "backend" / "docs" / "1.txt"
DEFAULT_LOG_DIR = ROOT / "tests" / ".sse-probe-logs"


@dataclass(frozen=True)
class Endpoint:
    origin: str
    path: str
    model: str
    api_key: str


@dataclass(frozen=True)
class ProbeConfig:
    responses: Endpoint
    chat: Endpoint
    messages: Endpoint


@dataclass(frozen=True)
class ProxyRequest:
    case_name: str
    upstream: Endpoint


def parse_config(path: Path) -> ProbeConfig:
    lines = [line.strip() for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
    if len(lines) < 6:
        raise RuntimeError(f"{path} must contain responses/chat endpoint, model, key, messages endpoint, model, key")

    first_url = lines[0]
    responses_url, chat_url = split_responses_chat_url(first_url)
    responses_model = lines[1]
    responses_key = lines[2]
    messages_url = normalize_v1_path(lines[3])
    messages_model = lines[4]
    messages_key = lines[5]

    responses_origin, responses_path = split_origin_path(responses_url)
    chat_origin, chat_path = split_origin_path(chat_url)
    messages_origin, messages_path = split_origin_path(messages_url)
    return ProbeConfig(
        responses=Endpoint(responses_origin, responses_path, responses_model, responses_key),
        chat=Endpoint(chat_origin, chat_path, responses_model, responses_key),
        messages=Endpoint(messages_origin, messages_path, messages_model, messages_key),
    )


def split_responses_chat_url(raw: str) -> tuple[str, str]:
    if "或" in raw:
        left, right = raw.split("或", 1)
        parsed = urlparse(left)
        if right.startswith("http://") or right.startswith("https://"):
            return left, normalize_v1_path(right)
        chat_path = right if right.startswith("/v1/") else "/v1" + right
        return left, f"{parsed.scheme}://{parsed.netloc}{chat_path}"
    parsed = urlparse(raw)
    origin = f"{parsed.scheme}://{parsed.netloc}"
    return f"{origin}/responses", f"{origin}/v1/chat/completions"


def normalize_v1_path(raw: str) -> str:
    parsed = urlparse(raw)
    if parsed.path.startswith("/v1/"):
        return raw
    return f"{parsed.scheme}://{parsed.netloc}/v1{parsed.path or ''}"


def split_origin_path(raw: str) -> tuple[str, str]:
    parsed = urlparse(raw)
    if not parsed.scheme or not parsed.netloc:
        raise RuntimeError(f"invalid endpoint URL: {raw}")
    return f"{parsed.scheme}://{parsed.netloc}", parsed.path or "/"


def find_free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def append_log(log_dir: Path, case_name: str, channel: str, payload: dict[str, Any]) -> None:
    log_dir.mkdir(parents=True, exist_ok=True)
    with (log_dir / f"{case_name}.{channel}.jsonl").open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(payload, ensure_ascii=False) + "\n")


def safe_json_loads(raw: str) -> Any:
    try:
        return json.loads(raw)
    except Exception:
        return raw


def compact_event_summary(parsed: Any) -> str:
    if not isinstance(parsed, dict):
        return ""
    event_type = parsed.get("type", "")
    parts: list[str] = []
    if event_type:
        parts.append(f"type={event_type}")
    if "delta" in parsed and isinstance(parsed["delta"], str) and parsed["delta"]:
        parts.append(f"delta={parsed['delta']!r}")
    if event_type == "content_block_delta":
        delta = parsed.get("delta")
        if isinstance(delta, dict):
            parts.append(f"delta_type={delta.get('type')}")
            text = delta.get("text") or delta.get("thinking") or delta.get("partial_json")
            if text:
                parts.append(f"payload={str(text)[:80]!r}")
    if event_type == "response.output_item.added":
        item = parsed.get("item")
        if isinstance(item, dict):
            parts.append(f"item_type={item.get('type')}")
    if event_type == "response.output_item.done":
        item = parsed.get("item")
        if isinstance(item, dict):
            parts.append(f"item_type={item.get('type')}")
    if "choices" in parsed:
        for choice in parsed.get("choices") or []:
            if isinstance(choice, dict):
                delta = choice.get("delta") or {}
                if isinstance(delta, dict):
                    if delta.get("content"):
                        parts.append(f"content={str(delta.get('content'))[:80]!r}")
                    if delta.get("tool_calls"):
                        parts.append("tool_calls=true")
                if choice.get("finish_reason"):
                    parts.append(f"finish_reason={choice.get('finish_reason')}")
    return " ".join(parts)


def sanitize_request(payload: dict[str, Any]) -> dict[str, Any]:
    return json.loads(json.dumps(payload, ensure_ascii=False))


class LoggingProxy:
    def __init__(self, requests: "queue.Queue[ProxyRequest]", log_dir: Path) -> None:
        self.requests = requests
        self.log_dir = log_dir
        self.server: ThreadingHTTPServer | None = None

    def start(self, port: int) -> str:
        proxy = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, _format: str, *_args: Any) -> None:
                return

            def do_GET(self) -> None:
                if self.path == "/healthz":
                    body = b'{"ok":true}'
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                    return
                self.send_error(404)

            def do_POST(self) -> None:
                proxy_request = proxy.requests.get(timeout=10)
                content_length = int(self.headers.get("Content-Length", "0") or "0")
                body = self.rfile.read(content_length)
                upstream_url = proxy_request.upstream.origin + proxy_request.upstream.path
                request_payload = safe_json_loads(body.decode("utf-8", errors="replace"))
                if isinstance(request_payload, dict):
                    append_log(proxy.log_dir, proxy_request.case_name, "request", sanitize_request(request_payload))

                headers = {
                    "Authorization": f"Bearer {proxy_request.upstream.api_key}",
                    "Content-Type": self.headers.get("Content-Type", "application/json"),
                    "Accept": self.headers.get("Accept", "text/event-stream"),
                }
                timeout = httpx.Timeout(180.0, connect=30.0)
                try:
                    with httpx.Client(timeout=timeout) as client:
                        with client.stream("POST", upstream_url, headers=headers, content=body) as resp:
                            append_log(
                                proxy.log_dir,
                                proxy_request.case_name,
                                "proxy",
                                {
                                    "upstream_status": resp.status_code,
                                    "content_type": resp.headers.get("content-type", ""),
                                    "upstream_path": proxy_request.upstream.path,
                                },
                            )
                            self.send_response(resp.status_code)
                            content_type = resp.headers.get("content-type", "text/event-stream")
                            self.send_header("Content-Type", content_type)
                            self.send_header("Cache-Control", "no-cache")
                            self.send_header("Connection", "close")
                            self.end_headers()
                            if resp.status_code >= 400:
                                raw = resp.read()
                                append_log(
                                    proxy.log_dir,
                                    proxy_request.case_name,
                                    "proxy",
                                    {
                                        "upstream_status": resp.status_code,
                                        "body": raw.decode("utf-8", errors="replace"),
                                    },
                                )
                                self.wfile.write(raw)
                                self.wfile.flush()
                                return
                            self._stream_sse(resp, proxy_request.case_name, proxy.log_dir)
                except Exception as exc:
                    append_log(
                        proxy.log_dir,
                        proxy_request.case_name,
                        "proxy",
                        {"exception": f"{type(exc).__name__}: {exc}"},
                    )
                    raise

            def _stream_sse(self, resp: httpx.Response, case_name: str, log_dir: Path) -> None:
                event_name = ""
                data_lines: list[str] = []
                raw_lines = 0
                for raw_line in resp.iter_lines():
                    raw_lines += 1
                    self.wfile.write((raw_line + "\n").encode("utf-8"))
                    self.wfile.flush()
                    if raw_line == "":
                        if event_name or data_lines:
                            data = "\n".join(data_lines).strip()
                            parsed = safe_json_loads(data) if data else ""
                            append_log(
                                log_dir,
                                case_name,
                                "proxy",
                                {"event": event_name, "raw_data": data, "parsed": parsed},
                            )
                            summary = compact_event_summary(parsed)
                            print(f"[proxy][{case_name}] event={event_name or '<message>'} {summary}", flush=True)
                        event_name = ""
                        data_lines = []
                        continue
                    if raw_line.startswith("event:"):
                        event_name = raw_line.split(":", 1)[1].strip()
                    elif raw_line.startswith("data:"):
                        data_lines.append(raw_line.split(":", 1)[1].lstrip())
                    elif raw_line:
                        append_log(log_dir, case_name, "proxy", {"raw_line": raw_line})
                if event_name or data_lines:
                    data = "\n".join(data_lines).strip()
                    parsed = safe_json_loads(data) if data else ""
                    append_log(
                        log_dir,
                        case_name,
                        "proxy",
                        {"event": event_name, "raw_data": data, "parsed": parsed, "eof": True},
                    )
                    summary = compact_event_summary(parsed)
                    print(f"[proxy][{case_name}] eof event={event_name or '<message>'} {summary}", flush=True)
                if raw_lines == 0:
                    append_log(log_dir, case_name, "proxy", {"empty_stream": True})

        self.server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
        thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        thread.start()
        base_url = f"http://127.0.0.1:{port}"
        wait_for_health(base_url)
        return base_url

    def stop(self) -> None:
        if self.server is not None:
            self.server.shutdown()
            self.server.server_close()


def wait_for_health(base_url: str) -> None:
    deadline = time.time() + 10
    while time.time() < deadline:
        try:
            with httpx.Client(timeout=1.0) as client:
                if client.get(f"{base_url}/healthz").status_code == 200:
                    return
        except Exception:
            time.sleep(0.1)
    raise TimeoutError("proxy did not become healthy")


def responses_cases(model: str) -> list[tuple[str, dict[str, Any]]]:
    tool = {
        "type": "function",
        "name": "lookup_weather",
        "description": "Look up weather for a city.",
        "parameters": {
            "type": "object",
            "properties": {"city": {"type": "string"}},
            "required": ["city"],
            "additionalProperties": False,
        },
    }
    return [
        (
            "deep_responses_forced_tool",
            {
                "model": model,
                "input": "Call lookup_weather for Hangzhou.",
                "stream": True,
                "tools": [tool],
                "tool_choice": {"type": "function", "name": "lookup_weather"},
            },
        ),
        (
            "deep_responses_reasoning",
            {
                "model": model,
                "input": "Think briefly, then answer with one short sentence: why is the sky blue?",
                "stream": True,
                "reasoning": {"effort": "high", "summary": "auto"},
                "include": ["reasoning.encrypted_content"],
                "max_output_tokens": 256,
            },
        ),
    ]


def chat_cases(model: str) -> list[tuple[str, dict[str, Any]]]:
    return [
        (
            "deep_chat_forced_tool",
            {
                "model": model,
                "messages": [{"role": "user", "content": "Call lookup_weather for Hangzhou."}],
                "stream": True,
                "tools": [
                    {
                        "type": "function",
                        "function": {
                            "name": "lookup_weather",
                            "description": "Look up weather for a city.",
                            "parameters": {
                                "type": "object",
                                "properties": {"city": {"type": "string"}},
                                "required": ["city"],
                                "additionalProperties": False,
                            },
                        },
                    }
                ],
                "tool_choice": {"type": "function", "function": {"name": "lookup_weather"}},
            },
        ),
    ]


def messages_cases(model: str) -> list[tuple[str, dict[str, Any]]]:
    tool = {
        "name": "lookup_weather",
        "description": "Look up weather for a city.",
        "input_schema": {
            "type": "object",
            "properties": {"city": {"type": "string"}},
            "required": ["city"],
            "additionalProperties": False,
        },
    }
    return [
        (
            "deep_messages_forced_tool",
            {
                "model": model,
                "max_tokens": 256,
                "messages": [{"role": "user", "content": "Call lookup_weather for Hangzhou."}],
                "stream": True,
                "tools": [tool],
                "tool_choice": {"type": "tool", "name": "lookup_weather"},
            },
        ),
        (
            "deep_messages_thinking",
            {
                "model": model,
                "max_tokens": 1024,
                "messages": [{"role": "user", "content": "Think step by step internally, then answer: 17 * 19 = ?"}],
                "stream": True,
                "thinking": {"type": "enabled", "budget_tokens": 512},
            },
        ),
    ]


def consume_responses(client: OpenAI, case_name: str, payload: dict[str, Any], log_dir: Path) -> None:
    with client.responses.create(**payload) as stream:
        for event in stream:
            event_dict = event.model_dump(mode="json")
            append_log(log_dir, case_name, "sdk", {"type": event.type, "event": event_dict})
            print(f"[sdk][{case_name}] type={event.type} {compact_event_summary(event_dict)}", flush=True)


def consume_chat(client: OpenAI, case_name: str, payload: dict[str, Any], log_dir: Path) -> None:
    for chunk in client.chat.completions.create(**payload):
        chunk_dict = chunk.model_dump(mode="json")
        append_log(log_dir, case_name, "sdk", {"event": chunk_dict})
        print(f"[sdk][{case_name}] chunk {compact_event_summary(chunk_dict)}", flush=True)


def consume_messages(client: Anthropic, case_name: str, payload: dict[str, Any], log_dir: Path) -> None:
    for event in client.messages.create(**payload):
        event_dict = event.model_dump(mode="json")
        event_type = str(event_dict.get("type") or getattr(event, "type", ""))
        append_log(log_dir, case_name, "sdk", {"type": event_type, "event": event_dict})
        print(f"[sdk][{case_name}] type={event_type} {compact_event_summary(event_dict)}", flush=True)


def run_case(
    requests: "queue.Queue[ProxyRequest]",
    log_dir: Path,
    case_name: str,
    endpoint: Endpoint,
    payload: dict[str, Any],
    consume: Any,
) -> None:
    print(f"\n===== {case_name} =====", flush=True)
    append_log(log_dir, case_name, "request", sanitize_request(payload))
    requests.put(ProxyRequest(case_name=case_name, upstream=endpoint))
    try:
        consume(case_name, payload)
    except Exception as exc:
        append_log(log_dir, case_name, "sdk", {"exception": f"{type(exc).__name__}: {exc}"})
        print(f"[sdk][{case_name}] exception={type(exc).__name__}: {exc}", flush=True)


def main() -> None:
    parser = argparse.ArgumentParser(description="Probe Responses, Chat Completions, and Anthropic Messages stream semantics.")
    parser.add_argument("--case", action="append", default=[], help="Run only selected case names")
    parser.add_argument("--log-dir", default=str(DEFAULT_LOG_DIR), help="Directory for JSONL logs")
    parser.add_argument("--port", type=int, default=0, help="Local proxy port, 0 means random")
    args = parser.parse_args()

    config = parse_config(CONFIG_PATH)
    log_dir = Path(args.log_dir)
    port = args.port or find_free_port()
    requests: "queue.Queue[ProxyRequest]" = queue.Queue()
    proxy = LoggingProxy(requests, log_dir)
    base_url = proxy.start(port)

    openai_client = OpenAI(
        api_key=os.environ.get("CHATAPI_PROTOCOL_PROBE_OPENAI_KEY", "local-proxy-key"),
        base_url=f"{base_url}/v1",
    )
    anthropic_client = Anthropic(
        api_key=os.environ.get("CHATAPI_PROTOCOL_PROBE_ANTHROPIC_KEY", "local-proxy-key"),
        base_url=base_url,
    )
    selected = set(args.case)

    try:
        for case_name, payload in responses_cases(config.responses.model):
            if selected and case_name not in selected:
                continue
            run_case(
                requests,
                log_dir,
                case_name,
                config.responses,
                payload,
                lambda name, body: consume_responses(openai_client, name, body, log_dir),
            )
        for case_name, payload in chat_cases(config.chat.model):
            if selected and case_name not in selected:
                continue
            run_case(
                requests,
                log_dir,
                case_name,
                config.chat,
                payload,
                lambda name, body: consume_chat(openai_client, name, body, log_dir),
            )
        for case_name, payload in messages_cases(config.messages.model):
            if selected and case_name not in selected:
                continue
            run_case(
                requests,
                log_dir,
                case_name,
                config.messages,
                payload,
                lambda name, body: consume_messages(anthropic_client, name, body, log_dir),
            )
    finally:
        proxy.stop()


if __name__ == "__main__":
    main()
