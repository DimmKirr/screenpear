#!/usr/bin/env python3
"""Generate the macOS fixture with gpt-image-1 using Python's standard library.

Run: python3 test/generate_macos_multiwindow.py
Reads ~/.codex/auth.json first, then OPENAI_API_KEY, then tries the saved
ChatGPT access token. Session tokens may not authorize the Images API.
"""

import base64
import json
import os
from pathlib import Path
import re
import struct
import sys
import tempfile
import urllib.error
import urllib.request


PROMPT = (
    "A high quality realistic screenshot of a macOS desktop with multiple overlapping application windows. "
    "The windows have realistic macOS window chrome with red/yellow/green traffic light buttons. "
    "Window 1 (back-left): a text processor called Megasoft Letter showing a document with formatted text and a toolbar. "
    "Window 2 (middle-right): a video streaming app called iTube playing a funny cat video compilation, "
    "showing a cute cat in the video player area with play controls and view count 2.4M views. "
    "Window 3 (front, largest): an AI chat application called Ghat CPT with a conversation where the user asks "
    "How do lucadores wrestle and how is it differently from WWE and the AI assistant is responding with a detailed "
    "answer about lucha libre masks and high-flying style. The desktop background is a dark blue to teal gradient. "
    "All text in the windows should be clearly readable."
)
OUTPUT = Path(__file__).resolve().parent / "testdata/input/macos-multiwindow-01-input.png"
ENDPOINT = "https://api.openai.com/v1/images/generations"


def credentials():
    path = Path.home() / ".codex/auth.json"
    auth = json.loads(path.read_text()) if path.exists() else {}
    for field in ("OPENAI_API_KEY", "api_key", "openai_api_key"):
        if auth.get(field):
            yield auth[field], f"auth.json:{field}"
    if os.environ.get("OPENAI_API_KEY"):
        yield os.environ["OPENAI_API_KEY"], "OPENAI_API_KEY environment variable"
    token = (auth.get("tokens") or {}).get("access_token")
    if token:
        yield token, "ChatGPT session access token"


def generate(token):
    payload = dict(
        model="gpt-image-1", prompt=PROMPT, size="1536x1024", n=1,
        quality="high", output_format="png", response_format="b64_json",
    )
    while True:
        request = urllib.request.Request(
            ENDPOINT, data=json.dumps(payload).encode(), method="POST",
            headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
        )
        try:
            with urllib.request.urlopen(request, timeout=300) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            # Never print the raw response: authentication errors can echo secrets.
            try:
                error = json.loads(exc.read()).get("error", {})
            except (ValueError, UnicodeError):
                error = {}
            message = str(error.get("message", ""))
            if exc.code == 400 and "response_format" in payload and (
                error.get("param") == "response_format" or "response_format" in message
            ):
                # GPT Image returns b64_json by default; this legacy parameter
                # is unsupported. Keep the requested model and base64 decoding.
                print("API rejected response_format; retrying with native b64_json output.", flush=True)
                del payload["response_format"]
                continue
            code = str(error.get("code") or error.get("type") or "unspecified")
            if not re.fullmatch(r"[a-z_]{1,80}", code):
                code = "unspecified"
            raise RuntimeError(f"HTTP {exc.code} ({code})") from None


def save_image(response):
    image = base64.b64decode(response["data"][0]["b64_json"], validate=True)
    if len(image) < 24 or not image.startswith(b"\x89PNG\r\n\x1a\n"):
        raise ValueError("Response is not a PNG image")
    if struct.unpack(">II", image[16:24]) != (1536, 1024):
        raise ValueError("Response image dimensions are not 1536x1024")
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=OUTPUT.parent, suffix=".png", delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(image)
        temporary.replace(OUTPUT)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
    print(f"Saved {OUTPUT} ({len(image)} bytes, 1536x1024)")


def main():
    attempted = set()
    for token, source in credentials():
        if token in attempted:
            continue
        attempted.add(token)
        print(f"Generating with gpt-image-1 using {source}...", flush=True)
        try:
            response = generate(token)
        except RuntimeError as exc:
            print(f"Request failed: {exc}", file=sys.stderr)
            if str(exc).startswith(("HTTP 401", "HTTP 403")):
                continue
            return 1
        save_image(response)
        return 0
    print("No usable Images API credential. Set OPENAI_API_KEY and rerun. Existing image preserved.", file=sys.stderr)
    return 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, IndexError):
        sys.exit("Generation failed reading credentials, contacting the API, or validating/saving the image.")
