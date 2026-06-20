"""Local text-to-speech for Jarvix v2.

Fails soft: if the speech engine can't initialise (no audio device, driver
missing, headless), we print the text instead of crashing the caller. Jarvix
should never die just because it can't talk.
"""

import pyttsx3


def speak(text: str) -> None:
    try:
        engine = pyttsx3.init()
        engine.setProperty("rate", 175)
        engine.say(text)
        engine.runAndWait()
    except Exception as exc:  # engine init / driver / device failure
        print(f"[tts unavailable, printing instead] {text}  ({exc})")
