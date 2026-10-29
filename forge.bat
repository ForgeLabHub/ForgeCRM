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
set "FILE_VERSION=1.1.0"

rem Порт и адреса: переменная окружения важнее .env, .env важнее значений ниже.
set "PORT_DEFAULT=8080"
set "FRONTEND_PORT_DEFAULT=5173"
set "ROOT=%CD%"
set "RUN=%ROOT%\.run"
if not exist "%RUN%" mkdir "%RUN%"
set "PY=.venv\Scripts\python.exe"
if not exist "%PY%" set "PY=python"
set "ENVFILE=%ROOT%\.env"
if defined FORGE_ENV_FILE set "ENVFILE=%FORGE_ENV_FILE%"
set "QUIET=0"
set "JSON=0"
set "ENV_LOADED=0"
set "ENV_SKIPPED=0"
set "PADSTR=                                        "

rem --- глобальные флаги: forge.bat [-q] [--no-color] [--json] команда [аргументы]
set "CMD="
:args
if "%~1"=="" goto :args_done
set "A=%~1"
if /i "!A!"=="-q"         ( set "QUIET=1" & shift & goto :args )
if /i "!A!"=="--quiet"    ( set "QUIET=1" & shift & goto :args )
if /i "!A!"=="--no-color" ( shift & goto :args )
if /i "!A!"=="--json"     ( set "JSON=1" & shift & goto :args )
if /i "!A!"=="-h"         goto :help
if /i "!A!"=="--help"     goto :help
if /i "!A!"=="-V"         ( call :load_env & goto :version )
if /i "!A!"=="--version"  ( call :load_env & goto :version )
if defined CMD goto :args_done
set "CMD=!A!"
shift
goto :args
:args_done
if not defined CMD set "CMD=menu"

rem .env читается до диспетчера: он влияет на порт, адреса и на все команды ниже.
call :load_env
if not defined PORT set "PORT=%PORT_DEFAULT%"
if not defined FRONTEND_PORT set "FRONTEND_PORT=%FRONTEND_PORT_DEFAULT%"
if not defined URL set "URL=http://localhost:%PORT%/healthz"
if not defined FRONTEND_URL set "FRONTEND_URL=http://localhost:%PORT%"

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
if /i "%CMD%"=="env"     goto :env
if /i "%CMD%"=="config"  goto :env
if /i "%CMD%"=="completion" goto :completion
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
echo   env show^|init^|set^|check^|edit^|path^|reload   работа с .env
echo.
echo   Флаги: -q/--quiet, --no-color, --json (для status), -h, -V
echo   .env: %ENVFILE%  (переопределить: set FORGE_ENV_FILE=...)
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
echo   15^) env show            16^) env check
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
if "%CH%"=="15" ( call "%~f0" env show & goto :menu )
if "%CH%"=="16" ( call "%~f0" env check & goto :menu )
if "%CH%"=="0" exit /b 0
goto :menu

:doctor
echo.
echo   Окружение
where python >nul 2>&1 && ( echo   ok    python найден ) || echo   fail  python не найден
if exist "%PY%" ( echo   ok    %PY% ) else ( echo   warn  .venv нет — forge.bat install )
where go >nul 2>&1 && ( echo   ok    go найден ) || echo   fail  go не найден
if exist "%ENVFILE%" (
  echo   ok    .env: загружено !ENV_LOADED! переменных ^(не перетёрто: !ENV_SKIPPED!^)
) else (
  echo   warn  .env нет — forge.bat env init ^(или см. .env.example^)
)
if exist .git (
  git ls-files --error-unmatch .env >nul 2>&1 && ( echo   fail  .env отслеживается git ) || ( echo   ok    .env не отслеживается git )
)
netstat -ano | findstr /r /c:":%PORT% .*LISTENING" >nul 2>&1 && ( echo   warn  порт %PORT% занят ) || ( echo   ok    порт %PORT% свободен )
exit /b 0

:status
if "%JSON%"=="1" goto :status_json
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

:status_json
set "API=0"
set "WEB=0"
if exist "%RUN%\*.pid" (
  for %%f in ("%RUN%\*.pid") do (
    set /p PID=<"%%f"
    tasklist /FI "PID eq !PID!" 2>nul | findstr /i "!PID!" >nul && (
      if /i "%%~nf"=="web" ( set "WEB=1" ) else ( set "API=1" )
    )
  )
)
if not defined PORT set "PORT=0"
call :pad "0" 0
echo {"name":"%NAME%","port":%PORT%,"url":"%URL%","api_running":%API%,"web_running":%WEB%}
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
call :hermetic
make test-unit
exit /b 0

:lint
call :hermetic
make fmt-check make vet make lint
exit /b 0

:build
call :hermetic
make build
exit /b 0

:verify
call :hermetic
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

:load_env
rem Читает .env в окружение процесса скрипта. Правила те же, что в forge.sh:
rem комментарии #, пустые строки, префикс export, кавычки, пробелы вокруг =.
rem Уже заданная переменная окружения НЕ перетирается — поэтому
rem `set PORT=9000 && forge.bat start` работает независимо от .env.
rem Ограничение cmd.exe: значения с символами % и ! не поддерживаются.
set "ENV_LOADED=0"
set "ENV_SKIPPED=0"
if not exist "%ENVFILE%" exit /b 0
for /f "usebackq eol=# tokens=1,* delims==" %%A in ("%ENVFILE%") do call :env_line "%%A" "%%B"
exit /b 0

:env_line
set "K=%~1"
set "V=%~2"
if /i "!K:~0,7!"=="export " set "K=!K:~7!"
set "K=!K: =!"
if "!K!"=="" exit /b 0
echo !K!| findstr /r /c:"^[A-Za-z_][A-Za-z0-9_]*$" >nul || exit /b 0
echo ;PATH;IFS;PATHEXT;COMSPEC;SYSTEMROOT;WINDIR;TEMP;TMP;USERPROFILE;PROMPT;CD;| findstr /i /c:";!K!;" >nul && exit /b 0
call :trimv
rem инлайн-комментарий: KEY=value # пояснение — как в forge.sh
echo !V!| findstr /c:" #" >nul && for /f "tokens=1 delims=#" %%x in ("!V!") do set "V=%%x"
call :trimv
if defined %K% (
  set /a ENV_SKIPPED+=1
  set "ENV_SRC_%K%=окружение"
) else (
  set "%K%=!V!"
  set "ENV_KEYS=!ENV_KEYS! !K!"
  set /a ENV_LOADED+=1
  set "ENV_SRC_%K%=файл"
)
exit /b 0

:trimv
rem убирает кавычки и пробелы вокруг значения
if not defined V exit /b 0
set "V=!V:"=!"
:trimv_lead
if "!V:~0,1!"==" " ( set "V=!V:~1!" & goto :trimv_lead )
if "!V:~-1!"==" " ( set "V=!V:~0,-1!" & goto :trimv_lead )
exit /b 0

:strlen
rem strlen <строка> -> LEN
set "S=%~1"
set "LEN=0"
:strlen_loop
if "!S!"=="" exit /b 0
set "S=!S:~1!"
set /a LEN+=1
goto :strlen_loop

:pad
rem pad <строка> <ширина> -> PADDED
call :strlen "%~1"
set /a N=%~2 - LEN
if !N! leq 0 ( set "PADDED=%~1" & exit /b 0 )
set "PADDED=%~1!PADSTR:~0,%N%!"
exit /b 0

:is_secret
rem is_secret <имя> -> SECRET=1|0
echo %~1| findstr /i /r "_MINUTES$ _SECONDS$ _HOURS$ _DAYS$ _TTL$ _SIZE$ _COUNT$ _LIMIT$ _PORT$ _URL$ _URI$ _HOST$" >nul && ( set "SECRET=0" & exit /b 0 )
echo %~1| findstr /i /r "SECRET TOKEN PASSWORD PASSWD CREDENTIAL PRIVATE API_KEY APIKEY ADMIN_CODE DSN" >nul && ( set "SECRET=1" & exit /b 0 )
set "SECRET=0"
exit /b 0

:is_placeholder
rem is_placeholder <значение> -> PLACEHOLDER=1|0
set "PH=0"
if "%~1"=="" set "PH=1"
echo %~1| findstr /i /r "change-me changeme change_me example your- your_ xxx placeholder admin123 demo123 todo замени ЗАМЕНИ сгенерируй СГЕНЕРИРУЙ поменяй ПОМЕНЯЙ измени ИЗМЕНИ впиши ВПИШИ укажи УКАЖИ пароль ПАРОЛЬ секрет СЕКРЕТ" >nul && set "PH=1"
set "PLACEHOLDER=!PH!"
exit /b 0

:mask
rem mask <имя> <значение> -> MV
set "MV=%~2"
if "!MV!"=="" ( set "MV=(пусто)" & exit /b 0 )
call :is_secret "%~1"
if "!SECRET!"=="0" exit /b 0
call :strlen "!MV!"
if !LEN! leq 6 ( set "MV=****" & exit /b 0 )
set "HEAD=!MV:~0,2!"
set "TAIL=!MV:~-2!"
set "MV=!HEAD!…!TAIL! (!LEN! симв.)"
exit /b 0

:env
set "SUB=%~1"
if not defined SUB set "SUB=show"
if /i "%SUB%"=="show"   goto :env_show
if /i "%SUB%"=="list"   goto :env_show
if /i "%SUB%"=="path"   goto :env_path
if /i "%SUB%"=="check"  goto :env_check
if /i "%SUB%"=="init"   ( call :env_init "%~2" & exit /b 0 )
if /i "%SUB%"=="set"    ( call :env_set "%~2" & exit /b 0 )
if /i "%SUB%"=="edit"   goto :env_edit
if /i "%SUB%"=="reload" goto :env_reload
echo   env show ^| init [--force] ^| set KEY=VALUE ^| check ^| edit ^| path ^| reload
exit /b 0

:env_path
echo %ENVFILE%
exit /b 0

:hermetic
rem Тесты, линт и сборка не должны зависеть от боевого .env — как в forge.sh.
if defined ENV_KEYS for %%k in (!ENV_KEYS!) do set "%%k="
exit /b 0

:env_show
if not exist "%ENVFILE%" (
  echo   warn  файла %ENVFILE% нет
  echo         создать из примера: forge.bat env init
  exit /b 1
)
echo.
echo   Переменные: %ENVFILE%
echo   NAME                         SOURCE      VALUE
for /f "usebackq eol=# tokens=1,* delims==" %%A in ("%ENVFILE%") do call :env_show_line "%%A" "%%B"
if "%QUIET%"=="0" echo.
if "%QUIET%"=="0" echo   загружено %ENV_LOADED% · не перетёрто %ENV_SKIPPED%
exit /b 0

:env_show_line
set "K=%~1"
set "V=%~2"
if /i "!K:~0,7!"=="export " set "K=!K:~7!"
set "K=!K: =!"
if "!K!"=="" exit /b 0
echo !K!| findstr /r /c:"^[A-Za-z_][A-Za-z0-9_]*$" >nul || exit /b 0
call :trimv
call :mask "!K!" "!V!"
set "SRC=файл"
if defined ENV_SRC_%K% set "SRC=!ENV_SRC_%K%!"
call :pad "!K!" 28
set "KP=!PADDED!"
call :pad "!SRC!" 11
echo   !KP! !PADDED! !MV!
exit /b 0

:env_check
if not exist "%ENVFILE%" (
  echo   fail  %ENVFILE% нет — forge.bat env init
  exit /b 1
)
set "EXAMPLE="
for %%e in (.env.example env.example .env.sample) do if exist %%e if not defined EXAMPLE set "EXAMPLE=%%e"
set "FAILS=0"
echo.
echo   Сверка %ENVFILE%
if defined EXAMPLE (
  set "MISSING="
  for /f "usebackq eol=# tokens=1 delims== " %%K in ("%EXAMPLE%") do call :check_present "%%K"
  if not defined MISSING ( echo   ok    все переменные из !EXAMPLE! на месте ) else ( echo   warn  нет в .env: !MISSING! )
)
set "PHKEYS="
for /f "usebackq eol=# tokens=1,* delims==" %%A in ("%ENVFILE%") do call :check_line "%%A" "%%B"
if defined PHKEYS (
  echo   fail  остались значения из примера:!PHKEYS!
  set /a FAILS+=1
)
if exist .gitignore (
  findstr /x /c:".env" .gitignore >nul 2>&1 && ( echo   ok    .env указан в .gitignore ) || ( echo   warn  .env не указан в .gitignore )
)
if "%FAILS%"=="0" ( echo   ok    конфигурация готова к запуску & exit /b 0 )
echo   fail  замечаний: %FAILS%
exit /b 1

:check_present
set "CK=%~1"
if /i "!CK:~0,7!"=="export " set "CK=!CK:~7!"
set "CK=!CK: =!"
if "!CK!"=="" exit /b 0
echo !CK!| findstr /r /c:"^[A-Za-z_][A-Za-z0-9_]*$" >nul || exit /b 0
findstr /r /i /c:"^!CK!=" "%ENVFILE%" >nul 2>&1 || set "MISSING=!MISSING! !CK!"
exit /b 0

:check_line
set "CK=%~1"
set "CV=%~2"
if /i "!CK:~0,7!"=="export " set "CK=!CK:~7!"
set "CK=!CK: =!"
if "!CK!"=="" exit /b 0
echo !CK!| findstr /r /c:"^[A-Za-z_][A-Za-z0-9_]*$" >nul || exit /b 0
call :is_secret "!CK!"
if "!SECRET!"=="0" exit /b 0
set "V=!CV!"
call :trimv
if not defined V (
  echo   fail  !CK! пуст
  set /a FAILS+=1
  exit /b 0
)
call :is_placeholder "!V!"
if "!PLACEHOLDER!"=="1" set "PHKEYS=!PHKEYS! !CK!"
exit /b 0

:env_init
set "FORCE=0"
if /i "%~1"=="--force" set "FORCE=1"
if exist "%ENVFILE%" if "%FORCE%"=="0" (
  echo   fail  %ENVFILE% уже существует
  echo         перезаписать: forge.bat env init --force ^(прежний файл останется в .env.bak^)
  exit /b 1
)
set "EXAMPLE="
for %%e in (.env.example env.example .env.sample) do if exist %%e if not defined EXAMPLE set "EXAMPLE=%%e"
if not defined EXAMPLE (
  echo   fail  нет .env.example — нечего копировать
  exit /b 1
)
if exist "%ENVFILE%" copy /y "%ENVFILE%" "%ENVFILE%.bak" >nul && echo   ok    прежний файл сохранён: .env.bak
copy /y "%EXAMPLE%" "%ENVFILE%" >nul || ( echo   fail  не удалось создать %ENVFILE% & exit /b 1 )
echo   ok    создан из %EXAMPLE%
rem Заглушки секретов заменяются случайными значениями: файл сразу рабочий.
powershell -NoProfile -Command ^
  "$p=$env:ENVFILE; $chars='ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'.ToCharArray(); $rx='^(change[-_]?me|changeme|example|your[-_]|xxx|placeholder|admin123|demo123|<|todo|Замени|ЗАМЕНИ|Сгенерируй|СГЕНЕРИРУЙ|Поменяй|ПОМЕНЯЙ|Измени|ИЗМЕНИ|Впиши|ВПИШИ|Укажи|УКАЖИ|Пароль|ПАРОЛЬ|Секрет|СЕКРЕТ)'; $out=@(); $changed=@(); foreach($l in (Get-Content -LiteralPath $p)) { if ($l -match '^\s*(export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$') { $k=$matches[2]; $v=$matches[3].Trim().Trim([char]34,[char]39); if ($k -match '(SECRET|TOKEN|PASSWORD|PASSWD|CREDENTIAL|PRIVATE|API_?KEY|ADMIN_CODE|DSN)' -and ($v -eq '' -or $v -match $rx)) { $s=-join (1..32 | ForEach-Object { $chars | Get-Random }); $out += ($k + '=' + $s); $changed += $k } else { $out += $l } } else { $out += $l } }; [System.IO.File]::WriteAllLines($p, $out, (New-Object System.Text.UTF8Encoding $false)); if ($changed.Count -gt 0) { Write-Host ('  ok    сгенерированы случайные значения: ' + ($changed -join ' ')) } else { Write-Host '  warn  секретных значений для генерации не нашлось — проверьте файл вручную' }"
call :load_env
echo   дальше: forge.bat env check ^&^& forge.bat doctor
exit /b 0

:env_set
set "KV=%~1"
if not defined KV (
  echo   fail  формат: forge.bat env set KEY=VALUE
  exit /b 2
)
for /f "tokens=1,* delims==" %%a in ("!KV!") do ( set "ENVK=%%a" & set "ENVV=%%b" )
set "ENVK=!ENVK: =!"
echo !ENVK!| findstr /r /c:"^[A-Za-z_][A-Za-z0-9_]*$" >nul || ( echo   fail  недопустимое имя переменной: !ENVK! & exit /b 2 )
if not exist "%ENVFILE%" type nul > "%ENVFILE%"
copy /y "%ENVFILE%" "%ENVFILE%.bak" >nul 2>&1
powershell -NoProfile -Command ^
  "$p=$env:ENVFILE; $k=$env:ENVK; $v=$env:ENVV; $done=$false; $out=@(); foreach($l in (Get-Content -LiteralPath $p)) { if (-not $done -and $l -match ('^\s*(export\s+)?' + [regex]::Escape($k) + '\s*=')) { $out += ($k + '=' + $v); $done=$true } else { $out += $l } }; if (-not $done) { $out += ($k + '=' + $v) }; [System.IO.File]::WriteAllLines($p, $out, (New-Object System.Text.UTF8Encoding $false))"
call :mask "!ENVK!" "!ENVV!"
echo   ok    записано: !ENVK!=!MV!
echo   применить: forge.bat restart
exit /b 0

:env_edit
if not exist "%ENVFILE%" (
  echo   warn  файла нет — создаю из примера
  call :env_init || exit /b 1
)
if defined EDITOR ( start "" "%EDITOR%" "%ENVFILE%" ) else ( notepad "%ENVFILE%" )
echo   ok    сохранено
echo   проверить: forge.bat env check · применить: forge.bat restart
exit /b 0

:env_reload
call :load_env
echo   ok    перечитано: загружено %ENV_LOADED%, пропущено %ENV_SKIPPED%
exit /b 0

:completion
echo   Автодополнение сделано для bash/zsh ^(forge.sh completion^).
echo   В cmd.exe дополнение файлов и так встроено: наберите forge.bat en^<Tab^>.
exit /b 0

:version
echo   ForgeCRM
if exist .git for /f %%i in ('git rev-parse --short HEAD 2^>nul') do echo   коммит: %%i
if exist "%ENVFILE%" ( echo   конфигурация: %ENVFILE% ^(!ENV_LOADED! переменных^) ) else ( echo   конфигурация: %ENVFILE% ^(файла нет^) )
echo   скрипт: forge.bat v%FILE_VERSION%
exit /b 0
