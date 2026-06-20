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

import json
import re
from dataclasses import dataclass

from app.brain.ollama_client import ask_ollama
from app.tools import desktop, music

# Intent names
OPEN_APP = "open_app"
OPEN_FOLDER = "open_folder"
MUSIC_PLAY_PAUSE = "music_play_pause"
MUSIC_NEXT = "music_next"
MUSIC_PREVIOUS = "music_previous"
MUSIC_VOLUME_UP = "music_volume_up"
MUSIC_VOLUME_DOWN = "music_volume_down"
MUSIC_PLAY_QUERY = "music_play_query"
BRIEF = "brief"
TODAY = "today"
READ_EMAILS = "read_emails"
SCAN_MAIL = "scan_mail"
DRAFT_EMAIL = "draft_email"
SEND_EMAIL = "send_email"
QUESTION = "question"
CONVERSATION = "conversation"
CLARIFICATION_NEEDED = "clarification_needed"
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
    MUSIC_PLAY_QUERY,
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
    t = " ".join(text.lower().strip().strip(".!?,").split())
    t = re.sub(r"^(hey\s+)?jarvis\b[:,]?\s*", "", t)
    for filler in (
        "can you please ",
        "could you please ",
        "would you please ",
        "please ",
        "can you ",
        "could you ",
        "would you ",
    ):
        if t.startswith(filler):
            t = t[len(filler):]
            break
    return t.strip()


def _match_alias(text: str, names: list[str]) -> str | None:
    """Return the longest allowlisted alias that appears in the text, if any."""
    found = [n for n in names if n in text]
    return max(found, key=len) if found else None


def _extract_music_query(text: str) -> str | None:
    t = _clean(text)
    quoted = re.search(r"['\"]([^'\"]+)['\"]", text)
    if quoted:
        return quoted.group(1).strip()

    m = re.search(r"\b(?:play|search for|find)\s+(.+)", t)
    if not m:
        return None

    query = m.group(1).strip()
    query = re.sub(r"\b(on|in)\s+spotify\b", "", query).strip()
    query = re.sub(r"\b(song|track|music)\b", "", query).strip()
    query = re.sub(r"^(a|an|some|the)\s+", "", query).strip()
    query = re.sub(r"\s+", " ", query)
    if query in {"", "a", "an", "some", "the", "spotify"}:
        return None
    return query or None


def _parse_rules(text: str) -> Intent:
    """Fast deterministic parser for common spoken commands."""
    t = _clean(text)
    if not t:
        return Intent(UNKNOWN, raw=text)
    if t in {
        "can you",
        "can you please",
        "please",
        "and you",
        "by calendar",
        "bye",
        "you",
    }:
        return Intent(CLARIFICATION_NEEDED, raw=text)
    if "by calendar" in t:
        return Intent(CLARIFICATION_NEEDED, raw=text)

    folders = desktop.list_folders()
    apps = desktop.list_apps()

    # 1. Music commands are common in speech and should tolerate polite phrasing.
    if any(k in t for k in ("volume up", "louder", "turn it up", "turn up")):
        return Intent(MUSIC_VOLUME_UP, raw=text)
    if any(k in t for k in ("volume down", "quieter", "turn it down", "turn down")):
        return Intent(MUSIC_VOLUME_DOWN, raw=text)
    if any(
        k in t
        for k in (
            "change song",
            "change the song",
            "change track",
            "change the track",
            "next song",
            "next the song",
            "next track",
            "skip",
            "skip song",
            "skip this song",
            "skip track",
            "skip this track",
            "something else",
            "another song",
        )
    ):
        return Intent(MUSIC_NEXT, raw=text)
    if any(k in t for k in ("previous", "last song", "go back", "prev")):
        return Intent(MUSIC_PREVIOUS, raw=text)
    if "spotify" in t and any(k in t.split() for k in ("open", "start", "play")):
        query = _extract_music_query(text)
        return Intent(MUSIC_PLAY_QUERY, query, text)
    if any(k in t for k in ("play music", "play a music", "start music")):
        return Intent(MUSIC_PLAY_QUERY, raw=text)
    if any(k in t for k in ("pause", "resume", "play music", "stop music", "play pause")):
        return Intent(MUSIC_PLAY_PAUSE, raw=text)
    if t.startswith(("play ", "search for ", "find ")):
        query = _extract_music_query(text)
        return Intent(MUSIC_PLAY_QUERY, query, text) if query else Intent(MUSIC_PLAY_PAUSE, raw=text)

    # 2. Explicit "open ..." commands.
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

    # 3. Email drafting / sending (must come before plain email-reading).
    if ("email" in t or "mail" in t) and any(
        k in t for k in ("draft", "write", "compose", "send")
    ):
        recipient = _extract_recipient(text)
        message = _extract_message(text)
        name = SEND_EMAIL if ("send" in t.split()) else DRAFT_EMAIL
        return Intent(name, raw=text, recipient=recipient, message=message)

    # 4. Gmail reading vs calendar extraction.
    if any(k in t for k in ("email", "emails", "mail", "inbox")):
        if any(k in t for k in ("calendar", "calender", "event", "events", "schedule", "add")):
            return Intent(SCAN_MAIL, raw=text)
        if any(k in t for k in ("read", "tell", "summarize", "summary", "today", "latest", "recent", "check")):
            return Intent(READ_EMAILS, raw=text)
        return Intent(SCAN_MAIL, raw=text)

    # 5. Day / schedule / plan.
    if any(
        k in t
        for k in (
            "my day",
            "today",
            "calendar",
            "schedule",
            "my plan",
            "plan for",
            "to do",
            "to-do",
            "supposed to do",
        )
    ):
        return Intent(TODAY, raw=text)

    # 6. Briefing.
    if any(k in t for k in ("brief", "briefing", "welcome", "catch me up", "good morning")):
        return Intent(BRIEF, raw=text)

    # 7. Obvious questions/chit-chat skip the classifier and go straight to a spoken answer.
    if t.startswith(
        (
            "what ",
            "what's ",
            "why ",
            "who ",
            "who's ",
            "when ",
            "where ",
            "how ",
            "explain ",
            "tell me ",
            "define ",
        )
    ):
        return Intent(QUESTION, raw=text)
    if any(k in t for k in ("how are you", "thank you", "thanks", "nice", "cool")):
        return Intent(CONVERSATION, raw=text)

    return Intent(UNKNOWN, raw=text)


def _coerce_llm_intent(data: dict, raw: str) -> Intent:
    name = str(data.get("intent") or UNKNOWN).strip().lower()
    allowed = {
        OPEN_APP,
        OPEN_FOLDER,
        MUSIC_PLAY_PAUSE,
        MUSIC_NEXT,
        MUSIC_PREVIOUS,
        MUSIC_VOLUME_UP,
        MUSIC_VOLUME_DOWN,
        MUSIC_PLAY_QUERY,
        BRIEF,
        TODAY,
        READ_EMAILS,
        SCAN_MAIL,
        DRAFT_EMAIL,
        SEND_EMAIL,
        QUESTION,
        CONVERSATION,
        CLARIFICATION_NEEDED,
        UNKNOWN,
    }
    if name not in allowed:
        return Intent(UNKNOWN, raw=raw)

    arg_value = data.get("arg")
    arg = str(arg_value).strip() if arg_value is not None else None
    recipient = data.get("recipient")
    recipient = str(recipient).strip() if recipient is not None else None
    message = data.get("message")
    message = str(message).strip() if message is not None else None

    if name == OPEN_APP and arg:
        arg = _match_alias(arg.lower(), desktop.list_apps())
        if not arg:
            return Intent(UNKNOWN, raw=raw)
    if name == OPEN_FOLDER and arg:
        arg = _match_alias(arg.lower(), desktop.list_folders())
        if not arg:
            return Intent(UNKNOWN, raw=raw)

    return Intent(name, arg=arg, raw=raw, recipient=recipient, message=message)


def _parse_with_llm(text: str) -> Intent:
    apps = ", ".join(desktop.list_apps())
    folders = ", ".join(desktop.list_folders())
    system_prompt = f"""
You classify one spoken command for Jarvix.

Return ONLY valid JSON with keys: intent, arg, recipient, message.
Use null when a field is not needed.

Allowed intents:
- {OPEN_APP}: open a configured app. arg must be one of: {apps}
- {OPEN_FOLDER}: open a configured folder. arg must be one of: {folders}
- {MUSIC_PLAY_QUERY}: play/open Spotify or play a requested song. arg is the song/artist/Spotify URL, or null for just Spotify/music.
- {MUSIC_PLAY_PAUSE}: pause, resume, or toggle current playback.
- {MUSIC_NEXT}: next/change/skip song.
- {MUSIC_PREVIOUS}: previous/back/last song.
- {MUSIC_VOLUME_UP}: louder/volume up.
- {MUSIC_VOLUME_DOWN}: quieter/volume down.
- {READ_EMAILS}: read or summarize Gmail/inbox messages.
- {SCAN_MAIL}: check Gmail for calendar-worthy events/deadlines and add approved items to Calendar.
- {TODAY}: summarize today's schedule/plan/tasks.
- {BRIEF}: brief/catch up/good morning.
- {DRAFT_EMAIL}: draft an email. recipient/message when present.
- {SEND_EMAIL}: send an email. recipient/message when present.
- {QUESTION}: knowledge, explanation, advice, or reasoning.
- {CONVERSATION}: casual chat that does not need a tool.
- {CLARIFICATION_NEEDED}: too vague, incomplete, or likely speech recognition failure.
- {UNKNOWN}: anything else.

Safety:
- Never invent app or folder aliases.
- Use {SCAN_MAIL} only when the command asks to find/add calendar events from email.
- Use {READ_EMAILS} when the command only asks to read/check/summarize email.
- Use {CLARIFICATION_NEEDED} for fragments like "can you please", "by calendar", "and you", or nonsense.
"""
    user_prompt = f"Command: {text}"
    try:
        raw = ask_ollama(
            system_prompt,
            user_prompt,
            json_mode=True,
            timeout=4,
            num_predict=80,
        )
        data = json.loads(raw)
    except Exception:
        return Intent(UNKNOWN, raw=text)
    return _coerce_llm_intent(data, text)


def parse(text: str, use_llm: bool = True) -> Intent:
    """Map raw transcribed text to an Intent. Never raises.

    Rules handle common commands instantly. The local LLM is used only as a
    fallback for unfamiliar wording.
    """
    intent = _parse_rules(text)
    if intent.name != UNKNOWN or not use_llm:
        return intent
    return _parse_with_llm(text)


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
        if intent.name == MUSIC_PLAY_QUERY:
            if intent.arg:
                return music.play(intent.arg)
            music.open_spotify()
            return music.play_pause()
    except desktop.DesktopError as exc:
        return str(exc)
    except Exception as exc:  # never let a tool crash the voice loop
        return f"Sorry, that failed: {exc}"

    return None
