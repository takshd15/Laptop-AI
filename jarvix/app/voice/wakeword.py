"""Voice wake-word detector for Jarvix v2: listen for "hey jarvis".

Reuses the existing offline STT (faster-whisper). It is energy-gated by the
recorder: it only transcribes when there's actually speech, so the CPU is idle
when the room is quiet. Matching is deliberately loose to absorb the small STT
model's mishears of the name (jarvis / jarvix / jervis ...).
"""

from __future__ import annotations

import re

from app.config import WAKE_DEBUG, WAKE_WORD
from app.voice.recorder import record
from app.voice.stt import transcribe


def _matches(text: str) -> bool:
    t = re.sub(r"[^a-z ]+", " ", text.lower())
    t = re.sub(r"\s+", " ", t).strip()
    # "jarv" catches jarvis/jarvix/jarvi and most mishears; plus a few variants.
    if "jarv" in t:
        return True
    w = WAKE_WORD.lower()
    variants = (
        w,
        "hey " + w,
        "jervis",
        "jarviss",
        "jarvis",
        "jarvis please",
        "jarvis can",
        "travis",
        "charvis",
        "jars",
        "jar",
    )
    return any(v in t for v in variants)


def wait_for_wake_word(verbose: bool = False) -> str:
    """Block until the wake word is heard. Returns the matching transcript.

    Raises ``MicUnavailable`` (from ``record``) if there's no usable mic.
    """
    while True:
        audio = record(
            max_seconds=5.0,
            silence_threshold=0.003,
            trailing_silence=1.0,
            start_timeout=3.0,
        )
        if audio.size == 0:
            continue  # no speech this window; keep listening
        text = transcribe(audio)
        if not text:
            continue
        if _matches(text):
            if verbose:
                print(f"  heard: {text!r}")
            return text
        if verbose and WAKE_DEBUG:
            print(f"  ignored: {text!r}")
