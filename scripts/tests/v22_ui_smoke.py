"""Opt-in built web smoke against the isolated Go integration fixture.

Run via SCRAPIO_V22_UI_PYTHON (a Python with DrissionPage installed) and
TestV22A02A03DevDatabase. Credentials/fixture address arrive on stdin only.
"""

import json
import sys
import time

from DrissionPage import Chromium, ChromiumOptions
from DrissionPage.errors import ContextLostError


def wait(tab, expression, timeout=8):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            if tab.run_js("return " + expression):
                return
        except ContextLostError:
            pass  # A refresh may replace the JavaScript context between polls.
        time.sleep(0.1)
    raise AssertionError("UI condition failed: " + expression + "\n" + tab.run_js("return document.body.innerText"))


def click(tab, text):
    print("UI step: " + text, flush=True)
    wait(tab, "[...document.querySelectorAll('button')].some(x => x.textContent.trim() === " + json.dumps(text) + " && !x.disabled)")
    tab.run_js("""
        const button = [...document.querySelectorAll('button')].find(x => x.textContent.trim() === arguments[0]);
        if (!button || button.disabled) throw new Error('button unavailable: ' + arguments[0]);
        button.click();
    """, text)


config = json.load(sys.stdin)
browser = Chromium(ChromiumOptions().auto_port().headless())
tab = browser.new_tab()
try:
    print("UI step: launching Chromium and opening built web", flush=True)
    tab.get(config["base"])
    tab.run_js("document.cookie = 'token=' + arguments[0] + ';path=/'", config["token"])
    tab.get(config["base"] + "/#/app/collectors/" + config["collector_id"])
    wait(tab, "document.querySelector('#collector-name')?.value.includes('fixture') || document.querySelector('#collector-name')?.value.includes('edited')")
    tab.run_js("""
        const input = document.querySelector('#collector-name');
        input.value = 'UI autosave';
        input.dispatchEvent(new Event('input', {bubbles:true}));
    """)
    wait(tab, "document.querySelector('.save-state')?.textContent === '已保存'")
    click(tab, "检查草稿结构")
    wait(tab, "document.querySelector('.validation-state')?.dataset.state === 'valid'")
    click(tab, "打开浏览器会话")
    wait(tab, "document.querySelector('.browser-session-meta [data-state]')?.dataset.state === 'ready'")
    wait(tab, "document.querySelector('.browser-session-frame')?.naturalWidth > 0")
    # Reload restores the same in-memory session from sessionStorage.
    original = tab.run_js("return sessionStorage.getItem(arguments[0])", "scrapio:v22:session:" + config["collector_id"])
    tab.refresh()
    wait(tab, "document.querySelector('.browser-session-meta [data-state]')?.dataset.state === 'ready'")
    restored = tab.run_js("return sessionStorage.getItem(arguments[0])", "scrapio:v22:session:" + config["collector_id"])
    assert restored == original, "reload created a second session"
    # Fixture fails the first close: the UI must retain ID and show failure.
    click(tab, "关闭会话")
    wait(tab, "document.querySelector('.editor-browser .app-error')?.textContent.includes('重试关闭')")
    assert tab.run_js("return sessionStorage.getItem(arguments[0])", "scrapio:v22:session:" + config["collector_id"]) == original
    assert not tab.run_js("return !!document.querySelector('.browser-session-frame')")
    click(tab, "关闭会话")
    wait(tab, "[...document.querySelectorAll('button')].some(x => x.textContent.trim() === '打开浏览器会话')")
    assert not tab.run_js("return sessionStorage.getItem(arguments[0])", "scrapio:v22:session:" + config["collector_id"])
    click(tab, "打开浏览器会话")
    wait(tab, "document.querySelector('.browser-session-meta [data-state]')?.dataset.state === 'ready'")
    assert tab.run_js("return sessionStorage.getItem(arguments[0])", "scrapio:v22:session:" + config["collector_id"]) != original
    click(tab, "关闭会话")
    wait(tab, "!document.querySelector('.browser-session-meta')")
    print("UI smoke passed: autosave, validation, open, restore, failed close, retry and reopen")
finally:
    tab.close()
    browser.quit()
