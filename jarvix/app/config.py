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

TIMEZONE = os.getenv("TIMEZONE", "Europe/Amsterdam")

USER_DISPLAY_NAME = os.getenv("USER_DISPLAY_NAME", "Mr Taksh")
JARVIX_GREETING = os.getenv("JARVIX_GREETING", "Welcome back")

# Speech-to-text (faster-whisper, CPU). "tiny.en" is fastest; "base.en" more accurate.
STT_MODEL = os.getenv("STT_MODEL", "tiny.en")
STT_LANGUAGE = os.getenv("STT_LANGUAGE", "en")

# Max seconds to record one voice command (recorder also stops early on silence).
VOICE_RECORD_SECONDS = int(os.getenv("VOICE_RECORD_SECONDS", "5"))

# How Jarvix is woken. "clap" = double-clap detector. Reserved for future modes.
WAKE_MODE = os.getenv("WAKE_MODE", "clap")

# Welcome routine: only SAFE actions auto-run on wake. Set any to "" to disable.
AUTO_OPEN_APP_ON_WAKE = os.getenv("AUTO_OPEN_APP_ON_WAKE", "cursor")
AUTO_OPEN_FOLDER_ON_WAKE = os.getenv("AUTO_OPEN_FOLDER_ON_WAKE", "jarvix")
AUTO_START_MUSIC_ON_WAKE = os.getenv("AUTO_START_MUSIC_ON_WAKE", "true").lower() in (
    "1", "true", "yes", "on",
)
