"""Text-to-speech for Jarvix v2.

ElevenLabs is the primary voice. Local Windows TTS is a soft fallback so Jarvix
doesn't die just because the network, quota, or audio device has a bad moment.
"""

import re

import numpy as np
import pyttsx3
import requests
import sounddevice as sd

from app.config import (
    ELEVENLABS_API_KEY,
    ELEVENLABS_MODEL_ID,
    ELEVENLABS_OUTPUT_FORMAT,
    ELEVENLABS_SIMILARITY_BOOST,
    ELEVENLABS_SPEAKER_BOOST,
    ELEVENLABS_STABILITY,
    ELEVENLABS_STYLE,
    ELEVENLABS_TIMEOUT_SECONDS,
    ELEVENLABS_VOICE_ID,
    TTS_RATE,
    TTS_VOICE_HINTS,
    TTS_VOLUME,
)

ELEVENLABS_TTS_URL = "https://api.elevenlabs.io/v1/text-to-speech"


def _voice_score(voice) -> int:
    haystack = f"{getattr(voice, 'name', '')} {getattr(voice, 'id', '')}".lower()
    return sum(1 for hint in TTS_VOICE_HINTS if hint in haystack)


def _engine():
    engine = pyttsx3.init()
    engine.setProperty("rate", TTS_RATE)
    engine.setProperty("volume", TTS_VOLUME)

    voices = engine.getProperty("voices") or []
    best = max(voices, key=_voice_score, default=None)
    if best and _voice_score(best) > 0:
        engine.setProperty("voice", best.id)

    return engine


def _clean_for_speech(text: str) -> str:
    text = re.sub(r"[*_`#>]+", "", text)
    text = re.sub(r"\s+", " ", text).strip()
    return text


def _speak_local(text: str) -> None:
    engine = _engine()
    engine.say(text)
    engine.runAndWait()


def _elevenlabs_sample_rate() -> int:
    match = re.search(r"pcm_(\d+)", ELEVENLABS_OUTPUT_FORMAT)
    return int(match.group(1)) if match else 16000


def _speak_elevenlabs(text: str) -> None:
    if not ELEVENLABS_API_KEY:
        raise RuntimeError("ELEVENLABS_API_KEY is not configured.")

    response = requests.post(
        f"{ELEVENLABS_TTS_URL}/{ELEVENLABS_VOICE_ID}",
        params={"output_format": ELEVENLABS_OUTPUT_FORMAT},
        headers={
            "xi-api-key": ELEVENLABS_API_KEY,
            "Accept": "audio/wav",
            "Content-Type": "application/json",
        },
        json={
            "text": text,
            "model_id": ELEVENLABS_MODEL_ID,
            "voice_settings": {
                "stability": ELEVENLABS_STABILITY,
                "similarity_boost": ELEVENLABS_SIMILARITY_BOOST,
                "style": ELEVENLABS_STYLE,
                "use_speaker_boost": ELEVENLABS_SPEAKER_BOOST,
            },
        },
        timeout=ELEVENLABS_TIMEOUT_SECONDS,
    )
    response.raise_for_status()

    if ELEVENLABS_OUTPUT_FORMAT.startswith("pcm_"):
        audio = np.frombuffer(response.content, dtype=np.int16).astype(np.float32) / 32768.0
        sd.play(audio, samplerate=_elevenlabs_sample_rate())
        sd.wait()
        return

    raise RuntimeError(f"Unsupported ElevenLabs output format: {ELEVENLABS_OUTPUT_FORMAT}")


def speak(text: str) -> None:
    text = _clean_for_speech(text)
    if not text:
        return
    try:
        _speak_elevenlabs(text)
    except Exception as exc:
        try:
            print(f"[elevenlabs unavailable, using local tts] {exc}")
            _speak_local(text)
        except Exception as local_exc:  # engine init / driver / device failure
            print(f"[tts unavailable, printing instead] {text}  ({local_exc})")
