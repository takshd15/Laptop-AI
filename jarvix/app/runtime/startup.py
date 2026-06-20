"""Login entry point for Jarvix: starts the clap wake loop.

Launched by ``start_jarvix.bat`` from the Windows Startup folder. The wake loop
stays SILENT until it hears a double clap (the welcome routine only runs after
the first clap), so it never spams you on login.
"""

import sys

from app.main import app


def main() -> None:
    # Drive the Typer app exactly as `python -m app.main wake` would, so option
    # defaults resolve correctly (don't call the command function directly).
    sys.argv = ["jarvix", "wake"]
    app()


if __name__ == "__main__":
    main()
