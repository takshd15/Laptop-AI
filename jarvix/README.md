# Jarvix — Local Voice Laptop Operator (V2)

Jarvix is a **voice-first, local** assistant for your Windows laptop. Double-clap
to wake it; it greets you, speaks a daily briefing from your Google Calendar and
Gmail, opens your apps and project folder, controls music, and takes spoken
commands — routing them to safe actions. Email sends and calendar writes always
require explicit confirmation.

Everything runs **locally** (speech-to-text + the LLM via Ollama). The only
network calls are to Google APIs (Gmail/Calendar) you authorize.

> **Reality check:** Jarvix can't run while the laptop is shut down or asleep. It
> runs once you're logged in and the background process is started (see
> [Run on startup](#run-on-startup)). It needs the mic permission allowed.

---

## What it can do

- 🗣️ Spoken daily briefing (calendar + important email + short plan)
- 📂 Open allowlisted apps and folders (no arbitrary commands)
- 🎵 Music control via media keys (play/pause, next, prev, volume, mute) + `play "<song>"`
- 🎙️ Speech-to-text command mode (offline, `faster-whisper`)
- 🧭 Deterministic intent router (rule-based, fast, predictable)
- 👏 Double-clap wake trigger
- 🌅 Welcome routine (brief + open app/folder + music)
- 📨 Scan Gmail for events/deadlines → add to Calendar **after confirmation**
- ✉️ Draft emails, and send them **only after you confirm**

---

## Requirements

- Windows 10/11, a working microphone and speakers
- Python 3.12 + the bundled virtual env (`.venv`)
- [Ollama](https://ollama.com) running locally with the `llama3.2:3b` model
- A Google Cloud OAuth client (Desktop app) with Gmail + Calendar scopes

---

## Setup

```powershell
cd C:\Users\hp\VectorDB\jarvix
python -m venv .venv             # if .venv doesn't exist yet
.\.venv\Scripts\activate
pip install -r requirements.txt

# Local model
ollama serve                     # leave running
ollama pull llama3.2:3b

# Config
copy .env.example .env           # then edit values if needed
```

**Google credentials:** put your OAuth client file at
`secrets/google_credentials.json`. The token is created on first auth and saved
to `secrets/google_token.json`. Both are gitignored.

First run authorizes Google in your browser:

```powershell
python -m app.main reauth
```

Make sure your Google Cloud OAuth consent screen includes **all four** scopes:

```
gmail.readonly   gmail.send   calendar.readonly   calendar.events
```

---

## How to run

```powershell
cd C:\Users\hp\VectorDB\jarvix
.\.venv\Scripts\activate

python -m app.main wake          # full experience: clap clap -> welcome -> commands
```

`warmup` first makes LLM commands snappier (keeps the model resident ~25 min):

```powershell
python -m app.main warmup
```

### Command reference

| Command | What it does |
|---|---|
| `wake` | First double-clap runs the welcome routine, then clap to give commands |
| `welcome` | Run the welcome routine once (brief + open app/folder + music) |
| `brief` | Print **and speak** the daily briefing |
| `today` | Full LLM today/tomorrow plan |
| `today-fast` | Instant deterministic plan (no LLM) |
| `listen` | Record one spoken command and print the transcription |
| `route "<text>"` | Parse a text command and execute it (test the brain without voice) |
| `open-app <alias>` | Open an allowlisted app (e.g. `vscode`, `chrome`) |
| `open-folder <alias>` | Open an allowlisted folder (e.g. `jarvix`, `downloads`) |
| `music <action>` | `playpause` \| `next` \| `prev` \| `volume-up` \| `volume-down` \| `mute` |
| `play "<song>"` | Open Spotify, search the song, and start playback |
| `scan-mail` | Read Gmail → propose calendar events → **confirm** → write |
| `draft-email <to> "<msg>"` | Draft an email (does **not** send) |
| `send-email <to> "<msg>"` | Draft, read aloud, **confirm**, then send |
| `reauth` | Refresh Google OAuth (needed after scope changes) |

---

## The wake / welcome flow

```
double clap
  → speak briefing (calendar + email + short plan)
  → open AUTO_OPEN_APP_ON_WAKE        (e.g. vscode)
  → open AUTO_OPEN_FOLDER_ON_WAKE     (e.g. jarvix, in Explorer)
  → tap play/pause                    (resumes Spotify; does NOT pick a song)
  → listen for the next command (clap again to talk)
```

Music on wake just **resumes** whatever was last playing. To play a *specific*
song, use `play "<song>"` or say *"play …"*.

---

## Configuration (`.env`)

| Key | Meaning |
|---|---|
| `USER_DISPLAY_NAME` | Name in the greeting (e.g. `Mr Taksh`) |
| `JARVIX_GREETING` | Greeting prefix (e.g. `Welcome back`) |
| `STT_MODEL` | `tiny.en` (fastest) or `base.en` (more accurate) |
| `VOICE_RECORD_SECONDS` | Max seconds to record one command (also stops on silence) |
| `WAKE_MODE` | `clap` (reserved for future modes) |
| `AUTO_OPEN_APP_ON_WAKE` | App alias to open on wake (`""` to disable) |
| `AUTO_OPEN_FOLDER_ON_WAKE` | Folder alias to open on wake (`""` to disable) |
| `AUTO_START_MUSIC_ON_WAKE` | `true`/`false` — tap play/pause on wake |
| `OLLAMA_MODEL` / `OLLAMA_URL` | Local model + endpoint |
| `TIMEZONE` | Calendar timezone |

### Aliases & contacts (user-editable)

- **Apps/folders:** `app/memory/aliases.json` — add your own. Only listed
  aliases can be opened (allowlist; no arbitrary paths or shell).
- **Contacts:** `app/memory/contacts.json` (gitignored — holds real emails).
  Copy `contacts.example.json` to start. Unknown recipients prompt you to type
  the address, which is then saved.

---

## Safety model

| Action | Policy |
|---|---|
| Read calendar/Gmail, plan, speak | ✅ auto |
| Open allowlisted apps/folders | ✅ auto |
| Music play/pause/next/prev/volume | ✅ auto |
| Add calendar event | ⚠️ requires typed confirmation |
| Send email | ⚠️ requires typed `yes` (via `permissions.py`) |
| Arbitrary terminal commands | ⛔ blocked — not implemented |
| Delete/move files, delete/archive email | ⛔ not implemented |

Jarvix says "sent" only **after** the Gmail API confirms success.

---

## Voice tuning

- **Clap too sensitive / missed:** edit `threshold` (default `0.25`) in
  `app/voice/clap_detector.py`. Raise it if noise triggers it; lower if claps are
  missed. Widen `max_gap` (default `0.8s`) if your two claps land far apart.
- **Mishears speech:** set `STT_MODEL=base.en` in `.env` (slower, more accurate).
  The model downloads on first use.
- **No microphone / no audio device:** `listen`/`wake` fail gracefully with a
  message instead of crashing. TTS also falls back to printing.

---

## Run on startup

`start_jarvix.bat` activates the venv and runs `python -m app.runtime.startup`
(the wake loop). To launch it on login:

1. Press `Win + R`, type `shell:startup`, press Enter.
2. Put a shortcut to `start_jarvix.bat` in that folder.

On login Jarvix starts and **waits silently** for a double clap — it doesn't
greet until you clap, so it never spams you.

To disable: delete the shortcut from the Startup folder.

---

## Troubleshooting

- **`reauth` / 403 on send:** add `gmail.send` (and the other 3 scopes) in Google
  Cloud, then run `python -m app.main reauth`.
- **LLM slow:** run `python -m app.main warmup` first; use `today-fast` for an
  instant plan.
- **`open-app cursor` fails:** Cursor isn't installed — use `vscode`, or add the
  real exe path under `apps` in `aliases.json`.
- **Ollama errors:** ensure `ollama serve` is running and `llama3.2:3b` is pulled.

---

## Project layout

```
app/
  main.py            CLI commands (Typer)
  config.py          env-driven settings
  brain/             ollama_client, intent_router, command_parser
  voice/             tts, recorder, stt, clap_detector, wake_loop
  tools/             gmail, calendar, google_auth, desktop, music, email_actions
  safety/            permissions (confirmation gates)
  memory/            aliases.json, contacts.json, preferences.json
  runtime/           startup.py (login entry point)
```
