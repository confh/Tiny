@echo off
setlocal
go run ./src %* --disable-cache
endlocal