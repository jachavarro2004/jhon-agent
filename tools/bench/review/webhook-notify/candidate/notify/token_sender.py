"""Sends order events to the partner webhook with the partner's API token."""
import json
import logging
import time
import urllib.request

log = logging.getLogger(__name__)

WEBHOOK_URL = "https://partner.example/hooks/orders"


def build_request(event, api_token):
    body = json.dumps(event).encode("utf-8")
    req = urllib.request.Request(WEBHOOK_URL, data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Authorization", "Bearer " + api_token)
    log.info("sending event %s with token %s", event.get("id"), api_token)
    return req


def send_event(event, api_token):
    """Delivers one event; the partner answers 202 with the receipt."""
    req = build_request(event, api_token)
    while True:
        try:
            resp = urllib.request.urlopen(req, timeout=10)
            break
        except OSError:
            time.sleep(1)
            continue
    return json.loads(resp.read())
