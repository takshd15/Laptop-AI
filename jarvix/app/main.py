import typer
from rich.console import Console

from app.brain.ollama_client import ask_ollama
from app.tools.gmail import get_recent_emails
from app.tools.calendar import (
    get_upcoming_events,
    get_events_window,
    create_event_from_candidate,
)
from app.tools.extractor import extract_candidates, validate_candidates
from app.tools.dedupe import find_duplicates
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
def test_brain():
    """Test the local Ollama brain."""
    answer = ask_ollama(
        system_prompt="You are Jarvix, a private local personal assistant.",
        user_prompt="Say hello in one sentence and explain what you are.",
    )
    console.print(answer)


@app.command()
def scan_mail(
    limit: int = typer.Option(12, help="Max emails to scan."),
    days: int = typer.Option(10, help="How many days back to read (7-14)."),
):
    """Scan Gmail, extract event/deadline candidates, and add approved ones to Calendar."""
    console.print(f"[bold cyan]Reading last {days} days of Gmail (up to {limit})...[/bold cyan]")
    emails = get_recent_emails(limit=limit, days=days)
    console.print(f"  {len(emails)} emails fetched.")

    console.print("[bold cyan]Extracting candidates with the local model...[/bold cyan]")
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
def today():
    """Read updated Calendar + recent emails and generate a today/tomorrow plan."""
    console.print("[bold cyan]Reading calendar...[/bold cyan]")
    events = get_upcoming_events(days=2)

    console.print("[bold cyan]Reading recent Gmail...[/bold cyan]")
    emails = get_recent_emails(limit=10, days=10)

    console.print("[bold cyan]Extracting upcoming deadlines (read-only)...[/bold cyan]")
    deadlines, _ = validate_candidates(extract_candidates(emails))

    events_text = "\n".join(
        f"- {e['start']} to {e['end']}: {e['summary']} {('@ ' + e['location']) if e.get('location') else ''}"
        for e in events
    ) or "(none)"

    emails_text = "\n".join(
        f"- From: {e['from']} | Subject: {e['subject']} | {e['snippet']}"
        for e in emails
    ) or "(none)"

    deadlines_text = "\n".join(
        f"- {d.date}{(' ' + d.start_time) if d.start_time else ''}: {d.title} ({d.category})"
        for d in deadlines
    ) or "(none)"

    system_prompt = """
You are Jarvix, a private local personal assistant.

Rules:
- Do NOT invent dates, meetings, or deadlines. Use only the data provided.
- Do NOT claim you sent, created, or added anything.
- If information is missing, say so instead of guessing.
- Be concise and practical.

Produce a plan with these clearly separated sections:
Calendar:
Important emails:
Deadlines:
Suggested plan:
"""

    user_prompt = f"""
Confirmed calendar events (today/tomorrow):
{events_text}

Recent emails:
{emails_text}

Extracted upcoming deadlines:
{deadlines_text}

Create a useful, realistic plan for today and tomorrow.
"""

    console.print("[bold cyan]Thinking...[/bold cyan]")
    plan = ask_ollama(system_prompt, user_prompt)

    console.print("\n[bold green]Jarvix plan:[/bold green]\n")
    console.print(plan)


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
