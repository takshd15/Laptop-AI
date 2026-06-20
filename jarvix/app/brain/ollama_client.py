import requests
from app.config import OLLAMA_MODEL, OLLAMA_URL

# Keep the model resident between commands. On low-RAM machines Ollama otherwise
# evicts it, and the next call pays a ~100s cold reload. This is the single
# biggest latency win on CPU-only hardware.
KEEP_ALIVE = "25m"


def ask_ollama(
    system_prompt: str,
    user_prompt: str,
    json_mode: bool = False,
    timeout: int = 120,
    num_predict: int | None = None,
) -> str:
    payload = {
        "model": OLLAMA_MODEL,
        "stream": False,
        "keep_alive": KEEP_ALIVE,
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt},
        ],
    }

    # When json_mode is on, Ollama constrains the model to emit valid JSON.
    if json_mode:
        payload["format"] = "json"

    # Cap generated tokens to keep CPU generation fast and bounded.
    if num_predict is not None:
        payload["options"] = {"num_predict": num_predict}

    response = requests.post(OLLAMA_URL, json=payload, timeout=timeout)
    response.raise_for_status()

    data = response.json()
    return data["message"]["content"]


def warmup() -> None:
    """Load the model into memory so later calls are fast. Cheap, tiny generation."""
    ask_ollama(
        system_prompt="Reply with one word.",
        user_prompt="ready",
        num_predict=5,
        timeout=300,
    )
