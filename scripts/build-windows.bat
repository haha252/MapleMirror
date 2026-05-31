@echo off
setlocal
set "ROOT=%~dp0.."
pushd "%ROOT%"
if errorlevel 1 goto :path_failed
chcp 65001 >nul

set "GOCACHE=%CD%\.cache\go-build"
set "OUT=dist\windows-amd64"
if not exist "%OUT%" mkdir "%OUT%"

powershell -NoProfile -ExecutionPolicy Bypass -File "scripts\check-file-lines.ps1"
if errorlevel 1 goto :failed

go test ./...
if errorlevel 1 goto :failed

set "REV=unknown"
for /f %%i in ('git rev-parse --short HEAD 2^>NUL') do set "REV=%%i"
set "VERSION=dev-%REV%"
set "GOOS=windows"
set "GOARCH=amd64"
set "CGO_ENABLED=0"

go build -trimpath -ldflags "-X main.version=%VERSION%" -o "%OUT%\mirror-master.exe" .\cmd\master
if errorlevel 1 goto :failed
go build -trimpath -ldflags "-X main.version=%VERSION%" -o "%OUT%\mirror-node.exe" .\cmd\node
if errorlevel 1 goto :failed

if exist "%OUT%\configs" rmdir /s /q "%OUT%\configs"
xcopy /e /i /y "configs" "%OUT%\configs" >nul
if exist "%OUT%\web" rmdir /s /q "%OUT%\web"
xcopy /e /i /y "web" "%OUT%\web" >nul
powershell -NoProfile -Command "$m=ConvertFrom-Json '\"Windows amd64 \u6784\u5efa\u5b8c\u6210\uff1a\"'; Write-Host ($m + '%OUT%')"
if errorlevel 1 goto :failed
popd
exit /b 0

:failed
powershell -NoProfile -Command "$m=ConvertFrom-Json '\"Windows amd64 \u6784\u5efa\u5931\u8d25\uff0c\u8bf7\u68c0\u67e5\u4e0a\u65b9\u4e2d\u6587\u9519\u8bef\u4fe1\u606f\u3002\"'; Write-Host $m"
popd
exit /b 1

:path_failed
powershell -NoProfile -Command "$m=ConvertFrom-Json '\"\u65e0\u6cd5\u8fdb\u5165\u9879\u76ee\u6839\u76ee\u5f55\uff0c\u8bf7\u68c0\u67e5\u811a\u672c\u4f4d\u7f6e\u3002\"'; Write-Host $m"
exit /b 1
