"""Wake loop for Jarvix v2: double-clap -> listen -> transcribe -> act -> speak.

Decoupled from the rest of the app: the caller injects ``handle_text`` (which
parses + executes an intent and returns a response string) and ``speak``. This
keeps the voice plumbing here and the routing logic in main.
"""

from __future__ import annotations

from typing import Callable


def wake_loop(
    handle_text: Callable[[str], str],
    speak: Callable[[str], None],
    announce: str = "Yes?",
    once: bool = False,
) -> None:
    """Run the clap-wake loop. Set ``once=True`` to handle a single command."""
    # Imported lazily so non-voice commands don't pay the import cost.
    from app.voice.clap_detector import wait_for_double_clap
    from app.voice.recorder import record, MicUnavailable
    from app.voice.stt import transcribe

    while True:
        print("Waiting for double clap...")
        try:
            wait_for_double_clap()
            print("Listening...")
            speak(announce)
            audio = record()
        except MicUnavailable as exc:
            print(f"Microphone unavailable: {exc}")
            speak("My microphone isn't available, so I can't listen right now.")
            return
        if audio.size == 0:
            speak("I didn't hear a command.")
            if once:
                return
            continue

        text = transcribe(audio)
        if not text:
            speak("I couldn't make that out.")
            if once:
                return
            continue

        print(f"Heard: {text}")
        response = handle_text(text)
        if response:
            print(f"Jarvix: {response}")
            speak(response)

        if once:
            return
