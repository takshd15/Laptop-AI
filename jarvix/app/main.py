import typer
from rich.console import Console

from app.brain.ollama_client import ask_ollama, warmup as _warmup
from app.tools.gmail import get_recent_emails
from app.tools.calendar import (
    get_upcoming_events,
    get_events_window,
    create_event_from_candidate,
)
from app.tools.extractor import extract_candidates, validate_candidates
from app.tools.dedupe import find_duplicates
from app.tools.prefilter import prefilter, is_worthy
from app.safety.permissions import needs_confirmation, confirm
from app.models import EventCandidate
from app.voice.tts import speak
from app.tools.desktop import open_app as desktop_open_app, open_folder as desktop_open_folder, DesktopError
from app.tools import music as music_tool
from app.tools import email_actions
from app.brain import intent_router
from app.config import (
    USER_DISPLAY_NAME,
    JARVIX_GREETING,
    AUTO_OPEN_APP_ON_WAKE,
    AUTO_OPEN_FOLDER_ON_WAKE,
    AUTO_START_MUSIC_ON_WAKE,
)

app = typer.Typer(help="Jarvix v0 - local AI assistant for Gmail + Calendar.")
console = Console()


# --------------------------------------------------------------------------- #
# Rendering / confirmation helpers
# --------------------------------------------------------------------------- #
def _format_candidate(index: int, c: EventCandidate, dupes: list[dict]) -> str:
    when = c.date or "?"
    if c.start_time:
        when += f" {c.start_time}"
        if c.end_time:
            when += f"-{c.end_time}"
    elif c.all_day:
        when += " (all day)"

    lines = [
        f"[bold]{index}.[/bold] {c.title}  [dim]({c.category}, conf {c.confidence:.2f})[/dim]",
        f"    When: {when}",
    ]
    if c.location:
        lines.append(f"    Where: {c.location}")
    if c.meeting_link:
        lines.append(f"    Link: {c.meeting_link}")
    lines.append(f"    Why: {c.reason}")
    lines.append(f"    Source: {c.source_subject}")
    if c.missing_fields:
        lines.append(f"    [yellow]Missing: {', '.join(c.missing_fields)}[/yellow]")
    if dupes:
        names = ", ".join(d.get("summary", "?") for d in dupes)
        lines.append(f"    [magenta]Possible duplicate of: {names}[/magenta]")
    return "\n".join(lines)


def _select_candidates(candidates: list[EventCandidate]) -> list[EventCandidate]:
    """Ask the user which candidates to add. Returns the approved subset."""
    answer = input(
        "\nApprove which? Type 'all', 'none', or numbers (e.g. 1,3): "
    ).strip().lower()

    if answer in ("", "none", "no", "n"):
        return []
    if answer in ("all", "a", "yes", "y"):
        return list(candidates)

    chosen: list[EventCandidate] = []
    for token in answer.replace(" ", "").split(","):
        if token.isdigit():
            idx = int(token) - 1
            if 0 <= idx < len(candidates):
                chosen.append(candidates[idx])
    return chosen


# --------------------------------------------------------------------------- #
# Intent dispatch (used by `route` and the wake loop)
# --------------------------------------------------------------------------- #
def run_intent(intent: intent_router.Intent) -> str:
    """Execute a parsed intent and return a short response to speak/print.

    Safe device/music intents run directly. Higher-level intents are handled
    here. Calendar writes (scan_mail) are intentionally NOT auto-run by voice -
    they require the terminal confirmation gate.
    """
    simple = intent_router.execute(intent)
    if simple is not None:
        return simple

    if intent.name == intent_router.BRIEF:
        return _brief_text()
    if intent.name == intent_router.TODAY:
        return _today_text()
    if intent.name == intent_router.SCAN_MAIL:
        # Reads aloud + asks for typed confirmation; never auto-writes the calendar.
        _run_scan(limit=12, days=10, use_prefilter=True, speak_summary=True)
        return ""  # all speaking already happened inside the scan
    if intent.name == intent_router.DRAFT_EMAIL:
        return _voice_email(intent.recipient, intent.message, send=False)
    if intent.name == intent_router.SEND_EMAIL:
        return _voice_email(intent.recipient, intent.message, send=True)
    return "Sorry, I did not understand that command."


def _voice_email(recipient_name: str | None, message: str | None, send: bool) -> str:
    """Draft an email (read it aloud), and optionally send it after confirmation.

    Drafting never sends. Sending requires an explicit typed 'yes' - no
    confirmation means no send. Unknown contacts are handled by asking the user
    to type the address.
    """
    if not recipient_name:
        return "I'm not sure who to email. Please try again and name the recipient."

    to_email = email_actions.resolve_recipient(recipient_name)
    if not to_email:
        speak(f"I don't know {recipient_name}'s email. Please type it.")
        to_email = input(f"Email address for {recipient_name} (blank to cancel): ").strip()
        if not to_email:
            return "Okay, cancelled. No email drafted."
        email_actions.save_contact(recipient_name, to_email)
        console.print(f"[dim]Saved {recipient_name} -> {to_email} to contacts.[/dim]")

    console.print("[bold cyan]Drafting...[/bold cyan]")
    draft = email_actions.build_draft(recipient_name, to_email, message or "")

    console.print(
        f"\n[bold green]Draft email[/bold green]\n"
        f"[bold]To:[/bold] {draft.to_name} <{draft.to_email}>\n"
        f"[bold]Subject:[/bold] {draft.subject}\n\n{draft.body}\n"
    )
    speak(f"Here is the draft for {recipient_name}. Subject: {draft.subject}. {draft.body}")

    if not send:
        return "I've drafted it but not sent it. Say send email when you want it out."

    # Phase 2.10 safety gate: routed through permissions.py. No "yes" = no send.
    assert needs_confirmation("send_email")
    speak("Should I send this?")
    if not confirm(f"Send email to {draft.to_email}\nSubject: {draft.subject}"):
        return "Okay, I did not send it."

    try:
        email_actions.send_email(draft)
    except Exception as exc:
        console.print(f"[red]Send failed: {exc}[/red]")
        return f"Sending failed: {exc}"

    return f"Sent to {recipient_name}."


def _humanize_actions(actions: list[str]) -> str:
    """['opening cursor', 'starting music'] -> 'Opening cursor and starting music'."""
    if len(actions) == 1:
        joined = actions[0]
    elif len(actions) == 2:
        joined = f"{actions[0]} and {actions[1]}"
    else:
        joined = ", ".join(actions[:-1]) + f", and {actions[-1]}"
    return joined[0].upper() + joined[1:]


def run_welcome() -> str:
    """The Jarvis-style wake routine: brief + safe auto-actions.

    Only SAFE actions auto-run (briefing, open app, open folder, music). No
    Gmail or calendar writes ever happen here. Returns the full text to speak;
    the briefing already opens with the greeting.
    """
    briefing = _brief_text()
    actions: list[str] = []

    if AUTO_OPEN_APP_ON_WAKE:
        try:
            desktop_open_app(AUTO_OPEN_APP_ON_WAKE)
            actions.append(f"opening {AUTO_OPEN_APP_ON_WAKE}")
        except DesktopError as exc:
            console.print(f"[yellow]Welcome: {exc}[/yellow]")

    if AUTO_OPEN_FOLDER_ON_WAKE:
        try:
            desktop_open_folder(AUTO_OPEN_FOLDER_ON_WAKE)
        except DesktopError as exc:
            console.print(f"[yellow]Welcome: {exc}[/yellow]")

    if AUTO_START_MUSIC_ON_WAKE:
        try:
            music_tool.play_pause()
            actions.append("starting music")
        except Exception as exc:  # media key should never crash the routine
            console.print(f"[yellow]Welcome: music failed: {exc}[/yellow]")

    if actions:
        return f"{briefing} {_humanize_actions(actions)}."
    return briefing


@app.command()
def welcome(silent: bool = typer.Option(False, "--silent", help="Print only, do not speak.")):
    """Run the welcome routine once: briefing + safe auto-actions."""
    text = run_welcome()
    console.print(f"\n[bold green]Jarvix:[/bold green]\n{text}")
    if not silent:
        speak(text)


@app.command()
def draft_email(
    to: str = typer.Argument(..., help="Recipient contact name, e.g. alex."),
    message: str = typer.Argument(..., help="What to say, e.g. \"I'll be late\"."),
):
    """Draft an email (does NOT send). Reads it aloud."""
    console.print(_voice_email(to, message, send=False))


@app.command()
def send_email(
    to: str = typer.Argument(..., help="Recipient contact name, e.g. alex."),
    message: str = typer.Argument(..., help="What to say, e.g. \"I'll be late\"."),
):
    """Draft an email, read it aloud, and send it after a typed confirmation."""
    console.print(_voice_email(to, message, send=True))


@app.command()
def reauth():
    """Delete the saved Google token and re-consent (needed after scope changes)."""
    from app.config import GOOGLE_TOKEN_FILE
    from app.tools.google_auth import get_google_credentials

    if GOOGLE_TOKEN_FILE.exists():
        GOOGLE_TOKEN_FILE.unlink()
        console.print(f"[yellow]Deleted {GOOGLE_TOKEN_FILE}[/yellow]")
    console.print("[bold cyan]Opening browser for Google consent (incl. Gmail send)...[/bold cyan]")
    get_google_credentials()
    console.print("[green]Re-authenticated. New scopes are active.[/green]")


@app.command()
def route(text: str = typer.Argument(..., help="Command text to route, e.g. 'open cursor'.")):
    """Parse a text command, show the matched intent, and execute it."""
    intent = intent_router.parse(text)
    console.print(f"[dim]intent={intent.name} arg={intent.arg}[/dim]")
    response = run_intent(intent)
    console.print(f"[green]{response}[/green]")


# --------------------------------------------------------------------------- #
# Commands
# --------------------------------------------------------------------------- #
@app.command()
def warmup():
    """Pre-load the model into memory so the next 'today'/'scan-mail' is fast."""
    console.print("[bold cyan]Warming up the model...[/bold cyan]")
    _warmup()
    console.print("[green]Model is resident (kept ~25 min). Run 'today' now.[/green]")


@app.command()
def test_brain():
    """Test the local Ollama brain."""
    answer = ask_ollama(
        system_prompt="You are Jarvix, a private local personal assistant.",
        user_prompt="Say hello in one sentence and explain what you are.",
    )
    console.print(answer)


def _spoken_candidate(c: EventCandidate) -> str:
    """A short, speakable description of one candidate, e.g. 'Interview on 2026-06-21 at 15:00'."""
    when = ""
    if c.date:
        when = f" on {c.date}"
        if c.start_time:
            when += f" at {c.start_time}"
    return f"{c.title}{when}"


def _run_scan(limit: int, days: int, use_prefilter: bool, speak_summary: bool = False):
    """Shared scan pipeline: read -> (prefilter) -> extract -> validate -> dedupe -> confirm -> write.

    When ``speak_summary`` is on (voice flow), Jarvix reads the candidates aloud
    before the confirmation prompt and speaks the final result. The calendar
    write still requires the same typed confirmation - voice never auto-writes.
    """
    console.print(f"[bold cyan]Reading last {days} days of Gmail (up to {limit})...[/bold cyan]")
    emails = get_recent_emails(limit=limit, days=days)
    console.print(f"  {len(emails)} emails fetched.")

    if use_prefilter:
        before = len(emails)
        emails = prefilter(emails)
        console.print(
            f"  Prefilter kept {len(emails)}/{before} likely-relevant email(s); "
            f"the model only sees those."
        )

    console.print("[bold cyan]Extracting candidates with the local model (cached when unchanged)...[/bold cyan]")
    raw_candidates = extract_candidates(emails)
    accepted, rejected = validate_candidates(raw_candidates)

    console.print(
        f"  {len(accepted)} valid candidate(s), {len(rejected)} rejected."
    )
    if rejected:
        for c, why in rejected:
            console.print(f"    [dim]rejected: {c.title or '(untitled)'} - {why}[/dim]")

    if not accepted:
        console.print("[yellow]No calendar-worthy candidates found. Nothing to add.[/yellow]")
        if speak_summary:
            speak("I didn't find any calendar-worthy items in your recent email.")
        return

    console.print("[bold cyan]Checking your calendar for duplicates...[/bold cyan]")
    existing = get_events_window()

    console.print("\n[bold green]Candidates:[/bold green]\n")
    dupes_by_index: list[list[dict]] = []
    for i, c in enumerate(accepted, start=1):
        dupes = find_duplicates(c, existing)
        dupes_by_index.append(dupes)
        console.print(_format_candidate(i, c, dupes))
        console.print("")

    if speak_summary:
        n = len(accepted)
        items = " ".join(
            f"{i}. {_spoken_candidate(c)}"
            + (" (possible duplicate)" if dupes_by_index[i - 1] else "")
            + "."
            for i, c in enumerate(accepted, start=1)
        )
        speak(
            f"I found {n} calendar-worthy item{'s' if n != 1 else ''}: {items} "
            f"Should I add {'them' if n != 1 else 'it'}?"
        )

    # Safety gate: writing to the calendar always requires explicit confirmation.
    assert needs_confirmation("create_calendar_event")
    approved = _select_candidates(accepted)

    if not approved:
        console.print("[yellow]Nothing approved. No events created.[/yellow]")
        if speak_summary:
            speak("Okay, I didn't add anything.")
        return

    console.print(f"\n[bold cyan]Creating {len(approved)} event(s)...[/bold cyan]")
    created_count = 0
    for c in approved:
        try:
            created = create_event_from_candidate(c)
            created_count += 1
            console.print(f"  [green]Added:[/green] {c.title}  {created.get('htmlLink', '')}")
        except Exception as exc:  # surface the failure, keep going
            console.print(f"  [red]Failed:[/red] {c.title} - {exc}")

    if speak_summary:
        speak(f"Done. I added {created_count} to your calendar.")


@app.command()
def scan_mail(
    limit: int = typer.Option(15, help="Max emails to scan."),
    days: int = typer.Option(10, help="How many days back to read (7-14)."),
):
    """Scan Gmail (prefiltered + cached), extract candidates, and add approved ones to Calendar."""
    _run_scan(limit=limit, days=days, use_prefilter=True)


@app.command()
def scan_mail_deep(
    limit: int = typer.Option(12, help="Max emails to scan."),
    days: int = typer.Option(14, help="How many days back to read."),
):
    """Thorough scan: no keyword prefilter, the model sees every fetched email."""
    _run_scan(limit=limit, days=days, use_prefilter=False)


@app.command()
def open_app(name: str = typer.Argument(..., help="App alias, e.g. cursor / chrome / vscode.")):
    """Open an allowlisted desktop app by alias."""
    try:
        msg = desktop_open_app(name)
        console.print(f"[green]{msg}[/green]")
    except DesktopError as exc:
        console.print(f"[red]{exc}[/red]")
        raise typer.Exit(code=1)


@app.command()
def open_folder(name: str = typer.Argument(..., help="Folder alias, e.g. jarvix / downloads.")):
    """Open an allowlisted folder by alias in Explorer."""
    try:
        msg = desktop_open_folder(name)
        console.print(f"[green]{msg}[/green]")
    except DesktopError as exc:
        console.print(f"[red]{exc}[/red]")
        raise typer.Exit(code=1)


@app.command()
def wake(
    with_welcome: bool = typer.Option(
        True, "--welcome/--no-welcome", help="Run the welcome routine on the first clap."
    ),
):
    """Stay awake: first clap runs the welcome routine, then double-clap to talk. Ctrl+C to stop."""
    from app.voice.wake_loop import wake_loop
    from app.voice.clap_detector import wait_for_double_clap
    from app.voice.recorder import MicUnavailable

    def handle(text: str) -> str:
        intent = intent_router.parse(text)
        console.print(f"[dim]intent={intent.name} arg={intent.arg}[/dim]")
        return run_intent(intent)

    console.print("[bold cyan]Jarvix is awake. Ctrl+C to stop.[/bold cyan]")
    try:
        if with_welcome:
            console.print("[cyan]Double-clap to start your day...[/cyan]")
            wait_for_double_clap()
            text = run_welcome()
            console.print(f"\n[bold green]Jarvix:[/bold green]\n{text}")
            speak(text)
        wake_loop(handle, speak)
    except MicUnavailable as exc:
        console.print(f"[red]Microphone unavailable: {exc}. Jarvix can't run the wake loop.[/red]")
        raise typer.Exit(code=1)
    except KeyboardInterrupt:
        console.print("\n[yellow]Jarvix going to sleep.[/yellow]")


@app.command()
def listen():
    """Record one spoken command from the mic and transcribe it (offline)."""
    from app.voice.recorder import record, MicUnavailable
    from app.voice.stt import transcribe

    console.print("[bold cyan]Listening... speak now.[/bold cyan]")
    try:
        audio = record()
    except MicUnavailable as exc:
        console.print(f"[red]Microphone unavailable: {exc}[/red]")
        raise typer.Exit(code=1)
    if audio.size == 0:
        console.print("[yellow]Heard nothing. Try again.[/yellow]")
        raise typer.Exit(code=1)

    console.print("[bold cyan]Transcribing...[/bold cyan]")
    text = transcribe(audio)
    if not text:
        console.print("[yellow]Could not make out any words.[/yellow]")
        raise typer.Exit(code=1)

    console.print(f"\n[bold green]You said:[/bold green] {text}")


@app.command()
def music(
    action: str = typer.Argument(
        ..., help="playpause | play | pause | next | prev | volume-up | volume-down | mute"
    )
):
    """Control playback with media keys: playpause, next, prev, volume-up/down, mute."""
    act = action.strip().lower()
    if act in ("playpause", "play", "pause", "toggle", "play-pause"):
        msg = music_tool.play_pause()
    elif act in ("next", "skip"):
        msg = music_tool.next_track()
    elif act in ("prev", "previous", "back"):
        msg = music_tool.previous_track()
    elif act in ("volume-up", "volumeup", "vol-up", "louder"):
        msg = music_tool.volume_up()
    elif act in ("volume-down", "volumedown", "vol-down", "quieter"):
        msg = music_tool.volume_down()
    elif act in ("mute", "unmute"):
        msg = music_tool.mute()
    else:
        console.print(
            f"[red]Unknown music action '{action}'. Use: playpause, next, prev, "
            f"volume-up, volume-down, mute.[/red]"
        )
        raise typer.Exit(code=1)
    console.print(f"[green]{msg}[/green]")


@app.command()
def play(song: str = typer.Argument(..., help="Song or artist to search and play on Spotify.")):
    """Open Spotify, search for a song, and start playback."""
    try:
        msg = music_tool.play(song)
        console.print(f"[green]{msg}[/green]")
    except ValueError as exc:
        console.print(f"[red]{exc}[/red]")
        raise typer.Exit(code=1)


def _brief_text() -> str:
    """Build the short spoken daily briefing text (no speaking/printing of result)."""
    console.print("[bold cyan]Reading calendar...[/bold cyan]")
    soon = get_upcoming_events(days=2, limit=20)
    window = get_events_window(days_ahead=14, limit=50)

    console.print("[bold cyan]Reading recent Gmail...[/bold cyan]")
    emails = get_recent_emails(limit=8, days=10)

    timed = [e for e in soon if "T" in (e.get("start") or "")]
    deadlines = [e for e in window if "T" not in (e.get("start") or "")]
    important = [e for e in emails if is_worthy(e)]

    events_text = "\n".join(
        f"- {e['start']} to {e['end']}: {e['summary']}" for e in timed
    ) or "(none)"
    deadlines_text = "\n".join(
        f"- {e['start']}: {e['summary']}" for e in deadlines
    ) or "(none)"
    emails_text = "\n".join(
        f"- From: {e['from']} | Subject: {e['subject']}" for e in important
    ) or "(none)"

    system_prompt = f"""
You are Jarvix, a private local personal assistant giving a SPOKEN briefing.

Rules:
- This text will be read aloud, so write flowing natural sentences. No bullet
  points, no markdown, no headings, no emojis.
- Start with exactly: "{JARVIX_GREETING} {USER_DISPLAY_NAME}."
- Keep it under 4 sentences total.
- Do NOT invent dates, meetings, or deadlines. Use only the data provided.
- Do NOT claim you sent, created, or added anything.
- If there is nothing scheduled, say the day looks open.
"""

    user_prompt = f"""
Today/tomorrow events:
{events_text}

Upcoming deadlines (next 14 days):
{deadlines_text}

Important recent emails:
{emails_text}

Give the spoken briefing now.
"""

    console.print("[bold cyan]Thinking...[/bold cyan]")
    return ask_ollama(system_prompt, user_prompt, timeout=300, num_predict=200).strip()


@app.command()
def brief(
    silent: bool = typer.Option(False, "--silent", help="Print only, do not speak."),
):
    """Speak a short spoken daily briefing: greeting + today's events + key emails."""
    briefing = _brief_text()
    console.print("\n[bold green]Jarvix:[/bold green]\n")
    console.print(briefing)
    if not silent:
        speak(briefing)


def _today_text() -> str:
    """Build the today/tomorrow plan text via a single model call."""
    console.print("[bold cyan]Reading calendar...[/bold cyan]")
    soon = get_upcoming_events(days=2, limit=20)
    window = get_events_window(days_ahead=14, limit=50)

    console.print("[bold cyan]Reading recent Gmail...[/bold cyan]")
    emails = get_recent_emails(limit=8, days=10)

    # Timed events (today/tomorrow) are "Calendar"; all-day items are "Deadlines".
    timed = [e for e in soon if "T" in (e.get("start") or "")]
    deadlines = [e for e in window if "T" not in (e.get("start") or "")]

    events_text = "\n".join(
        f"- {e['start']} to {e['end']}: {e['summary']} {('@ ' + e['location']) if e.get('location') else ''}"
        for e in timed
    ) or "(none)"

    emails_text = "\n".join(
        f"- From: {e['from']} | Subject: {e['subject']} | {e['snippet']}"
        for e in emails
    ) or "(none)"

    deadlines_text = "\n".join(
        f"- {e['start']}: {e['summary']}" for e in deadlines
    ) or "(none)"

    system_prompt = """
You are Jarvix, a private local personal assistant.

Rules:
- Do NOT invent dates, meetings, or deadlines. Use only the data provided.
- Do NOT claim you sent, created, or added anything.
- If information is missing, say so instead of guessing.
- Maximum 12 bullets total. No long explanations. No motivational essay.

Use exactly these four sections:
Calendar:
Deadlines:
Important emails:
Suggested plan:
"""

    user_prompt = f"""
Confirmed calendar events (today/tomorrow):
{events_text}

Recent emails:
{emails_text}

Upcoming all-day deadlines (next 14 days):
{deadlines_text}

Create a short, realistic plan for today and tomorrow.
"""

    console.print("[bold cyan]Thinking...[/bold cyan]")
    return ask_ollama(system_prompt, user_prompt, timeout=300, num_predict=300)


@app.command()
def today():
    """Read updated Calendar + recent emails and generate a today/tomorrow plan.

    Deadlines come from the calendar (all-day events written by scan-mail), so
    this command runs a single model call and stays fast - it does NOT re-extract
    from every email.
    """
    plan = _today_text()
    console.print("\n[bold green]Jarvix plan:[/bold green]\n")
    console.print(plan)


@app.command()
def today_fast():
    """Instant deterministic plan (no LLM): calendar + deadlines + top emails."""
    soon = get_upcoming_events(days=2, limit=20)
    window = get_events_window(days_ahead=14, limit=50)
    emails = get_recent_emails(limit=5, days=7)

    timed = [e for e in soon if "T" in (e.get("start") or "")]
    deadlines = [e for e in window if "T" not in (e.get("start") or "")]
    important = [e for e in emails if is_worthy(e)] or emails

    console.print("\n[bold green]Today (fast):[/bold green]\n")

    console.print("[bold]Calendar:[/bold]")
    if timed:
        for e in timed:
            when = e["start"][11:16] if "T" in e["start"] else e["start"]
            console.print(f"- {when} {e['summary']}")
    else:
        console.print("- (nothing scheduled today/tomorrow)")

    console.print("\n[bold]Deadlines:[/bold]")
    if deadlines:
        for e in deadlines[:5]:
            console.print(f"- {e['start']} {e['summary']}")
    else:
        console.print("- (none in the next 14 days)")

    console.print(f"\n[bold]Important emails:[/bold] ({len(important)})")
    for e in important[:3]:
        console.print(f"- {e['subject']}")

    console.print("\n[bold]Suggested:[/bold]")
    if timed:
        console.print(f"1. Prepare for: {timed[0]['summary']}.")
    else:
        console.print("1. No fixed events - use the time for deep work.")
    console.print("2. Triage the important emails above.")
    console.print("3. Re-check the calendar tonight.")
    console.print("\n[dim]Run 'today' for the full LLM plan.[/dim]")


@app.command()
def full_run(
    limit: int = typer.Option(12, help="Max emails to scan."),
    days: int = typer.Option(10, help="How many days back to read (7-14)."),
):
    """Run scan-mail first, then generate the today/tomorrow plan."""
    scan_mail(limit=limit, days=days)
    console.print("\n[bold]--- Daily plan ---[/bold]\n")
    today()


if __name__ == "__main__":
    app()
