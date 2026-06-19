import typer
from rich.console import Console

from app.brain.ollama_client import ask_ollama
from app.tools.gmail import get_recent_emails
from app.tools.calendar import get_upcoming_events

app = typer.Typer()
console = Console()


@app.command()
def test_brain():
    """Test local Ollama brain."""
    answer = ask_ollama(
        system_prompt="You are Jarvix, a private local personal assistant.",
        user_prompt="Say hello in one sentence and explain what you are.",
    )
    console.print(answer)


@app.command()
def today():
    """Read Gmail + Calendar and make a daily plan."""
    console.print("[bold cyan]Reading calendar...[/bold cyan]")
    events = get_upcoming_events(days=2)

    console.print("[bold cyan]Reading recent Gmail...[/bold cyan]")
    emails = get_recent_emails(limit=10)

    events_text = "\n".join(
        [
            f"- {event['start']} to {event['end']}: {event['summary']} at {event.get('location', '')}"
            for event in events
        ]
    )

    emails_text = "\n".join(
        [
            f"- From: {email['from']}\n  Subject: {email['subject']}\n  Snippet: {email['snippet']}"
            for email in emails
        ]
    )

    system_prompt = """
You are Jarvix, a private local personal assistant.

Your job:
- summarize the user's day
- identify important emails
- suggest a realistic plan
- be concise but useful
- do not invent meetings
- do not claim you sent or created anything
- clearly separate calendar, email, and suggested plan
"""

    user_prompt = f"""
Calendar events:
{events_text}

Recent emails:
{emails_text}

Create a useful plan for today and tomorrow.
"""

    console.print("[bold cyan]Thinking...[/bold cyan]")
    plan = ask_ollama(system_prompt, user_prompt)

    console.print("\n[bold green]Jarvix plan:[/bold green]\n")
    console.print(plan)


if __name__ == "__main__":
    app()
