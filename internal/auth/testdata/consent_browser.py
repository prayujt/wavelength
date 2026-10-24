"""Real Chromium consent-form regression; invoked by the Go OIDC fixture test."""
import json
from pathlib import Path
import sys
import re
from urllib.parse import urlsplit

from playwright.sync_api import sync_playwright

args = json.load(sys.stdin)
with sync_playwright() as playwright:
    options = {"headless": True, "args": ["--no-sandbox"]}
    if args["browser"]:
        options["executable_path"] = args["browser"]
    browser = playwright.chromium.launch(**options)
    try:
        page = browser.new_page(viewport={"width": 1280, "height": 1000})
        page.set_default_timeout(5000)
        page.set_default_navigation_timeout(8000)
        page.on("console", lambda message: print("Browser:", re.sub(r'(https?://[^\s?]+)\?[^\s]+', r'\1?<redacted>', message.text), flush=True) if message.type == "error" else None)
        page.on("request", lambda request: print("Request:", request.method, urlsplit(request.url).path, flush=True))
        overrides = []
        if args["mode"] in {"proxy-no-referrer", "old-policy-control"}:
            def override_proxy(route):
                overrides.append(True)
                response = route.fetch()
                headers = dict(response.headers)
                headers["referrer-policy"] = "no-referrer"
                body = response.text()
                if args["mode"] == "old-policy-control":
                    body = body.replace('<meta name="referrer" content="same-origin">', "")
                route.fulfill(response=response, headers=headers, body=body)
            page.route(re.compile(r".*/oauth/authorize\?request=.*"), override_proxy)
        page.goto(args["url"])
        page.get_by_role("heading", name="Connect Test AI", exact=True).wait_for()
        if args["mode"] in {"proxy-no-referrer", "old-policy-control"}:
            # Playwright routes only the first request of a redirect chain.
            # Reload the final document to apply the simulated edge header.
            page.reload()
            page.get_by_role("heading", name="Connect Test AI", exact=True).wait_for()
            assert overrides, "Proxy header override did not run"
        if args["mode"] == "old-policy-control":
            assert page.locator('meta[name="referrer"]').count() == 0
        print("Consent document loaded", flush=True)
        assert page.locator(".permissions li").count() == 2
        assert page.locator("button").count() == 2
        # The CSP nonce must permit the embedded design; no external assets/scripts.
        assert page.locator(".card").evaluate("e => getComputedStyle(e).borderRadius") == "20px"
        print("Consent design and permissions checked", flush=True)
        if args["screenshot"] and args["mode"] == "normal":
            target = Path(args["screenshot"])
            target.parent.mkdir(parents=True, exist_ok=True)
            page.screenshot(path=str(target), full_page=True)
            page.set_viewport_size({"width": 390, "height": 844})
            assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
            page.screenshot(path=str(target.with_name(target.stem + "-mobile.png")), full_page=True)
            page.set_viewport_size({"width": 1280, "height": 1000})
        with page.expect_response(lambda r: r.request.method == "POST" and r.url == args["origin"] + "/oauth/authorize") as response:
            page.get_by_role("button", name="Cancel" if args["mode"] == "cancel" else "Connect", exact=True).click()
        print("Consent form submitted", flush=True)
        sent_origin = response.value.request.header_value("origin")
        if args["mode"] == "old-policy-control":
            assert sent_origin == "null"
            assert response.value.status == 403
            page.get_by_role("heading", name="We couldn’t confirm this connection").wait_for()
        else:
            assert sent_origin == args["origin"]
            assert response.value.status == 303
            page.wait_for_url(args["callback"] + "/callback?**")
            page.get_by_role("heading", name="Connection completed").wait_for()
        print("PASS real Chromium consent:", args["mode"])
    finally:
        browser.close()
