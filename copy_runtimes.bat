@echo off
setlocal

set "RUNTIMES_DIR=%USERPROFILE%\.tiny\runtimes"
if not exist "%RUNTIMES_DIR%" mkdir "%RUNTIMES_DIR%"

echo Copying runtimes to %RUNTIMES_DIR%...

if exist "src\embedded\tiny_runtime_windows_amd64.exe" (
    copy /y "src\embedded\tiny_runtime_windows_amd64.exe" "%RUNTIMES_DIR%\tiny_runtime_windows_amd64.exe"
)
if exist "src\embedded\tiny_runtime_linux_amd64" (
    copy /y "src\embedded\tiny_runtime_linux_amd64" "%RUNTIMES_DIR%\tiny_runtime_linux_amd64"
)
if exist "src\embedded\tiny_runtime_linux_arm64" (
    copy /y "src\embedded\tiny_runtime_linux_arm64" "%RUNTIMES_DIR%\tiny_runtime_linux_arm64"
)
if exist "src\embedded\tiny_runtime_darwin_arm64" (
    copy /y "src\embedded\tiny_runtime_darwin_arm64" "%RUNTIMES_DIR%\tiny_runtime_darwin_arm64"
)

echo Done.
endlocal
