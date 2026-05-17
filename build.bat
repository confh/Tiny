@echo off
setlocal

echo Building Tiny Windows runtime...
set GOOS=windows
set GOARCH=amd64
go build -ldflags "-s -w" -o src\embedded\tiny_runtime_windows_amd64.exe .\src\cmd\tiny_runtime
if errorlevel 1 exit /b 1

echo Building Tiny Linux runtime...
set GOOS=linux
set GOARCH=amd64
go build -ldflags "-s -w" -o src\embedded\tiny_runtime_linux_amd64 .\src\cmd\tiny_runtime
if errorlevel 1 exit /b 1

echo Building Tiny compiler...
set GOOS=windows
set GOARCH=amd64
go build -ldflags "-s -w" -o tiny.exe .\src
if errorlevel 1 exit /b 1

echo Done.
endlocal