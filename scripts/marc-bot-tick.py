#!/usr/bin/env python3
"""
marc-bot-tick.py — pick the oldest ready marc question and send it to Telegram.

Invoked by cron `*/30 9-18 * * 1-5 TZ=Asia/Bangkok`. Single-shot: picks one
question, sends it, exits. Designed to share /var/lib/marc/state/state.db with
marc-server (WAL mode allows concurrent reads + serialized writes).

The Telegram bot itself runs as the existing telegram-commands.service — this
script just does the periodic outbound send. Inbound (button callbacks, text
commands) is handled in telegram-commands.py.

No external Python deps beyond stdlib + requests.
"""

import json
import logging
import os
import sqlite3
import sys
from datetime import datetime, timezone
from pathlib import Path

import requests

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

SCRIPT_DIR = Path(__file__).resolve().parent
CONF_PATH = SCRIPT_DIR / "telegram.conf"
LOG_DIR = Path.home() / "kanoonth" / "logs"
LOG_DIR.mkdir(parents=True, exist_ok=True)
LOG_PATH = LOG_DIR / "marc-bot-tick.log"

MARC_STATE_DB = os.environ.get(
    "MARC_STATE_DB",
    os.path.expanduser("~/.marc/state.db"),
)

logging.basicConfig(
    filename=str(LOG_PATH),
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(message)s",
)
log = logging.getLogger("marc-bot-tick")


def load_telegram_conf():
    token = chat = ""
    with open(CONF_PATH) as f:
        for line in f:
            line = line.strip()
            if line.startswith("TELEGRAM_BOT_TOKEN="):
                token = line.split("=", 1)[1].strip('"')
            elif line.startswith("TELEGRAM_CHAT_ID="):
                chat = line.split("=", 1)[1].strip('"')
    if not token or not chat:
        log.error("missing TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID in %s", CONF_PATH)
        sys.exit(1)
    return token, int(chat)


# ---------------------------------------------------------------------------
# SQLite — marc state.db (WAL mode, set busy_timeout to ride out concurrent writes)
# ---------------------------------------------------------------------------

def open_marc_db(path: str) -> sqlite3.Connection:
    if not os.path.exists(path):
        log.error("marc state.db not found at %s; run `marc-server init` first", path)
        sys.exit(1)
    conn = sqlite3.connect(path, timeout=10.0)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA busy_timeout = 5000")
    conn.execute("PRAGMA foreign_keys = ON")
    return conn


def pick_oldest_ready(conn: sqlite3.Connection):
    cur = conn.execute(
        """
        SELECT question_id, project_id, situation, question, option_a, option_b,
               principle_tested, durability_score, obviousness_score
        FROM pending_questions
        WHERE status = 'ready'
        ORDER BY generated_at ASC
        LIMIT 1
        """
    )
    return cur.fetchone()


def mark_sent(conn: sqlite3.Connection, qid: int, message_id: int) -> None:
    now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    conn.execute(
        """
        UPDATE pending_questions
        SET status = 'sent',
            sent_at = ?,
            telegram_message_id = ?
        WHERE question_id = ?
        """,
        (now, message_id, qid),
    )
    conn.commit()


# ---------------------------------------------------------------------------
# Telegram outbound
# ---------------------------------------------------------------------------

def format_html(q) -> str:
    return (
        f"<b>marc-question-{q['question_id']}</b>\n\n"
        f"<b>Situation</b>\n{q['situation']}\n\n"
        f"<b>Question</b>\n{q['question']}\n\n"
        f"<b>A)</b> {q['option_a']}\n"
        f"<b>B)</b> {q['option_b']}\n\n"
        f"<i>Principle: {q['principle_tested']}</i>\n"
        f"<i>Reply text: <code>a {q['question_id']} A|B|S</code> or <code>a {q['question_id']} O reason</code></i>"
    )


def inline_keyboard(qid: int) -> dict:
    return {
        "inline_keyboard": [[
            {"text": "A", "callback_data": f"marc:a:{qid}"},
            {"text": "B", "callback_data": f"marc:b:{qid}"},
            {"text": "Other", "callback_data": f"marc:o:{qid}"},
            {"text": "Skip", "callback_data": f"marc:s:{qid}"},
        ]]
    }


def send_question(token: str, chat_id: int, q) -> int | None:
    api = f"https://api.telegram.org/bot{token}/sendMessage"
    body = {
        "chat_id": chat_id,
        "text": format_html(q),
        "parse_mode": "HTML",
        "reply_markup": json.dumps(inline_keyboard(q["question_id"])),
        "disable_web_page_preview": True,
    }
    try:
        r = requests.post(api, data=body, timeout=15)
    except requests.RequestException as e:
        log.error("sendMessage failed for q=%d: %s", q["question_id"], e)
        return None
    if r.status_code != 200:
        log.error("sendMessage non-200 for q=%d: %d %s", q["question_id"], r.status_code, r.text[:200])
        return None
    payload = r.json()
    if not payload.get("ok"):
        log.error("sendMessage ok=false for q=%d: %s", q["question_id"], payload)
        return None
    return payload["result"]["message_id"]


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main() -> int:
    token, chat_id = load_telegram_conf()
    conn = open_marc_db(MARC_STATE_DB)
    try:
        q = pick_oldest_ready(conn)
        if q is None:
            log.info("no ready question; nothing to send")
            return 0
        msg_id = send_question(token, chat_id, q)
        if msg_id is None:
            log.warning("send failed for q=%d; row left at status=ready", q["question_id"])
            return 0
        mark_sent(conn, q["question_id"], msg_id)
        log.info("sent q=%d as message_id=%d", q["question_id"], msg_id)
        return 0
    finally:
        conn.close()


if __name__ == "__main__":
    sys.exit(main())
