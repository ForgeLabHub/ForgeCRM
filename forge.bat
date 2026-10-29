@echo off
setlocal EnableDelayedExpansion
chcp 65001 >nul 2>&1
title ForgeCRM — управление проектом

rem ForgeCRM — управление проектом для Windows.
rem Зеркало forge.sh: те же команды, нативная реализация без bash.
rem   forge.bat              меню
rem   forge.bat help         список команд
rem   forge.bat start        запуск в фоне + открыть браузер
rem   forge.bat status       что запущено

cd /d "%~dp0"
set "NAME=ForgeCRM"
set "PORT=8080"
set "FRONTEND_PORT=5173"
set "URL=http://localhost:8080/healthz"
set "ROOT=%CD%"
set "RUN=%ROOT%\.run"
if not exist "%RUN%" mkdir "%RUN%"
set "PY=.venv\Scripts\python.exe"
if not exist "%PY%" set "PY=python"

set "CMD=%~1"
if "%CMD%"=="" set "CMD=menu"
shift >nul 2>&1

if /i "%CMD%"=="help"   goto :help
if /i "%CMD%"=="-h"     goto :help
if /i "%CMD%"=="--help" goto :help
if /i "%CMD%"=="menu"   goto :menu
if /i "%CMD%"=="status" goto :status
if /i "%CMD%"=="start"  goto :start
if /i "%CMD%"=="stop"   goto :stop
if /i "%CMD%"=="restart" goto :restart
if /i "%CMD%"=="doctor" goto :doctor
if /i "%CMD%"=="install" goto :install
if /i "%CMD%"=="setup"  goto :install
if /i "%CMD%"=="test"   goto :test
if /i "%CMD%"=="lint"   goto :lint
if /i "%CMD%"=="build"  goto :build
if /i "%CMD%"=="verify" goto :verify
if /i "%CMD%"=="logs"   goto :logs
if /i "%CMD%"=="open"   goto :open
if /i "%CMD%"=="db"     goto :db
if /i "%CMD%"=="docker" goto :docker
if /i "%CMD%"=="clean"  goto :clean
if /i "%CMD%"=="version" goto :version

echo Неизвестная команда: %CMD%
goto :help

:help
echo.
echo   ForgeCRM — управление проектом
echo.
echo   forge.bat [команда]
echo.
echo   status    что запущено              test     тесты
echo   start     запустить                lint     статический анализ
echo   stop      остановить               build    собрать
echo   restart   перезапустить            verify   lint+test+docs
echo   logs      логи (logs имя)          doctor   проверка окружения
echo   open      открыть в браузере       install  зависимости
echo   migrate^|backup^|restore  база
echo   db backup^|restore  база            clean    убрать артефакты
echo   docker up^|down^|logs              version  версия
echo.
exit /b 0

:menu
cls
echo.
echo   ForgeCRM
echo   ----------------------------------------
echo    1^) status          7^) build
echo    2^) start           8^) verify
echo    3^) stop            9^) docker up
echo    4^) restart        10^) docker down
echo    5^) logs           11^) db migrate
echo    6^) test           12^) doctor
echo   13^) backup базы        14^) migrate
echo    0^) выход
echo.
set /p "CH=выбор: "
if "%CH%"=="1" goto :status
if "%CH%"=="2" goto :start
if "%CH%"=="3" goto :stop
if "%CH%"=="4" goto :restart
if "%CH%"=="5" goto :logs
if "%CH%"=="6" goto :test
if "%CH%"=="7" goto :build
if "%CH%"=="8" goto :verify
if "%CH%"=="9" ( call "%~f0" docker up & goto :menu )
if "%CH%"=="10" ( call "%~f0" docker down & goto :menu )
if "%CH%"=="11" ( call "%~f0" db migrate & goto :menu )
if "%CH%"=="12" goto :doctor
if "%CH%"=="13" ( call "%~f0" db backup & goto :menu )
if "%CH%"=="14" ( call "%~f0" db migrate & goto :menu )
if "%CH%"=="0" exit /b 0
goto :menu

:doctor
echo.
echo   Окружение
where python >nul 2>&1 && ( echo   ok    python найден ) || echo   fail  python не найден
if exist "%PY%" ( echo   ok    %PY% ) else ( echo   warn  .venv нет — forge.bat install )
where go >nul 2>&1 && ( echo   ok    go найден ) || echo   fail  go не найден
if exist .env ( echo   ok    .env на месте ) else ( echo   warn  .env нет — см. .env.example )
if exist .git (
  git ls-files --error-unmatch .env >nul 2>&1 && ( echo   fail  .env отслеживается git ) || ( echo   ok    .env не отслеживается git )
)
netstat -ano | findstr /r /c:":%PORT% .*LISTENING" >nul 2>&1 && ( echo   warn  порт %PORT% занят ) || ( echo   ok    порт %PORT% свободен )
exit /b 0

:status
echo.
echo   ForgeCRM
if exist "%RUN%\*.pid" (
  for %%f in ("%RUN%\*.pid") do (
    set "P=%%~nf"
    set /p PID=<"%%f"
    tasklist /FI "PID eq !PID!" 2>nul | findstr /i "!PID!" >nul && echo   ok    !P! запущен ^(pid !PID!^) || echo   warn  !P! не запущен
  )
) else (
  echo   warn  ничего не запущено — forge.bat start
)
netstat -ano | findstr /r /c:":%PORT% .*LISTENING" >nul 2>&1 && echo   ok    порт %PORT% слушается || echo   warn  порт %PORT% свободен
exit /b 0

:start
call :start_services
timeout /t 2 /nobreak >nul
start "" "%URL%"
goto :status

:start_services
echo   Запуск API: cd backend ^&^& go run ./cmd/forgecrm-api
start "api" cmd /c "cd backend && go run ./cmd/forgecrm-api"
echo   ok    api запущен ^(порт %PORT%^)
if exist frontend\node_modules (
  start "web" cmd /c "cd frontend && npm run dev"
  echo   ok    web запущен ^(порт %FRONTEND_PORT%^)
)
exit /b 0

:stop
if exist "%RUN%\*.pid" (
  for %%f in ("%RUN%\*.pid") do (
    set "P=%%~nf"
    set /p PID=<"%%f"
    taskkill /PID !PID! /T /F >nul 2>&1 && echo   ok    !P! остановлен || echo   warn  !P! уже не работал
    del /q "%%f" >nul 2>&1
  )
)
echo   ok    остановлено
exit /b 0

:restart
call :stop
call :start_services
goto :status

:logs
set "L=%~1"
if "%L%"=="" (
  for %%f in ("%RUN%\*.log") do ( echo. & echo   --- %%~nxf & powershell -NoProfile -Command "Get-Content -Tail 30 '%%f'" )
) else (
  powershell -NoProfile -Command "Get-Content -Tail 50 -Wait '%RUN%\%L%.log'"
)
exit /b 0

:open
start "" "%URL%"
exit /b 0

:install
make setup
exit /b 0

:test
make test-unit
exit /b 0

:lint
make fmt-check make vet make lint
exit /b 0

:build
make build
exit /b 0

:verify
call :lint
call :test
make docs-check
exit /b 0

:db
if /i "%~1"=="backup" make backup
if /i "%~1"=="restore" make restore FILE=%~2
if /i "%~1"=="migrate" make migrate
if not defined CMD echo db migrate^|backup^|restore
exit /b 0

:docker
exit /b 0

:clean
if exist "%RUN%" rmdir /s /q "%RUN%"
if exist frontend\dist rmdir /s /q frontend\dist
echo   ok    артефакты и логи удалены ^(.venv и node_modules сохранены^)
exit /b 0

:version
echo   ForgeCRM
if exist .git for /f %%i in ('git rev-parse --short HEAD 2^>nul') do echo   коммит: %%i
echo   скрипт: forge.bat v1.0
exit /b 0
