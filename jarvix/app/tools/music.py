"""Basic music control for Jarvix v2 (Spotify desktop app).

Two layers, both Windows-only and dependency-free:

1. Media keys (play/pause, next, previous) via the OS virtual-key codes.
   These are global media keys, so they reach Spotify even when it is not the
   focused window. They control whatever media app is playing.

2. ``play(query)`` opens Spotify to a search for the song using the
   ``spotify:`` protocol URI, then nudges play/pause. Requires the Spotify
   desktop app to be installed (it registers the ``spotify:`` handler).
"""

from __future__ import annotations

import ctypes
import os
import time
from urllib.parse import quote

# Windows virtual-key codes for global media controls.
VK_MEDIA_NEXT_TRACK = 0xB0
VK_MEDIA_PREV_TRACK = 0xB1
VK_MEDIA_PLAY_PAUSE = 0xB3
VK_VOLUME_MUTE = 0xAD
VK_VOLUME_DOWN = 0xAE
VK_VOLUME_UP = 0xAF

KEYEVENTF_KEYUP = 0x0002


def _tap_key(vk: int) -> None:
    """Press and release a virtual key."""
    user32 = ctypes.windll.user32  # type: ignore[attr-defined]  # Windows-only
    user32.keybd_event(vk, 0, 0, 0)
    user32.keybd_event(vk, 0, KEYEVENTF_KEYUP, 0)


def play_pause() -> str:
    _tap_key(VK_MEDIA_PLAY_PAUSE)
    return "Toggled play/pause"


def next_track() -> str:
    _tap_key(VK_MEDIA_NEXT_TRACK)
    return "Skipped to next track"


def previous_track() -> str:
    _tap_key(VK_MEDIA_PREV_TRACK)
    return "Went to previous track"


def volume_up(steps: int = 2) -> str:
    for _ in range(max(1, steps)):
        _tap_key(VK_VOLUME_UP)
    return "Turned the volume up"


def volume_down(steps: int = 2) -> str:
    for _ in range(max(1, steps)):
        _tap_key(VK_VOLUME_DOWN)
    return "Turned the volume down"


def mute() -> str:
    _tap_key(VK_VOLUME_MUTE)
    return "Toggled mute"


def play(query: str) -> str:
    """Open Spotify to a search for ``query`` and start playback.

    Spotify does not auto-play the first search result via the URI alone, so we
    open the search and then send a play/pause tap as a best-effort start.
    """
    query = query.strip()
    if not query:
        raise ValueError("Nothing to play - empty query.")

    os.startfile(f"spotify:search:{quote(query)}")  # type: ignore[attr-defined]
    # Give the app a moment to focus the search before nudging playback.
    time.sleep(2.5)
    _tap_key(VK_MEDIA_PLAY_PAUSE)
    return f"Searching Spotify for {query}"
