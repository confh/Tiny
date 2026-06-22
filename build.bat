@echo off
setlocal

if "%1"=="--compiler" goto BUILD_COMPILER

echo Building Tiny Windows runtime...
set GOOS=windows
set GOARCH=amd64
go build -ldflags "-s -w" -o src\embedded\tiny_runtime_windows_amd64.exe .\src\cmd\tiny_runtime
if errorlevel 1 exit /b 1

echo Building Tiny Linux AMD64 runtime...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=amd64
go build -ldflags "-s -w" -o src\embedded\tiny_runtime_linux_amd64 .\src\cmd\tiny_runtime
if errorlevel 1 exit /b 1

echo Building Tiny Linux ARM64 runtime...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=arm64
go build -ldflags "-s -w" -o src\embedded\tiny_runtime_linux_arm64 .\src\cmd\tiny_runtime
if errorlevel 1 exit /b 1

echo Building Tiny Darwin runtime...
set CGO_ENABLED=0
set GOOS=darwin
set GOARCH=arm64
go build -ldflags "-s -w" -o src\embedded\tiny_runtime_darwin_arm64 .\src\cmd\tiny_runtime
if errorlevel 1 exit /b 1

:BUILD_COMPILER
echo Building Tiny compiler...
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -ldflags "-s -w" -o tiny.exe .\src
if errorlevel 1 exit /b 1

echo Done.
endlocal