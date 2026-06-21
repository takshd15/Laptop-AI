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


def transcribe_wake_word(audio: np.ndarray, language: str | None = None) -> str:
    """Accuracy-biased transcription for the short wake-listening window.

    Wake recognition is worth a little extra CPU: a wider beam recovers muffled
    speech, while ``hotwords`` teaches Whisper the likely spellings without
    changing how ordinary commands are transcribed.
    """
    if audio is None or audio.size == 0:
        return ""

    # Bring quiet speech into a useful range without aggressively amplifying
    # room noise. Whisper accepts float32 mono audio in [-1, 1].
    samples = np.asarray(audio, dtype=np.float32)
    rms = float(np.sqrt(np.mean(np.square(samples))))
    if 0 < rms < 0.06:
        gain = min(4.0, 0.06 / rms)
        samples = np.clip(samples * gain, -1.0, 1.0)

    segments, _ = _model().transcribe(
        samples,
        language=language or STT_LANGUAGE,
        beam_size=5,
        hotwords=(
            "Jarvis Jarvix Jervis Jorvis Jorvix Garvis Zarvis Charvis "
            "Joris Jorwes Doris Chavez"
        ),
        condition_on_previous_text=False,
    )
    return "".join(segment.text for segment in segments).strip()
