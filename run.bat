@echo off
setlocal
set TINY_DISABLE_CACHE=1
go run ./src %*
endlocal