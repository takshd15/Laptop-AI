"""Small in-memory dialogue state for filling missing voice-command details."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass, field

from app.brain import intent_router
from app.tools import desktop


@dataclass
class Pending:
    kind: str
    values: dict[str, str] = field(default_factory=dict)


class VoiceDialogue:
    """Remember exactly one incomplete command between wake interactions."""

    def __init__(self) -> None:
        self.pending: Pending | None = None

    @staticmethod
    def _value(text: str) -> str:
        value = " ".join(text.strip(" ,.?!").split())
        lowered = value.lower()
        for prefix in ("it is ", "it's ", "the city is ", "the folder is "):
            if lowered.startswith(prefix):
                return value[len(prefix):].strip()
        return value

    def _continue(self, text: str) -> tuple[intent_router.Intent | None, str | None]:
        assert self.pending is not None
        pending = self.pending
        value = self._value(text)
        if value.lower() in {"cancel", "never mind", "nevermind", "stop"}:
            self.pending = None
            return None, "Okay, cancelled."

        if pending.kind == "email_recipient":
            pending.values["recipient"] = value
            pending.kind = "email_message"
            return None, "What should the email say?"
        if pending.kind == "email_message":
            self.pending = None
            name = pending.values["intent"]
            return intent_router.Intent(
                name,
                raw=text,
                recipient=pending.values["recipient"],
                message=value,
            ), None
        if pending.kind == "weather_location":
            self.pending = None
            return intent_router.Intent(intent_router.WEATHER, arg=value, raw=text), None
        if pending.kind == "folder":
            aliases = desktop.list_folders()
            lowered = value.lower()
            match = next((name for name in aliases if name == lowered or name in lowered), None)
            if not match:
                return None, f"I don't know that folder. Try {', '.join(aliases)}."
            self.pending = None
            return intent_router.Intent(intent_router.OPEN_FOLDER, arg=match, raw=text), None
        if pending.kind == "comparison":
            self.pending = None
            original = pending.values["original"]
            return intent_router.Intent(intent_router.QUESTION, raw=f"{original} {value}"), None

        self.pending = None
        return None, "I didn't catch that clearly. Can you repeat it?"

    def handle(self, text: str, execute: Callable[[intent_router.Intent], str]) -> str:
        if self.pending is not None:
            intent, response = self._continue(text)
            if response is not None:
                return response
            if intent is None:
                return "I didn't catch that clearly. Can you repeat it?"
            return execute(intent)

        intent = intent_router.parse(text)
        if intent.name in {intent_router.SEND_EMAIL, intent_router.DRAFT_EMAIL}:
            values = {"intent": intent.name}
            if not intent.recipient:
                self.pending = Pending("email_recipient", values)
                return "Who should I email?"
            values["recipient"] = intent.recipient
            if not (intent.message or "").strip():
                self.pending = Pending("email_message", values)
                return "What should the email say?"
        if intent.name == intent_router.WEATHER and not intent.arg:
            self.pending = Pending("weather_location")
            return "Which city should I check?"
        if intent.name == intent_router.CLARIFICATION_NEEDED and intent.arg == "folder":
            self.pending = Pending("folder")
            return "Which folder should I open?"
        if intent.name == intent_router.CLARIFICATION_NEEDED and intent.arg == "comparison":
            self.pending = Pending("comparison", {"original": intent.raw})
            return "What two things should I compare?"
        return execute(intent)
