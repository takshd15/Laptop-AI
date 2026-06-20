import requests
from app.config import OLLAMA_MODEL, OLLAMA_URL


def ask_ollama(
    system_prompt: str,
    user_prompt: str,
    json_mode: bool = False,
    timeout: int = 120,
) -> str:
    payload = {
        "model": OLLAMA_MODEL,
        "stream": False,
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt},
        ],
    }

    # When json_mode is on, Ollama constrains the model to emit valid JSON.
    if json_mode:
        payload["format"] = "json"

    response = requests.post(OLLAMA_URL, json=payload, timeout=timeout)
    response.raise_for_status()

    data = response.json()
    return data["message"]["content"]
