import json
import urllib.error
import urllib.request

import pytest
from charon_cycler import slack

def test_post_sends_text_and_blocks():
    captured = {}
    def fake_post(url, data):
        captured["url"] = url
        captured["payload"] = json.loads(data.decode())
        return 200
    slack.post("http://hook", "hello", [{"type": "section"}], http_post=fake_post)
    assert captured["url"] == "http://hook"
    assert captured["payload"] == {"text": "hello", "blocks": [{"type": "section"}]}

def test_post_raises_on_non_200():
    with pytest.raises(RuntimeError):
        slack.post("http://hook", "x", [], http_post=lambda url, data: 500)

def test_http_post_returns_status_on_http_error(monkeypatch):
    def fake_urlopen(req, timeout=30):
        raise urllib.error.HTTPError(url="http://x", code=500, msg="err", hdrs=None, fp=None)
    monkeypatch.setattr(urllib.request, "urlopen", fake_urlopen)
    assert slack._http_post("http://x", b"{}") == 500
