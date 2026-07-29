import json
import urllib.request


def _http_post(url: str, data: bytes) -> int:
    req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.getcode()


def post(webhook_url: str, text: str, blocks: list, http_post=_http_post) -> None:
    data = json.dumps({"text": text, "blocks": blocks}).encode()
    status = http_post(webhook_url, data)
    if status != 200:
        raise RuntimeError(f"Slack webhook returned HTTP {status}")
