"""Double-clap wake detector for Jarvix v2.

A clap is a short, very loud transient. We watch the mic for peak amplitude
spikes and require TWO spikes within a short window (but not too close together)
to wake. A single clap, or steady loud noise like typing, is ignored.

Tuning knobs:
- ``threshold``    how loud a spike must be (0..1 of full scale).
- ``min_gap``      claps closer than this are treated as one (ignores echoes).
- ``max_gap``      the second clap must land within this of the first.
"""

from __future__ import annotations

import time

import numpy as np
import sounddevice as sd

from app.voice.recorder import MicUnavailable

SAMPLE_RATE = 16000
_BLOCK_SECONDS = 0.02  # 20 ms resolution for catching sharp transients


def detect_double_clap(peaks: list[float], dt: float, threshold: float,
                       min_gap: float, max_gap: float) -> bool:
    """Pure helper (testable): given per-block peak amplitudes, is there a
    double clap? ``dt`` is the seconds per block."""
    last_clap_t: float | None = None
    armed = True
    for i, peak in enumerate(peaks):
        now = i * dt
        if peak >= threshold and armed:
            armed = False  # require a dip before the next onset counts
            if last_clap_t is not None and min_gap <= (now - last_clap_t) <= max_gap:
                return True
            last_clap_t = now
        elif peak < threshold * 0.5:
            armed = True
            if last_clap_t is not None and (now - last_clap_t) > max_gap:
                last_clap_t = None
    return False


def wait_for_double_clap(
    threshold: float = 0.25,
    min_gap: float = 0.12,
    max_gap: float = 0.8,
    timeout: float | None = None,
) -> bool:
    """Block until a double clap is heard. Returns True on detection, or False
    if ``timeout`` seconds pass first (None = wait forever)."""
    block = int(SAMPLE_RATE * _BLOCK_SECONDS)
    last_clap_t: float | None = None
    armed = True
    start = time.time()

    try:
        with sd.InputStream(
            samplerate=SAMPLE_RATE, channels=1, dtype="float32", blocksize=block
        ) as stream:
            while True:
                if timeout is not None and (time.time() - start) > timeout:
                    return False

                data, _ = stream.read(block)
                samples = data[:, 0]
                peak = float(np.max(np.abs(samples))) if samples.size else 0.0
                now = time.time()

                if peak >= threshold and armed:
                    armed = False
                    if last_clap_t is not None and min_gap <= (now - last_clap_t) <= max_gap:
                        return True
                    last_clap_t = now
                elif peak < threshold * 0.5:
                    armed = True
                    if last_clap_t is not None and (now - last_clap_t) > max_gap:
                        last_clap_t = None
    except Exception as exc:  # PortAudioError, no device, etc.
        raise MicUnavailable(str(exc)) from exc
