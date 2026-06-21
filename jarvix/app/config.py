from pathlib import Path
from dotenv import load_dotenv
import os

ROOT = Path(__file__).resolve().parents[1]
load_dotenv(ROOT / ".env")

OLLAMA_MODEL = os.getenv("OLLAMA_MODEL", "llama3.2:3b")
OLLAMA_URL = os.getenv("OLLAMA_URL", "http://localhost:11434/api/chat")

GOOGLE_CREDENTIALS_FILE = ROOT / os.getenv(
    "GOOGLE_CREDENTIALS_FILE",
    "secrets/google_credentials.json",
)

GOOGLE_TOKEN_FILE = ROOT / os.getenv(
    "GOOGLE_TOKEN_FILE",
    "secrets/google_token.json",
)

SPOTIFY_CLIENT_ID = os.getenv("SPOTIFY_CLIENT_ID", "")
SPOTIFY_CLIENT_SECRET = os.getenv("SPOTIFY_CLIENT_SECRET", "")
SPOTIFY_REDIRECT_URI = os.getenv(
    "SPOTIFY_REDIRECT_URI",
    "http://127.0.0.1:8888/callback",
)
SPOTIFY_TOKEN_FILE = ROOT / os.getenv(
    "SPOTIFY_TOKEN_FILE",
    "secrets/spotify_token.json",
)

TIMEZONE = os.getenv("TIMEZONE", "Europe/Amsterdam")
DEFAULT_WEATHER_LOCATION = os.getenv("DEFAULT_WEATHER_LOCATION", "")
WEATHER_TIMEOUT_SECONDS = float(os.getenv("WEATHER_TIMEOUT_SECONDS", "5"))

USER_DISPLAY_NAME = os.getenv("USER_DISPLAY_NAME", "Mr Taksh")
JARVIX_GREETING = os.getenv("JARVIX_GREETING", "Welcome back")

# Local Windows text-to-speech. Voice hints are matched against installed SAPI
# voice names/ids in order, so Jarvix prefers a natural English male voice when
# one is available without requiring a cloud TTS account.
TTS_RATE = int(os.getenv("TTS_RATE", "165"))
TTS_VOLUME = float(os.getenv("TTS_VOLUME", "1.0"))
TTS_VOICE_HINTS = [
    hint.strip().lower()
    for hint in os.getenv("TTS_VOICE_HINTS", "george,david,male,english").split(",")
    if hint.strip()
]
ELEVENLABS_API_KEY = os.getenv("ELEVENLABS_API_KEY", "")
ELEVENLABS_VOICE_ID = os.getenv("ELEVENLABS_VOICE_ID", "pNInz6obpgDQGcFmaJgB")
ELEVENLABS_MODEL_ID = os.getenv("ELEVENLABS_MODEL_ID", "eleven_flash_v2_5")
ELEVENLABS_OUTPUT_FORMAT = os.getenv("ELEVENLABS_OUTPUT_FORMAT", "pcm_16000")
ELEVENLABS_TIMEOUT_SECONDS = float(os.getenv("ELEVENLABS_TIMEOUT_SECONDS", "8"))
ELEVENLABS_STABILITY = float(os.getenv("ELEVENLABS_STABILITY", "0.62"))
ELEVENLABS_SIMILARITY_BOOST = float(os.getenv("ELEVENLABS_SIMILARITY_BOOST", "0.86"))
ELEVENLABS_STYLE = float(os.getenv("ELEVENLABS_STYLE", "0.08"))
ELEVENLABS_SPEAKER_BOOST = os.getenv("ELEVENLABS_SPEAKER_BOOST", "true").lower() in (
    "1", "true", "yes", "on",
)

# Speech-to-text (faster-whisper, CPU). "tiny.en" is fastest; "base.en" more accurate.
STT_MODEL = os.getenv("STT_MODEL", "base.en")
STT_LANGUAGE = os.getenv("STT_LANGUAGE", "en")

# Max seconds to record one voice command (recorder also stops early on silence).
VOICE_RECORD_SECONDS = int(os.getenv("VOICE_RECORD_SECONDS", "5"))
MIC_SILENCE_THRESHOLD = float(os.getenv("MIC_SILENCE_THRESHOLD", "0.0015"))
WAKE_DEBUG = os.getenv("WAKE_DEBUG", "false").lower() in ("1", "true", "yes", "on")

# How Jarvix is woken:
#   "wakeword" = say "hey jarvis" (default, most reliable)
#   "clap"     = double-clap detector
#   "enter"    = press Enter (foreground terminal only)
WAKE_MODE = os.getenv("WAKE_MODE", "wakeword")

# The spoken wake word (matched loosely against the STT transcript).
WAKE_WORD = os.getenv("WAKE_WORD", "jarvis")

# Clap loudness floor (0..1 peak amplitude) for clap mode. Adaptive above this.
CLAP_THRESHOLD = float(os.getenv("CLAP_THRESHOLD", "0.06"))

# Welcome routine: only SAFE actions auto-run on wake. Set any to "" to disable.
AUTO_OPEN_APP_ON_WAKE = os.getenv("AUTO_OPEN_APP_ON_WAKE", "cursor")
AUTO_OPEN_FOLDER_ON_WAKE = os.getenv("AUTO_OPEN_FOLDER_ON_WAKE", "jarvix")
AUTO_START_MUSIC_ON_WAKE = os.getenv("AUTO_START_MUSIC_ON_WAKE", "true").lower() in (
    "1", "true", "yes", "on",
)
AUTO_MUSIC_QUERY_ON_WAKE = os.getenv(
    "AUTO_MUSIC_QUERY_ON_WAKE",
    "Should I Stay or Should I Go",
)
AUTO_MUSIC_URI_ON_WAKE = os.getenv("AUTO_MUSIC_URI_ON_WAKE", "")
