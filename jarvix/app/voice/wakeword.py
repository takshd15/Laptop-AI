"""Voice wake-word detector for Jarvix v2: listen for "hey jarvis".

Reuses the existing offline STT (faster-whisper). It is energy-gated by the
recorder: it only transcribes when there's actually speech, so the CPU is idle
when the room is quiet. Matching is deliberately loose to absorb the small STT
model's mishears of the name (jarvis / jarvix / jervis ...).
"""

from __future__ import annotations

import re

from app.config import MIC_SILENCE_THRESHOLD, WAKE_DEBUG, WAKE_WORD
from app.voice.recorder import record
from app.voice.stt import transcribe_wake_word


_KNOWN_VARIANTS = {
    "jarvis",
    "jarvix",
    "jervis",
    "jorvis",
    "jorvix",
    "jarves",
    "jarviss",
    "garvis",
    "zarvis",
    "zaravas",
    "charvis",
    "joris",
    "jorwes",
    "doris",
    "chavez",
    "harvest",
}


def _edit_distance(left: str, right: str) -> int:
    """Small dependency-free Levenshtein distance for one spoken word."""
    previous = list(range(len(right) + 1))
    for row, char_left in enumerate(left, start=1):
        current = [row]
        for column, char_right in enumerate(right, start=1):
            current.append(
                min(
                    current[-1] + 1,
                    previous[column] + 1,
                    previous[column - 1] + (char_left != char_right),
                )
            )
        previous = current
    return previous[-1]


def _is_wake_token(token: str) -> bool:
    token = token.lower()
    configured = re.sub(r"[^a-z]", "", WAKE_WORD.lower())
    if token == configured or token in _KNOWN_VARIANTS:
        return True
    # Fuzzy matching is deliberately narrow. Broad fragments such as "jar" and
    # "travis" caused music and ordinary speech to look like wake phrases.
    if not 5 <= len(token) <= 7 or token[0] not in "jgzcs":
        return False
    return min(_edit_distance(token, "jarvis"), _edit_distance(token, "jarvix")) <= 2


def _wake_span(text: str) -> tuple[int, int] | None:
    """Return a wake-token span only near the beginning of the utterance."""
    words = list(re.finditer(r"[A-Za-z]+", text))
    for index, match in enumerate(words[:3]):
        if match.group(0).lower() == "hey":
            continue
        if _is_wake_token(match.group(0)):
            return match.span()
    return None


def _matches(text: str) -> bool:
    return _wake_span(text) is not None


def command_after_wake_word(text: str | None) -> str:
    """Extract a same-utterance command after any accepted wake variant."""
    if not text:
        return ""
    span = _wake_span(text)
    if span is None:
        return ""
    command = text[span[1]:].strip(" ,.?!:-")
    if command.lower() in {"", "can you", "can you please", "please"}:
        return ""
    return command


def wait_for_wake_word(verbose: bool = False) -> str:
    """Block until the wake word is heard. Returns the matching transcript.

    Raises ``MicUnavailable`` (from ``record``) if there's no usable mic.
    """
    while True:
        audio = record(
            max_seconds=5.0,
            silence_threshold=MIC_SILENCE_THRESHOLD,
            trailing_silence=1.0,
            start_timeout=3.0,
        )
        if audio.size == 0:
            continue  # no speech this window; keep listening
        text = transcribe_wake_word(audio)
        if not text:
            continue
        if _matches(text):
            if verbose:
                print(f"  heard: {text!r}")
            return text
        if verbose and WAKE_DEBUG:
            print(f"  ignored: {text!r}")
