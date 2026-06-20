"""Command parsing seam for Jarvix v2.

Today the deterministic rule-based parsing lives in ``intent_router.parse``.
This module is the stable entry point the rest of the app should import, so a
future smarter parser (LLM-assisted slot filling, fuzzy aliases, etc.) can be
swapped in here without touching callers.
"""

from __future__ import annotations

from app.brain.intent_router import Intent, parse as _parse


def parse_command(text: str) -> Intent:
    """Turn raw (typed or transcribed) text into a structured Intent."""
    return _parse(text)
