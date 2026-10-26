@echo off
cd /d "%~dp0"
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0start.ps1" %*
set "status=%ERRORLEVEL%"
echo.
if not "%status%"=="0" echo Startup failed. Check the error messages above.
pause
exit /b %status%