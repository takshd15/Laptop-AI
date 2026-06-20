@echo off
REM Jarvix v2 - launched on login from the Windows Startup folder.
REM It starts the wake loop, which stays silent until a double clap.
cd /d C:\Users\hp\VectorDB\jarvix
call .venv\Scripts\activate
python -m app.runtime.startup
