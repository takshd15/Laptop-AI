"""Safe desktop control for Jarvix v2.

Only opens apps and folders that are explicitly listed in
``app/memory/aliases.json``. There is intentionally NO path to run an
arbitrary shell command or open an arbitrary path the user names, so the
model can never turn the laptop into spicy toast.
"""

from __future__ import annotations

import json
import os
import shutil
from pathlib import Path

ALIASES_FILE = Path(__file__).resolve().parents[1] / "memory" / "aliases.json"


class DesktopError(Exception):
    """Raised for unknown aliases or missing targets. Caller shows the message."""


def _load_aliases() -> dict:
    if not ALIASES_FILE.exists():
        raise DesktopError(f"Alias file not found: {ALIASES_FILE}")
    with open(ALIASES_FILE, "r", encoding="utf-8") as fh:
        return json.load(fh)


def _normalize(name: str) -> str:
    return name.strip().lower()


def list_folders() -> list[str]:
    return sorted(_load_aliases().get("folders", {}))


def list_apps() -> list[str]:
    return sorted(_load_aliases().get("apps", {}))


def open_folder(alias: str) -> str:
    """Open a folder from the allowlist in Explorer. Returns a message."""
    folders = _load_aliases().get("folders", {})
    key = _normalize(alias)
    if key not in folders:
        known = ", ".join(sorted(folders)) or "(none configured)"
        raise DesktopError(f"Unknown folder '{alias}'. Known folders: {known}")

    path = Path(folders[key])
    if not path.is_dir():
        raise DesktopError(f"Folder for '{alias}' does not exist: {path}")

    os.startfile(str(path))  # type: ignore[attr-defined]  # Windows-only
    return f"Opening folder {path}"


def _resolve_app(value: str) -> Path | None:
    """Resolve an allowlisted app value to a real executable, or None."""
    candidate = Path(value)
    if candidate.is_absolute():
        return candidate if candidate.exists() else None
    found = shutil.which(value)
    return Path(found) if found else None


def open_app(alias: str) -> str:
    """Launch an app from the allowlist. Returns a message."""
    apps = _load_aliases().get("apps", {})
    key = _normalize(alias)
    if key not in apps:
        known = ", ".join(sorted(apps)) or "(none configured)"
        raise DesktopError(f"Unknown app '{alias}'. Known apps: {known}")

    resolved = _resolve_app(apps[key])
    if resolved is None:
        raise DesktopError(
            f"App '{alias}' is configured as '{apps[key]}' but it was not found. "
            f"Is it installed and on PATH?"
        )

    os.startfile(str(resolved))  # type: ignore[attr-defined]  # Windows-only
    return f"Opening {alias}"
