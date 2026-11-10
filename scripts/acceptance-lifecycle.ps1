param(
    [string]$DeploymentRoot = (Join-Path $PSScriptRoot '..\.local'),
    [string]$Endpoint = 'http://121.199.175.151:25100/mcp/workbench',
    [switch]$UseCredentials,
    [string]$Token,
    [string]$AdminToken,
    [string]$CAFile,
    [string]$Remote = 'admin@121.199.175.151',
    [string]$RemoteDirectory = '/home/admin/loom-oi-beta',
    [ValidateSet('linux', 'windows')][string[]]$Platforms = @('linux', 'windows'),
    [switch]$SkipRestart
)
# Run after the ten-minute command acceptance has finished. The default run
# restarts each selected OIC and deliberately invalidates its existing sessions.
# Business assertions always enter through MCP; SSH is only deployment/CLI.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
Import-Module (Join-Path $PSScriptRoot 'McpClient.psm1') -Force
$DeploymentRoot = [IO.Path]::GetFullPath($DeploymentRoot)
if ($RemoteDirectory -notmatch '^/home/[A-Za-z0-9_-]+/[A-Za-z0-9_-]+$') { throw 'Use the dedicated beta deployment directory.' }
$secrets = Get-LoomCredentials $DeploymentRoot -UseCredentials:$UseCredentials
if ($PSBoundParameters.ContainsKey('Token')) { $secrets.WORKBENCH_TOKEN = $Token }
if ($PSBoundParameters.ContainsKey('AdminToken')) { $secrets.OIS_ADMIN_TOKEN = $AdminToken }
$base = $Endpoint.Substring(0, $Endpoint.IndexOf('/mcp/'))
$reports = Join-Path $DeploymentRoot 'reports'
New-Item -ItemType Directory -Force -Path $reports | Out-Null
$report = Join-Path $reports "lifecycle-$([DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss')).jsonl"
$runId = [Guid]::NewGuid().ToString('N').Substring(0, 10)
$sshOptions = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$expectedTools = @('linux_exec_command', 'linux_write_stdin', 'windows_exec_command', 'windows_write_stdin')
$createdTools = [Collections.Generic.List[string]]::new()
$loadedFixtures = [Collections.Generic.List[hashtable]]::new()
$restartRecovery = [Collections.Generic.List[string]]::new()
$connection = $null
$originalVM = $null

function Record([string]$Case, [string]$Step, $Evidence) {
    @{ timestamp = [DateTime]::UtcNow.ToString('o'); case = $Case; step = $Step; evidence = $Evidence } |
        ConvertTo-Json -Depth 64 -Compress | Add-Content -LiteralPath $report -Encoding utf8NoBOM
}
function Assert([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Admin([string]$Path, [string]$Method = 'GET', $Body = $null) {
    Invoke-LoomHttp $connection.Client "$base/api/v1/$Path" -Method $Method -Token $secrets.OIS_ADMIN_TOKEN -Body $Body
}
function Call([string]$Case, [string]$Name, [hashtable]$Arguments = @{}, [switch]$AllowError) {
    $result = Invoke-LoomTool $connection $Name $Arguments -AllowError:$AllowError
    Record $Case $Name @{ arguments = $Arguments; result = $result }
    return $result
}
function Finish([string]$Case, [string]$Platform, $Result) {
    $output = [string]$Result.output
    for ($attempt = 0; $Result['session_id']; $attempt++) {
        Assert ($attempt -lt 30) 'Short verification command did not finish.'
        $Result = Call $Case "${Platform}_write_stdin" @{ session_id = $Result.session_id; chars = ''; yield_time_ms = 1000 }
        $output += $Result.output
    }
    return @{ output = $output; exit_code = $Result.exit_code }
}
function ShellQuote([string]$Value) {
    $quote = [string][char]39
    $escapedQuote = $quote + [char]34 + $quote + [char]34 + $quote
    return $quote + $Value.Replace($quote, $escapedQuote) + $quote
}
function Oic([string]$Platform, [string[]]$Arguments, [switch]$ExpectFailure) {
    if ($Platform -eq 'windows') {
        $old = $env:WINDOWS_CHANNEL_TOKEN
        try {
            if ($UseCredentials) { $env:WINDOWS_CHANNEL_TOKEN = $secrets.WINDOWS_CHANNEL_TOKEN }
            $output = & (Join-Path $DeploymentRoot 'windows/bin/oic.exe') --config (Join-Path $DeploymentRoot 'windows/oic.yaml') @Arguments 2>&1
            $exitCode = $LASTEXITCODE
        } finally { $env:WINDOWS_CHANNEL_TOKEN = $old }
    } else {
        $argsText = ($Arguments | ForEach-Object { ShellQuote $_ }) -join ' '
        $command = "cd '$RemoteDirectory' && { if [ -f secrets.env ]; then set -a; . ./secrets.env; set +a; fi; } && ./bin/oic --config ./oic.yaml $argsText"
        $output = & ssh @sshOptions $Remote $command 2>&1
        $exitCode = $LASTEXITCODE
    }
    $text = ($output | Out-String).Trim()
    Record A10 "${Platform}_cli" @{ arguments = $Arguments; exit_code = $exitCode; output = $text }
    if ($ExpectFailure) { Assert ($exitCode -ne 0) 'Expected CLI operation to fail.' }
    else { Assert ($exitCode -eq 0) "OIC operation failed: $text" }
    return @{ exit_code = $exitCode; text = $text }
}
function WaitAvailable([string]$Tool, [int]$Seconds = 30) {
    $until = [DateTime]::UtcNow.AddSeconds($Seconds)
    do {
        $r = Admin "tools/$Tool"
        if ($r.Status -eq 200 -and $r.Body['available']) { return }
        Start-Sleep -Milliseconds 200
    } while ([DateTime]::UtcNow -lt $until)
    throw "Tool $Tool did not become available."
}
function SetWorkbench([string[]]$Names) {
    $access = if ($originalVM['access']) { $originalVM.access } else { @{} }
    $r = Admin 'vmcps/workbench' PUT @{ tools = $Names; access = $access }
    Assert ($r.Status -eq 200) "Cannot update workbench: $($r.Raw)"
}
function WriteJsonYaml([string]$Path, $Value) {
    # JSON is valid YAML and avoids platform-specific quoting in generated paths.
    [IO.File]::WriteAllText($Path, ($Value | ConvertTo-Json -Depth 64), [Text.UTF8Encoding]::new($false))
}

$windowsWorker = @'
param([string]$Label, [string]$PidFile)
$ErrorActionPreference = 'Stop'
if ($PidFile) { [IO.File]::WriteAllText($PidFile, [string]$PID) }
while ($null -ne ($line = [Console]::ReadLine())) {
    $f = $line | ConvertFrom-Json -AsHashtable -Depth 64
    if ($f.type -eq 'start') {
        $response = @{type='ready'; protocol=1; handlers=@('inspect')}
    } elseif ($f.type -eq 'invoke') {
        $response = @{type='result'; call_id=$f.call_id; result=@{pid=$PID; label=$Label; input=$f.input}}
    } elseif ($f.type -eq 'cancel') {
        $response = @{type='error'; call_id=$f.call_id; error=@{code='CALL_CANCELLED'; message='fixture cancelled'}}
    } else { exit 3 }
    [Console]::WriteLine(($response | ConvertTo-Json -Depth 64 -Compress))
}
'@
$linuxWorker = @'
import json, os, sys
label, pid_file = sys.argv[1], sys.argv[2]
if pid_file:
    with open(pid_file, 'w', encoding='utf-8') as f:
        f.write(str(os.getpid()))
for line in sys.stdin:
    frame = json.loads(line)
    if frame['type'] == 'start':
        response = {'type': 'ready', 'protocol': 1, 'handlers': ['inspect']}
    elif frame['type'] == 'invoke':
        response = {'type': 'result', 'call_id': frame['call_id'], 'result': {'pid': os.getpid(), 'label': label, 'input': frame.get('input', {})}}
    elif frame['type'] == 'cancel':
        response = {'type': 'error', 'call_id': frame['call_id'], 'error': {'code': 'CALL_CANCELLED', 'message': 'fixture cancelled'}}
    else:
        sys.exit(3)
    print(json.dumps(response), flush=True)
'@

try {
    $connection = Connect-LoomMcp $Endpoint $secrets.WORKBENCH_TOKEN $CAFile
    $vm = Admin 'vmcps/workbench'
    Assert ($vm.Status -eq 200) 'Cannot read workbench definition.'
    $originalVM = $vm.Body
    Assert ((($originalVM.tools | Sort-Object) -join ',') -eq (($expectedTools | Sort-Object) -join ',')) 'Run against a workbench containing only the four baseline tools.'

    foreach ($platform in $Platforms) {
        $appCode = "acceptance-$platform-$runId"
        $rollbackCode = "rollback-$platform-$runId"
        $staging = Join-Path $DeploymentRoot "lifecycle-fixtures/$platform-$runId"
        New-Item -ItemType Directory -Force -Path $staging | Out-Null
        if ($platform -eq 'windows') {
            $target = Join-Path $DeploymentRoot "windows/apps/$appCode"
            New-Item -ItemType Directory -Force -Path $target | Out-Null
            $workerPath = Join-Path $target 'worker.ps1'
            [IO.File]::WriteAllText($workerPath, $windowsWorker, [Text.UTF8Encoding]::new($false))
            $program = (Get-Command pwsh -ErrorAction Stop).Source
            $rootCommand = @($program, '-NoProfile', '-NonInteractive', '-File', $workerPath, '-Label', 'root', '-PidFile', (Join-Path $target 'root.pid'))
            $overrideCommand = @($program, '-NoProfile', '-NonInteractive', '-File', $workerPath, '-Label', 'override', '-PidFile', (Join-Path $target 'override.pid'))
            $rollbackCommand = @($program, '-NoProfile', '-NonInteractive', '-File', $workerPath, '-Label', 'rollback', '-PidFile', (Join-Path $target 'rollback.pid'))
            $appPath = Join-Path $target 'app.yaml'
            $changedPath = Join-Path $target 'changed.yaml'
            $rollbackPath = Join-Path $target 'rollback.yaml'
            $missingProgram = Join-Path $target 'intentionally-missing.exe'
        } else {
            $target = "$RemoteDirectory/apps/$appCode"
            $workerPath = "$target/worker.py"
            [IO.File]::WriteAllText((Join-Path $staging 'worker.py'), $linuxWorker, [Text.UTF8Encoding]::new($false))
            $rootCommand = @('python3', $workerPath, 'root', "$target/root.pid")
            $overrideCommand = @('python3', $workerPath, 'override', "$target/override.pid")
            $rollbackCommand = @('python3', $workerPath, 'rollback', "$target/rollback.pid")
            $appPath = "$target/app.yaml"; $changedPath = "$target/changed.yaml"; $rollbackPath = "$target/rollback.yaml"
            $missingProgram = "$target/intentionally-missing"
        }
        $schema = @{ type = 'object'; properties = @{ count = @{ type = 'integer'; default = 41 } }; required = @('count') }
        $definition = @{
            version = 1; app = @{ code = $appCode }; runtime = @{ command = $rootCommand }
            actions = @{
                inspect = @{ description = 'Inspect fixture runtime using same-name handler'; input_schema = $schema; input_mapping = @{ count = 'number' } }
                inherited = @{ description = 'Uses the root runtime instance'; handler = 'inspect'; input_schema = $schema; runtime = @{} }
                override = @{ description = 'Replaces root command and owns a separate instance'; handler = 'inspect'; input_schema = $schema; runtime = @{ command = $overrideCommand } }
            }
        }
        $changed = $definition | ConvertTo-Json -Depth 64 | ConvertFrom-Json -AsHashtable -Depth 64
        $changed.actions.inspect.description = 'Changed live definition must be rejected'
        $rollback = @{
            version = 1; app = @{ code = $rollbackCode }
            actions = @{
                a_ready = @{ description = 'First runtime starts successfully'; handler = 'inspect'; input_schema = $schema; runtime = @{ command = $rollbackCommand } }
                z_fail = @{ description = 'Second runtime cannot start'; handler = 'inspect'; input_schema = $schema; runtime = @{ command = @($missingProgram) } }
            }
        }
        if ($platform -eq 'windows') {
            WriteJsonYaml $appPath $definition; WriteJsonYaml $changedPath $changed; WriteJsonYaml $rollbackPath $rollback
        } else {
            WriteJsonYaml (Join-Path $staging 'app.yaml') $definition
            WriteJsonYaml (Join-Path $staging 'changed.yaml') $changed
            WriteJsonYaml (Join-Path $staging 'rollback.yaml') $rollback
            & ssh @sshOptions $Remote "mkdir -p '$target'"
            Assert ($LASTEXITCODE -eq 0) 'Cannot prepare Linux fixture directory.'
            & scp @sshOptions (Join-Path $staging 'worker.py') (Join-Path $staging 'app.yaml') (Join-Path $staging 'changed.yaml') (Join-Path $staging 'rollback.yaml') "${Remote}:$target/"
            Assert ($LASTEXITCODE -eq 0) 'Cannot upload Linux fixture.'
        }

        $loadedFixtures.Add(@{ platform = $platform; app = $appCode })
        [void](Oic $platform @('load', $appPath))
        $names = @{}
        foreach ($action in @('inspect', 'inherited', 'override')) {
            $name = "acceptance_${platform}_${action}_$runId"
            $names[$action] = $name
            $createdTools.Add($name)
            $r = Admin "tools/$name" PUT @{
                description = 'Temporary lifecycle acceptance fixture'
                source = @{ channel = $(if ($platform -eq 'linux') { 'linux-151' } else { 'windows-local' }); app = $appCode; action = $action }
                input_schema = @{ type = 'object'; properties = @{ number = @{ type = 'string' } } }
            }
            Assert ($r.Status -in 200, 201) "Cannot create temporary Tool: $($r.Raw)"
        }
        SetWorkbench (@($expectedTools) + $createdTools.ToArray())
        foreach ($name in $names.Values) { WaitAvailable $name }
        $root = Call A07 $names.inspect @{ number = '42' }
        $inherited = Call A07 $names.inherited
        $overridden = Call A07 $names.override
        Assert ($root.input.count -eq 42 -and -not $root.input.ContainsKey('number')) 'Local Schema rename/type adaptation failed.'
        Assert ($inherited.input.count -eq 41) 'Local fixed default was not applied.'
        Assert ($root.pid -eq $inherited.pid -and $root.label -eq 'root') 'Root runtime was not shared.'
        Assert ($root.pid -ne $overridden.pid -and $overridden.label -eq 'override') 'Action runtime did not replace command or own its instance.'
        Record A07 "${platform}_PASS" @{ root = $root; inherited = $inherited; overridden = $overridden }

        [void](Oic $platform @('load', $appPath))
        $same = Call A10 $names.inspect
        Assert ($same.pid -eq $root.pid) 'Identical load started a different runtime.'
        $rejected = Oic $platform @('load', $changedPath) -ExpectFailure
        Assert ($rejected.text.Contains('APP_ALREADY_LOADED')) 'Changed definition failed for an unexpected reason.'
        $same = Call A10 $names.inspect
        Assert ($same.pid -eq $root.pid) 'Rejected change disturbed the existing runtime.'
        [void](Oic $platform @('load', $rollbackPath) -ExpectFailure)
        $apps = (Oic $platform @('apps')).text | ConvertFrom-Json -AsHashtable -Depth 64
        Assert (@($apps | Where-Object { $_.app -eq $rollbackCode }).Count -eq 0) 'Failed initialization published a partial App.'
        $pidPath = if ($platform -eq 'linux') { "$target/rollback.pid" } else { Join-Path $target 'rollback.pid' }
        if ($platform -eq 'linux') {
            $check = 'test -s ' + (ShellQuote $pidPath) + ' || exit 23; p=$(cat ' + (ShellQuote $pidPath) + '); if kill -0 "$p" 2>/dev/null; then echo alive; else echo gone; fi'
        } else {
            $literal = $pidPath.Replace("'", "''")
            $check = 'if (-not (Test-Path -LiteralPath ''' + $literal + ''')) { exit 23 }; $fixturePid = [int](Get-Content -LiteralPath ''' + $literal + '''); if (Get-Process -Id $fixturePid -ErrorAction SilentlyContinue) { ''alive'' } else { ''gone'' }'
        }
        $reaped = Finish A10 $platform (Call A10 "${platform}_exec_command" @{ cmd = $check })
        Assert ($reaped.exit_code -eq 0 -and $reaped.output.Trim() -eq 'gone') 'Rollback left its started runtime alive or produced no PID evidence.'
        Record A10 "${platform}_load_PASS" @{ original_pid = $root.pid; inherited_pid = $inherited.pid; independent_pid = $overridden.pid; rollback = $reaped }

        # Remove temporary consumer definitions before invalidating their runtimes.
        SetWorkbench $expectedTools
        foreach ($name in $createdTools.ToArray()) {
            $r = Admin "tools/$name" DELETE
            Assert ($r.Status -eq 204) "Cannot remove fixture Tool $name"
            [void]$createdTools.Remove($name)
        }
        [void](Oic $platform @('unload', $appCode))
        for ($i = $loadedFixtures.Count - 1; $i -ge 0; $i--) { if ($loadedFixtures[$i].app -eq $appCode) { $loadedFixtures.RemoveAt($i) } }

        if (-not $SkipRestart) {
            $cmd = if ($platform -eq 'linux') { 'sleep 30; echo old-execution' } else { 'Start-Sleep -Seconds 30; Write-Output old-execution' }
            $oldSession = Call A10 "${platform}_exec_command" @{ cmd = $cmd; yield_time_ms = 0 }
            Assert ([bool]$oldSession['session_id']) 'Restart test did not create a live session.'
            $restartRecovery.Add($platform)
            [void](Oic $platform @('stop'))
            [void](Oic $platform @('start'))
            WaitAvailable "${platform}_exec_command"
            [void]$restartRecovery.Remove($platform)
            $denied = Call A10 "${platform}_write_stdin" @{ session_id = $oldSession.session_id; chars = ''; yield_time_ms = 0 } -AllowError
            Assert ($denied.error.code -eq 'SESSION_UNAVAILABLE') 'Old session survived OIC restart.'
            $newCmd = if ($platform -eq 'linux') { 'echo restart-ok' } else { 'Write-Output restart-ok' }
            $newExecution = Finish A10 $platform (Call A10 "${platform}_exec_command" @{ cmd = $newCmd })
            Assert ($newExecution.exit_code -eq 0 -and $newExecution.output.Trim() -eq 'restart-ok') 'Persisted App did not reload after restart.'
            Record A10 "${platform}_restart_PASS" @{ old_session = $oldSession.session_id; rejected = $denied; new_execution = $newExecution }
        }
    }
    $list = Invoke-McpRequest $connection tools/list
    $actual = @($list.Body.result.tools | ForEach-Object { $_.name })
    Assert ((($actual | Sort-Object) -join ',') -eq (($expectedTools | Sort-Object) -join ',')) 'Cleanup did not restore exactly four tools.'
    Record CLEANUP PASS @{ tools = $actual; skipped_restart = [bool]$SkipRestart }
    Write-Output "PASS A07 runtime/Schema and A10 loading$(if (-not $SkipRestart) { '/restart' }). Evidence: $report"
} catch {
    Record FAILURE exception @{ message = $_.Exception.Message; position = $_.InvocationInfo.PositionMessage }
    throw
} finally {
    if ($connection) {
        foreach ($platform in $restartRecovery.ToArray()) {
            try { [void](Oic $platform @('start')); WaitAvailable "${platform}_exec_command" }
            catch { Record CLEANUP restart_error @{ platform = $platform; message = $_.Exception.Message } }
        }
        if ($originalVM) {
            try { SetWorkbench @($originalVM.tools) } catch { Record CLEANUP workbench_error @{ message = $_.Exception.Message } }
        }
        foreach ($name in $createdTools.ToArray()) {
            try { $r = Admin "tools/$name" DELETE; if ($r.Status -notin 204, 404) { throw "HTTP $($r.Status)" } }
            catch { Record CLEANUP tool_error @{ tool = $name; message = $_.Exception.Message } }
        }
        foreach ($fixture in $loadedFixtures.ToArray()) {
            try { [void](Oic $fixture.platform @('unload', $fixture.app)) }
            catch { Record CLEANUP app_error @{ app = $fixture.app; message = $_.Exception.Message } }
        }
        $connection.Client.Dispose()
    }
}
