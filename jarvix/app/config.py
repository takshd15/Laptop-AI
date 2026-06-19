from pathlib import Path
from dotenv import load_dotenv
import os

ROOT = Path(__file__).resolve().parents[1]
load_dotenv(ROOT / ".env")

OLLAMA_MODEL = os.getenv("OLLAMA_MODEL", "llama3.1:8b")
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
