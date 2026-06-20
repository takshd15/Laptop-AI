"""Speech-to-text for Jarvix v2 using faster-whisper (offline, CPU).

The model is loaded lazily and cached, so the first transcription pays the load
cost (and a one-time model download) and later calls are fast.
"""

from __future__ import annotations

from functools import lru_cache

import numpy as np

from app.config import STT_MODEL, STT_LANGUAGE


@lru_cache(maxsize=1)
def _model():
    # int8 keeps memory + CPU cost low on the 7.6 GB laptop.
    from faster_whisper import WhisperModel

    return WhisperModel(STT_MODEL, device="cpu", compute_type="int8")


def transcribe(audio: np.ndarray, language: str | None = None) -> str:
    """Transcribe a 16 kHz float32 mono array to text."""
    if audio is None or audio.size == 0:
        return ""

    segments, _ = _model().transcribe(
        audio,
        language=language or STT_LANGUAGE,
        beam_size=1,  # greedy: fastest on CPU
    )
    return "".join(seg.text for seg in segments).strip()
