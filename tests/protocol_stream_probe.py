from __future__ import annotations

import argparse
import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import httpx
from anthropic import Anthropic
from openai import OpenAI


ROOT = Path(__file__).resolve().parents[1]
CONFIG_PATH = ROOT / "backend" / "docs" / "1.txt"
DEFAULT_LOG = ROOT / "tests" / ".protocol-stream-probe-results.json"


@dataclass
class EndpointConfig:
    base_url: str
    responses_api_key: str
    responses_model: str
    messages_api_key: str
    messages_model: str


def parse_config(path: Path) -> EndpointConfig:
    import re

    text = path.read_text(encoding="utf-8")

    def must(pattern: str, flags: int = 0) -> str:
        match = re.search(pattern, text, flags)
        if not match:
            raise RuntimeError(f"missing config pattern: {pattern}")
        return match.group(1).strip()

    return EndpointConfig(
        base_url=must(r"^(https?://\S+)$", re.MULTILINE).rstrip("/"),
        responses_api_key=must(r"chat/responses:\s*(sk-\S+)"),
        responses_model=must(r"chat/responses:.*?\n模型:\s*(\S+)", re.DOTALL),
        messages_api_key=must(r"message:\s*(sk-\S+)"),
        messages_model=must(r"message:.*?\n模型:\s*(\S+)", re.DOTALL),
    )


def compact_dict(value: Any) -> Any:
    if isinstance(value, dict):
        return {k: compact_dict(v) for k, v in value.items()}
    if isinstance(value, list):
        return [compact_dict(v) for v in value]
    return value


def probe_responses(config: EndpointConfig) -> dict[str, Any]:
    client = OpenAI(
        api_key=config.responses_api_key,
        base_url=f"{config.base_url.rstrip('/')}/",
    )
    payload = {
        "model": config.responses_model,
        "input": "Explain SSE in one sentence.",
        "stream": True,
    }
    events: list[dict[str, Any]] = []
    with client.responses.create(**payload) as stream:
        for event in stream:
            event_dict = event.model_dump(mode="json")
            events.append(
                {
                    "type": event.type,
                    "event": compact_dict(event_dict),
                }
            )
    return {
        "protocol": "responses",
        "request": payload,
        "event_types": [item["type"] for item in events],
        "events": events,
    }


def probe_chat(config: EndpointConfig) -> dict[str, Any]:
    client = OpenAI(
        api_key=config.responses_api_key,
        base_url=f"{config.base_url.rstrip('/')}/",
    )
    payload = {
        "model": config.responses_model,
        "messages": [{"role": "user", "content": "Explain SSE in one sentence."}],
        "stream": True,
    }
    raw_chunks: list[dict[str, Any]] = []
    stream = client.chat.completions.create(**payload)
    for chunk in stream:
        raw_chunks.append(chunk.model_dump(mode="json"))
    finish_reasons: list[str] = []
    delta_texts: list[str] = []
    tool_chunks = 0
    for chunk in raw_chunks:
        for choice in chunk.get("choices", []):
            finish_reason = choice.get("finish_reason")
            if finish_reason is not None:
                finish_reasons.append(str(finish_reason))
            delta = choice.get("delta", {})
            content = delta.get("content")
            if content:
                delta_texts.append(str(content))
            tool_calls = delta.get("tool_calls") or []
            tool_chunks += len(tool_calls)
    return {
        "protocol": "chat_completions",
        "request": payload,
        "chunk_count": len(raw_chunks),
        "finish_reasons": finish_reasons,
        "delta_text": "".join(delta_texts),
        "tool_call_chunk_count": tool_chunks,
        "chunks": raw_chunks,
    }


def probe_anthropic(config: EndpointConfig) -> dict[str, Any]:
    base_url = config.base_url.rstrip("/")
    if base_url.endswith("/v1"):
        base_url = base_url[:-3]
    client = Anthropic(
        api_key=config.messages_api_key,
        base_url=base_url,
    )
    payload = {
        "model": config.messages_model,
        "max_tokens": 128,
        "messages": [{"role": "user", "content": "Explain SSE in one sentence."}],
        "stream": True,
    }
    events: list[dict[str, Any]] = []
    stream = client.messages.create(**payload)
    for event in stream:
        event_dict = event.model_dump(mode="json")
        events.append(
            {
                "type": getattr(event, "type", ""),
                "event": compact_dict(event_dict),
            }
        )
    return {
        "protocol": "anthropic_messages",
        "request": payload,
        "event_types": [item["type"] for item in events],
        "events": events,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Probe stream behaviors for responses/chat/messages endpoints.")
    parser.add_argument("--out", default=str(DEFAULT_LOG), help="Output JSON file")
    args = parser.parse_args()

    config = parse_config(CONFIG_PATH)
    results: dict[str, Any] = {"base_url": config.base_url}
    for name, fn in [
        ("responses", probe_responses),
        ("chat_completions", probe_chat),
        ("anthropic_messages", probe_anthropic),
    ]:
        try:
            results[name] = fn(config)
        except Exception as exc:
            results[name] = {
                "protocol": name,
                "error": f"{type(exc).__name__}: {exc}",
            }
    output_path = Path(args.out)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding="utf-8")
    print(output_path)


if __name__ == "__main__":
    main()
