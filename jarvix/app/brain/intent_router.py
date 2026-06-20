"""Rule-based intent router for Jarvix v2.

Transcribed voice text -> a structured Intent -> a tool call. Deterministic on
purpose: local 3B models are unreliable for routing common commands, so the
frequent stuff (open app/folder, music, briefing) is matched by rules. Anything
unmatched becomes ``unknown`` and is answered safely instead of guessed at.

``execute`` only runs the safe, deterministic intents (apps, folders, music).
Higher-level intents (brief / today / scan_mail) are returned to the caller
(main) which owns their orchestration and any confirmation gates.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

from app.tools import desktop, music

# Intent names
OPEN_APP = "open_app"
OPEN_FOLDER = "open_folder"
MUSIC_PLAY_PAUSE = "music_play_pause"
MUSIC_NEXT = "music_next"
MUSIC_PREVIOUS = "music_previous"
MUSIC_VOLUME_UP = "music_volume_up"
MUSIC_VOLUME_DOWN = "music_volume_down"
BRIEF = "brief"
TODAY = "today"
SCAN_MAIL = "scan_mail"
DRAFT_EMAIL = "draft_email"
SEND_EMAIL = "send_email"
UNKNOWN = "unknown"

# Intents that execute() handles directly (safe + deterministic).
_SIMPLE = {
    OPEN_APP,
    OPEN_FOLDER,
    MUSIC_PLAY_PAUSE,
    MUSIC_NEXT,
    MUSIC_PREVIOUS,
    MUSIC_VOLUME_UP,
    MUSIC_VOLUME_DOWN,
}

# Phrases that separate the recipient from the message ("...saying I'll be late").
_SAY_MARKERS = ("saying", "telling them", "telling him", "telling her", "to say", "that", "about")


@dataclass
class Intent:
    name: str
    arg: str | None = None
    raw: str = ""
    recipient: str | None = None
    message: str | None = None


def _extract_recipient(text: str) -> str | None:
    """First name token after 'to', e.g. 'email to Alex saying...' -> 'Alex'."""
    m = re.search(r"\bto\s+([A-Za-z][\w.\-']*)", text, re.I)
    return m.group(1) if m else None


def _extract_message(text: str) -> str:
    """The instruction after a say-marker, e.g. '...saying I'll be late' -> "I'll be late"."""
    pattern = r"\b(?:" + "|".join(re.escape(m) for m in _SAY_MARKERS) + r")\b\s+(.+)"
    m = re.search(pattern, text, re.I)
    return m.group(1).strip() if m else ""


def _clean(text: str) -> str:
    return " ".join(text.lower().strip().strip(".!?,").split())


def _match_alias(text: str, names: list[str]) -> str | None:
    """Return the longest allowlisted alias that appears in the text, if any."""
    found = [n for n in names if n in text]
    return max(found, key=len) if found else None


def parse(text: str) -> Intent:
    """Map raw transcribed text to an Intent. Never raises."""
    t = _clean(text)
    if not t:
        return Intent(UNKNOWN, raw=text)

    folders = desktop.list_folders()
    apps = desktop.list_apps()

    # 1. Explicit "open ..." commands come first.
    if "open" in t.split() or t.startswith("open"):
        if "folder" in t or "directory" in t:
            fname = _match_alias(t, folders)
            return Intent(OPEN_FOLDER, fname, text) if fname else Intent(UNKNOWN, raw=text)
        aname = _match_alias(t, apps)
        if aname:
            return Intent(OPEN_APP, aname, text)
        fname = _match_alias(t, folders)
        if fname:
            return Intent(OPEN_FOLDER, fname, text)
        return Intent(UNKNOWN, raw=text)

    # 2. Email drafting / sending (must come before plain email-reading).
    if ("email" in t or "mail" in t) and any(
        k in t for k in ("draft", "write", "compose", "send")
    ):
        recipient = _extract_recipient(text)
        message = _extract_message(text)
        name = SEND_EMAIL if ("send" in t.split()) else DRAFT_EMAIL
        return Intent(name, raw=text, recipient=recipient, message=message)

    # 3. Email reading.
    if any(k in t for k in ("email", "emails", "mail", "inbox")):
        return Intent(SCAN_MAIL, raw=text)

    # 3. Day / schedule / plan.
    if any(k in t for k in ("my day", "today", "schedule", "my plan", "plan for", "to do", "to-do")):
        return Intent(TODAY, raw=text)

    # 4. Briefing.
    if any(k in t for k in ("brief", "briefing", "welcome", "catch me up", "good morning")):
        return Intent(BRIEF, raw=text)

    # 5. Music.
    if any(k in t for k in ("volume up", "louder", "turn it up", "turn up")):
        return Intent(MUSIC_VOLUME_UP, raw=text)
    if any(k in t for k in ("volume down", "quieter", "turn it down", "turn down")):
        return Intent(MUSIC_VOLUME_DOWN, raw=text)
    if any(k in t for k in ("next song", "next track", "skip")):
        return Intent(MUSIC_NEXT, raw=text)
    if any(k in t for k in ("previous", "last song", "go back", "prev")):
        return Intent(MUSIC_PREVIOUS, raw=text)
    if any(k in t for k in ("pause", "resume", "play music", "stop music", "play pause")):
        return Intent(MUSIC_PLAY_PAUSE, raw=text)

    return Intent(UNKNOWN, raw=text)


def execute(intent: Intent) -> str | None:
    """Run a safe/deterministic intent and return a spoken response.

    Returns None for higher-level intents (brief/today/scan_mail/unknown) so the
    caller can handle them. Catches tool errors and returns a safe message.
    """
    if intent.name not in _SIMPLE:
        return None

    try:
        if intent.name == OPEN_APP:
            return desktop.open_app(intent.arg or "")
        if intent.name == OPEN_FOLDER:
            return desktop.open_folder(intent.arg or "")
        if intent.name == MUSIC_PLAY_PAUSE:
            return music.play_pause()
        if intent.name == MUSIC_NEXT:
            return music.next_track()
        if intent.name == MUSIC_PREVIOUS:
            return music.previous_track()
        if intent.name == MUSIC_VOLUME_UP:
            return music.volume_up()
        if intent.name == MUSIC_VOLUME_DOWN:
            return music.volume_down()
    except desktop.DesktopError as exc:
        return str(exc)
    except Exception as exc:  # never let a tool crash the voice loop
        return f"Sorry, that failed: {exc}"

    return None
