"""Wake loop for Jarvix v2: double-clap -> listen -> transcribe -> act -> speak.

Decoupled from the rest of the app: the caller injects ``handle_text`` (which
parses + executes an intent and returns a response string) and ``speak``. This
keeps the voice plumbing here and the routing logic in main.
"""

from __future__ import annotations

from typing import Callable

from app.voice.wakeword import command_after_wake_word


_MUSIC_WORDS = (
    "music",
    "song",
    "spotify",
    "play",
    "pause",
    "resume",
    "skip",
    "next",
    "previous",
    "volume",
    "louder",
    "quieter",
)


def _looks_like_music_command(text: str) -> bool:
    t = text.lower()
    return any(word in t for word in _MUSIC_WORDS)


def _command_from_wake_text(text: str | None) -> str:
    return command_after_wake_word(text)


def wake_loop(
    handle_text: Callable[[str], str],
    speak: Callable[[str], None],
    wait_for_wake: Callable[[], str | None] | None = None,
    announce: str = "Yes?",
    once: bool = False,
) -> None:
    """Run the wake loop. ``wait_for_wake`` blocks until the user triggers Jarvix
    (defaults to the double-clap detector). Set ``once=True`` for a single command."""
    # Imported lazily so non-voice commands don't pay the import cost.
    from app.voice.recorder import record, MicUnavailable
    from app.voice.stt import transcribe
    from app.tools import music as music_tool
    from app.config import VOICE_RECORD_SECONDS

    if wait_for_wake is None:
        from app.voice.clap_detector import wait_for_double_clap
        wait_for_wake = lambda: wait_for_double_clap(verbose=True)

    while True:
        print("Waiting for wake trigger...")
        paused_music = False
        try:
            wake_text = wait_for_wake()
            wake_command = _command_from_wake_text(wake_text)
            if announce:
                speak(announce)
            if wake_command:
                print("Listening...")
                text = wake_command
                audio = None
            else:
                paused_music = music_tool.pause_if_playing()
                print("Listening...")
                audio = record(
                    max_seconds=float(VOICE_RECORD_SECONDS),
                    trailing_silence=1.1,
                    start_timeout=5.0,
                )
        except MicUnavailable as exc:
            print(f"Microphone unavailable: {exc}")
            speak("My microphone isn't available, so I can't listen right now.")
            return
        if audio is not None and audio.size == 0:
            print("Heard no command audio.")
            speak("I didn't hear a command.")
            if paused_music:
                music_tool.resume_playback()
            if once:
                return
            continue

        if audio is not None:
            text = transcribe(audio)
            if not text:
                print("Command audio was captured, but transcription was empty.")
                speak("I couldn't make that out.")
                if paused_music:
                    music_tool.resume_playback()
                if once:
                    return
                continue

        print(f"Heard: {text}")
        response = handle_text(text)
        if response:
            print(f"Jarvix: {response}")
            speak(response)
        if paused_music and not _looks_like_music_command(text):
            music_tool.resume_playback()

        if once:
            return
