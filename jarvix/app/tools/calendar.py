from datetime import datetime, timedelta, timezone
from app.tools.google_auth import calendar_service


def get_upcoming_events(days: int = 2, limit: int = 20) -> list[dict]:
    service = calendar_service()

    now = datetime.now(timezone.utc)
    end = now + timedelta(days=days)

    events_result = (
        service.events()
        .list(
            calendarId="primary",
            timeMin=now.isoformat(),
            timeMax=end.isoformat(),
            maxResults=limit,
            singleEvents=True,
            orderBy="startTime",
        )
        .execute()
    )

    events = []
    for event in events_result.get("items", []):
        start = event["start"].get("dateTime", event["start"].get("date"))
        end_time = event["end"].get("dateTime", event["end"].get("date"))

        events.append(
            {
                "id": event.get("id"),
                "summary": event.get("summary", "Untitled"),
                "start": start,
                "end": end_time,
                "location": event.get("location", ""),
            }
        )

    return events


def create_calendar_event(summary: str, start_iso: str, end_iso: str, description: str = "", location: str = ""):
    service = calendar_service()

    event = {
        "summary": summary,
        "location": location,
        "description": description,
        "start": {"dateTime": start_iso},
        "end": {"dateTime": end_iso},
    }

    created = service.events().insert(calendarId="primary", body=event).execute()
    return created
