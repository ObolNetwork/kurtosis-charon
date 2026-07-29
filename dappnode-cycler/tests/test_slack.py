import json
import pytest
from dappnode_cycler import slack

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
