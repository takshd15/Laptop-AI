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
from app.safety.permissions import needs_confirmation
from app.models import EventCandidate

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


def _run_scan(limit: int, days: int, use_prefilter: bool):
    """Shared scan pipeline: read -> (prefilter) -> extract -> validate -> dedupe -> confirm -> write."""
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

    # Safety gate: writing to the calendar always requires explicit confirmation.
    assert needs_confirmation("create_calendar_event")
    approved = _select_candidates(accepted)

    if not approved:
        console.print("[yellow]Nothing approved. No events created.[/yellow]")
        return

    console.print(f"\n[bold cyan]Creating {len(approved)} event(s)...[/bold cyan]")
    for c in approved:
        try:
            created = create_event_from_candidate(c)
            console.print(f"  [green]Added:[/green] {c.title}  {created.get('htmlLink', '')}")
        except Exception as exc:  # surface the failure, keep going
            console.print(f"  [red]Failed:[/red] {c.title} - {exc}")


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
def today():
    """Read updated Calendar + recent emails and generate a today/tomorrow plan.

    Deadlines come from the calendar (all-day events written by scan-mail), so
    this command runs a single model call and stays fast - it does NOT re-extract
    from every email.
    """
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
    plan = ask_ollama(system_prompt, user_prompt, timeout=300, num_predict=300)

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
