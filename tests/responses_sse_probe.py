from __future__ import annotations

import argparse
import json
import os
import queue
import re
import socket
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import httpx
import uvicorn
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, StreamingResponse
from openai import OpenAI


ROOT = Path(__file__).resolve().parents[1]
UPSTREAM_HINT = ROOT / "backend" / "docs" / "1.txt"
DEFAULT_LOG_DIR = ROOT / "tests" / ".sse-probe-logs"


@dataclass
class UpstreamConfig:
    base_url: str
    responses_api_key: str
    responses_model: str
    messages_api_key: str
    messages_model: str


def load_upstream_config(path: Path) -> UpstreamConfig:
    text = path.read_text(encoding="utf-8")
    base_url = must_match(text, r"^(https?://\S+)$", re.MULTILINE, "base_url")
    responses_api_key = must_match(text, r"chat/responses:\s*(sk-\S+)", 0, "responses_api_key")
    responses_model = must_match(text, r"chat/responses:.*?\n模型:\s*(\S+)", re.DOTALL, "responses_model")
    messages_api_key = must_match(text, r"message:\s*(sk-\S+)", 0, "messages_api_key")
    messages_model = must_match(text, r"message:.*?\n模型:\s*(\S+)", re.DOTALL, "messages_model")
    return UpstreamConfig(
        base_url=base_url.rstrip("/"),
        responses_api_key=responses_api_key,
        responses_model=responses_model,
        messages_api_key=messages_api_key,
        messages_model=messages_model,
    )


def must_match(text: str, pattern: str, flags: int, field_name: str) -> str:
    match = re.search(pattern, text, flags)
    if not match:
        raise RuntimeError(f"failed to parse {field_name} from {UPSTREAM_HINT}")
    return match.group(1).strip()


def find_free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def safe_json_loads(raw: str) -> Any:
    try:
        return json.loads(raw)
    except Exception:
        return raw


def compact_payload_summary(parsed: Any) -> str:
    if not isinstance(parsed, dict):
        return ""
    event_type = parsed.get("type", "")
    if event_type in {"response.created", "response.in_progress", "response.completed", "response.failed"}:
        response = parsed.get("response") or {}
        if isinstance(response, dict):
            parts = [
                f"status={response.get('status')}",
                f"model={response.get('model')}",
                f"service_tier={response.get('service_tier')}",
            ]
            usage = response.get("usage")
            if isinstance(usage, dict):
                parts.append(
                    "usage="
                    f"{usage.get('input_tokens')}/{usage.get('output_tokens')}/{usage.get('total_tokens')}"
                )
            return " ".join(part for part in parts if part and not part.endswith("=None"))
    if event_type in {"response.output_text.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta"}:
        delta = parsed.get("delta")
        if delta:
            return f"delta={delta!r}"
    if event_type in {"response.output_text.done", "response.reasoning_summary_text.done"}:
        text = parsed.get("text")
        if text:
            return f"text={text!r}"
    return ""


def append_log(log_dir: Path, case_name: str, channel: str, payload: dict[str, Any]) -> None:
    log_dir.mkdir(parents=True, exist_ok=True)
    path = log_dir / f"{case_name}.{channel}.jsonl"
    with path.open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(payload, ensure_ascii=False) + "\n")


def print_sse_event(case_name: str, event_name: str, data_lines: list[str], log_dir: Path) -> None:
    payload_text = "\n".join(data_lines).strip()
    parsed = safe_json_loads(payload_text) if payload_text else ""
    append_log(
        log_dir,
        case_name,
        "proxy",
        {
            "event": event_name or "",
            "raw_data": payload_text,
            "parsed": parsed,
        },
    )
    headline = f"[proxy][{case_name}] event={event_name or '<message>'}"
    print(headline, flush=True)
    if isinstance(parsed, dict):
        event_type = parsed.get("type")
        if event_type:
            print(f"[proxy][{case_name}] type={event_type}", flush=True)
        summary = compact_payload_summary(parsed)
        if summary:
            print(f"[proxy][{case_name}] {summary}", flush=True)
    elif payload_text:
        print(f"[proxy][{case_name}] data={payload_text}", flush=True)


class ProxyServer:
    def __init__(self, upstream: UpstreamConfig, case_name_queue: "queue.Queue[str]", log_dir: Path) -> None:
        self.upstream = upstream
        self.case_name_queue = case_name_queue
        self.log_dir = log_dir
        self.app = FastAPI()
        self._install_routes()

    def _install_routes(self) -> None:
        @self.app.get("/healthz")
        async def healthz() -> dict[str, bool]:
            return {"ok": True}

        @self.app.post("/v1/responses", response_model=None)
        async def proxy_responses(request: Request):
            body = await request.body()
            try:
                case_name = self.case_name_queue.get_nowait()
            except queue.Empty:
                case_name = "unnamed"
            headers = {
                "Authorization": f"Bearer {self.upstream.responses_api_key}",
                "Content-Type": request.headers.get("content-type", "application/json"),
                "Accept": request.headers.get("accept", "text/event-stream"),
            }
            upstream_url = f"{self.upstream.base_url}/responses"
            timeout = httpx.Timeout(120.0, connect=30.0)

            async def stream_upstream():
                event_name = ""
                data_lines: list[str] = []
                async with httpx.AsyncClient(timeout=timeout) as client:
                    async with client.stream("POST", upstream_url, headers=headers, content=body) as resp:
                        if resp.status_code >= 400:
                            error_text = await resp.aread()
                            print(
                                f"[proxy][{case_name}] upstream_http_error status={resp.status_code} body={error_text.decode('utf-8', errors='replace')}",
                                flush=True,
                            )
                            yield error_text
                            return

                        async for raw_line in resp.aiter_lines():
                            if raw_line == "":
                                if event_name or data_lines:
                                    print_sse_event(case_name, event_name, data_lines, self.log_dir)
                                yield b"\n"
                                event_name = ""
                                data_lines = []
                                continue

                            if raw_line.startswith(":"):
                                yield (raw_line + "\n").encode("utf-8")
                                continue

                            if raw_line.startswith("event:"):
                                event_name = raw_line.split(":", 1)[1].strip()
                            elif raw_line.startswith("data:"):
                                data_lines.append(raw_line.split(":", 1)[1].lstrip())

                            yield (raw_line + "\n").encode("utf-8")

            return StreamingResponse(
                stream_upstream(),
                status_code=200,
                media_type="text/event-stream",
                headers={
                    "Cache-Control": "no-cache",
                    "Connection": "keep-alive",
                },
            )


def make_cases(model: str) -> list[tuple[str, dict[str, Any]]]:
    return [
        (
            "basic_text",
            {
                "model": model,
                "input": "Say hello in one short sentence.",
                "stream": True,
            },
        ),
        (
            "sampling_knobs",
            {
                "model": model,
                "input": "Explain what SSE is in one sentence.",
                "stream": True,
                "temperature": 0.7,
                "top_p": 0.9,
                "max_output_tokens": 64,
                "store": False,
                "metadata": {"probe": "sampling_knobs"},
                "user": "chatapi-sse-probe",
            },
        ),
        (
            "reasoning_and_text",
            {
                "model": model,
                "input": "Think briefly, then answer: what is 2+2?",
                "stream": True,
                "reasoning": {"effort": "medium"},
                "text": {"verbosity": "low"},
                "include": ["reasoning.encrypted_content"],
            },
        ),
        (
            "tool_schema",
            {
                "model": model,
                "input": "If useful, call the weather tool for Tokyo.",
                "stream": True,
                "parallel_tool_calls": False,
                "tools": [
                    {
                        "type": "function",
                        "name": "lookup_weather",
                        "description": "Look up weather",
                        "parameters": {
                            "type": "object",
                            "properties": {
                                "city": {"type": "string"},
                            },
                            "required": ["city"],
                            "additionalProperties": False,
                        },
                    }
                ],
                "tool_choice": "auto",
            },
        ),
    ]


def consume_stream(client: OpenAI, case_name: str, payload: dict[str, Any], log_dir: Path) -> None:
    print(f"\n===== CASE {case_name} REQUEST =====", flush=True)
    print(json.dumps(payload, ensure_ascii=False, indent=2), flush=True)
    append_log(log_dir, case_name, "request", payload)
    stream = client.responses.create(**payload)
    with stream as events:
        for event in events:
            event_dict = event.model_dump(mode="json")
            append_log(
                log_dir,
                case_name,
                "sdk",
                {
                    "type": event.type,
                    "event": event_dict,
                },
            )
            print(f"[sdk][{case_name}] type={event.type}", flush=True)
            summary = compact_payload_summary(event_dict)
            if summary:
                print(f"[sdk][{case_name}] {summary}", flush=True)


def wait_for_health(base_url: str, timeout_seconds: float = 10.0) -> None:
    deadline = time.time() + timeout_seconds
    while time.time() < deadline:
        try:
            with httpx.Client(timeout=1.0) as client:
                resp = client.get(f"{base_url}/healthz")
                if resp.status_code == 200:
                    return
        except Exception:
            pass
        time.sleep(0.1)
    raise TimeoutError("proxy server did not become healthy in time")


def main() -> None:
    parser = argparse.ArgumentParser(description="Probe Responses SSE events through a local forwarding proxy.")
    parser.add_argument("--case", action="append", default=[], help="Only run selected case name(s)")
    parser.add_argument("--base-port", type=int, default=0, help="Fixed local port, 0 means random")
    parser.add_argument("--log-dir", default=str(DEFAULT_LOG_DIR), help="Directory for request/proxy/sdk JSONL logs")
    args = parser.parse_args()

    upstream = load_upstream_config(UPSTREAM_HINT)
    all_cases = make_cases(upstream.responses_model)
    selected = set(args.case)
    cases = [item for item in all_cases if not selected or item[0] in selected]
    if not cases:
        raise SystemExit("no matching cases selected")

    log_dir = Path(args.log_dir)
    port = args.base_port or find_free_port()
    case_name_queue: "queue.Queue[str]" = queue.Queue()
    proxy = ProxyServer(upstream, case_name_queue, log_dir)
    config = uvicorn.Config(proxy.app, host="127.0.0.1", port=port, log_level="warning")
    server = uvicorn.Server(config)
    server_thread = threading.Thread(target=server.run, daemon=True)
    server_thread.start()
    wait_for_health(f"http://127.0.0.1:{port}")

    client = OpenAI(
        api_key=os.environ.get("CHATAPI_SSE_PROBE_KEY", "local-proxy-key"),
        base_url=f"http://127.0.0.1:{port}/v1",
    )

    try:
        for case_name, payload in cases:
            case_name_queue.put(case_name)
            try:
                consume_stream(client, case_name, payload, log_dir)
            except Exception as exc:
                print(f"[sdk][{case_name}] exception={type(exc).__name__}: {exc}", flush=True)
    finally:
        server.should_exit = True
        server_thread.join(timeout=5.0)


if __name__ == "__main__":
    main()
