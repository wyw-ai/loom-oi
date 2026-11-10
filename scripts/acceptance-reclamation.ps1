param(
    [string]$DeploymentRoot = (Join-Path $PSScriptRoot '..\.local'),
    [string]$Endpoint = 'http://121.199.175.151:25100/mcp/workbench',
    [switch]$UseCredentials,
    [string]$Token,
    [string]$AdminToken,
    [string]$CAFile,
    [string]$Remote = 'admin@121.199.175.151',
    [string]$RemoteDirectory = '/home/admin/loom-oi-beta'
)
# A11 intentionally unloads/crashes/stops beta's command Apps. Run after other
# active acceptance executions finish; no OIS or unrelated service is stopped.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Import-Module (Join-Path $PSScriptRoot 'McpClient.psm1') -Force
$DeploymentRoot = [IO.Path]::GetFullPath($DeploymentRoot)
if ($RemoteDirectory -notmatch '^/home/[A-Za-z0-9_-]+/[A-Za-z0-9_-]+$') { throw 'Use the dedicated beta deployment directory under the SSH user home.' }
$secrets = Get-LoomCredentials $DeploymentRoot -UseCredentials:$UseCredentials
if ($PSBoundParameters.ContainsKey('Token')) { $secrets.WORKBENCH_TOKEN = $Token }
if ($PSBoundParameters.ContainsKey('AdminToken')) { $secrets.OIS_ADMIN_TOKEN = $AdminToken }
$windowsRoot = Join-Path $DeploymentRoot 'windows'
$localOic = Join-Path $windowsRoot 'bin/oic.exe'
$localConfig = Join-Path $windowsRoot 'oic.yaml'
$localApp = Join-Path $windowsRoot 'apps/powershell/app.yaml'
$localWorker = [IO.Path]::GetFullPath((Join-Path $windowsRoot 'apps/powershell/runtime/oiapp-powershell.exe'))
$sshOptions = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$reports = Join-Path $DeploymentRoot 'reports'
New-Item -ItemType Directory -Force $reports | Out-Null
$report = Join-Path $reports "reclamation-$([DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss')).jsonl"

function Record([string]$Step, $Evidence) {
    @{ timestamp = [DateTime]::UtcNow.ToString('o'); case = 'A11'; step = $Step; evidence = $Evidence } |
        ConvertTo-Json -Depth 64 -Compress | Add-Content -LiteralPath $report -Encoding utf8NoBOM
}
function Assert([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }

function Invoke-Oic([string]$Platform, [string]$Command) {
    if ($Platform -eq 'windows') {
        $arguments = @('--config', $localConfig, $Command)
        if ($Command -eq 'load') { $arguments += $localApp }
        if ($Command -eq 'unload') { $arguments += 'powershell' }
        $oldToken = $env:WINDOWS_CHANNEL_TOKEN
        try {
            if ($UseCredentials) { $env:WINDOWS_CHANNEL_TOKEN = $secrets.WINDOWS_CHANNEL_TOKEN }
            $raw = & $localOic @arguments
            if ($LASTEXITCODE -ne 0) { throw "Windows OIC $Command failed ($LASTEXITCODE)" }
        } finally { $env:WINDOWS_CHANNEL_TOKEN = $oldToken }
    } else {
        if ($Command -notin @('status', 'apps', 'load', 'unload', 'start', 'stop')) { throw 'Unsupported OIC operation' }
        $extra = switch ($Command) { 'load' { ' apps/bash/app.yaml' } 'unload' { ' bash' } default { '' } }
        $shell = "cd '$RemoteDirectory' && { if [ -f secrets.env ]; then set -a; . ./secrets.env; set +a; fi; } && bin/oic --config oic.yaml $Command$extra"
        $raw = & ssh @sshOptions $Remote $shell
        if ($LASTEXITCODE -ne 0) { throw "Linux OIC $Command failed ($LASTEXITCODE)" }
    }
    $text = $raw -join "`n"
    if ($text.Trim()) { return ($text | ConvertFrom-Json -AsHashtable -Depth 64) }
    return $null
}

# All remote process inspection and the one authorized SIGKILL are scoped by
# PID birth time, exact executable path, and ancestry under this beta OIC.
$linuxProbe = @'
import base64, glob, json, os, signal, sys
request = json.loads(base64.b64decode(sys.argv[1]))
def inspect(pid):
    try:
        text = open('/proc/%d/stat' % pid).read()
        fields = text[text.rfind(')') + 1:].split()
        return dict(pid=pid, alive=fields[0] != 'Z', state=fields[0], parent=int(fields[1]),
                    born=fields[19], exe=os.readlink('/proc/%d/exe' % pid),
                    argv=open('/proc/%d/cmdline' % pid, 'rb').read().decode('utf-8', 'replace').split('\0')[:-1])
    except (FileNotFoundError, ProcessLookupError, PermissionError):
        return dict(pid=pid, alive=False)
def owned_by(pid, owner):
    seen=set()
    while pid > 1 and pid not in seen:
        if pid == owner: return True
        seen.add(pid)
        row=inspect(pid)
        if not row.get('alive'): return False
        pid=row['parent']
    return False
action=request['action']
if action == 'inspect':
    result=[inspect(int(pid)) for pid in request['pids']]
elif action == 'tree':
    result=[]
    for path in glob.glob('/proc/[0-9]*'):
        row=inspect(int(path.rsplit('/', 1)[-1]))
        if row.get('alive') and owned_by(row['pid'], request['worker_pid']):
            result.append(row)
elif action == 'worker':
    result=[]
    for path in glob.glob('/proc/[0-9]*'):
        row=inspect(int(path.rsplit('/', 1)[-1]))
        if (row.get('alive') and row.get('exe') == request['exe'] and
            '--loom-process-supervisor' not in row.get('argv', []) and
            owned_by(row['pid'], request['oic_pid'])):
            result.append(row)
elif action == 'kill_worker':
    prior=request['worker']; row=inspect(prior['pid'])
    if not (row.get('alive') and row.get('born') == prior['born'] and row.get('exe') == request['exe']
            and owned_by(row['pid'], request['oic_pid'])):
        raise RuntimeError('worker identity or beta OIC ancestry changed; refusing SIGKILL')
    os.kill(row['pid'], signal.SIGKILL)
    result=dict(killed=row['pid'], born=row['born'], exe=row['exe'])
else:
    raise RuntimeError('unknown probe operation')
print(json.dumps(result))
'@
$linuxProbeEncoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($linuxProbe))
function Invoke-LinuxProbe([hashtable]$Request) {
    $encoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes(($Request | ConvertTo-Json -Depth 32 -Compress)))
    # Both substituted values are Base64, not interpolated shell source.
    $command = "python3 -c 'import base64; exec(base64.b64decode(""$linuxProbeEncoded""))' '$encoded'"
    $raw = & ssh @sshOptions $Remote $command
    if ($LASTEXITCODE -ne 0) { throw "Linux process probe failed ($LASTEXITCODE)" }
    return (($raw -join "`n") | ConvertFrom-Json -AsHashtable -Depth 32)
}

function Get-WindowsProcess([int]$ProcessId) {
    $p = Get-CimInstance Win32_Process -Filter "ProcessId=$ProcessId" -ErrorAction Stop
    if (-not $p) { return @{ pid = $ProcessId; alive = $false } }
    return @{ pid = $ProcessId; alive = $true; parent = [int]$p.ParentProcessId; born = $p.CreationDate.ToUniversalTime().ToString('o'); exe = $p.ExecutablePath; argv = $p.CommandLine }
}
function Inspect-Processes([string]$Platform, [int[]]$ProcessIds) {
    if ($Platform -eq 'linux') { return @(Invoke-LinuxProbe @{ action = 'inspect'; pids = $ProcessIds }) }
    return @($ProcessIds | ForEach-Object { Get-WindowsProcess $_ })
}
function Find-Worker([string]$Platform, [int]$OicProcessId) {
    if ($Platform -eq 'linux') {
        $rows = @(Invoke-LinuxProbe @{ action = 'worker'; exe = "$RemoteDirectory/apps/bash/runtime/oiapp-bash"; oic_pid = $OicProcessId })
    } else {
        $rows = @(Get-CimInstance Win32_Process -Filter "Name='oiapp-powershell.exe'" | Where-Object {
            $_.ExecutablePath -and [IO.Path]::GetFullPath($_.ExecutablePath) -eq $localWorker -and $_.ParentProcessId -eq $OicProcessId
        } | ForEach-Object { Get-WindowsProcess ([int]$_.ProcessId) })
    }
    Assert ($rows.Count -eq 1) "$Platform must have exactly one owned command worker; found $($rows.Count)"
    return $rows[0]
}
function Inspect-OwnedTree([string]$Platform, [int]$WorkerProcessId) {
    if ($Platform -eq 'linux') { return @(Invoke-LinuxProbe @{ action = 'tree'; worker_pid = $WorkerProcessId }) }
    $all = @(Get-CimInstance Win32_Process)
    $owned = [System.Collections.Generic.HashSet[int]]::new()
    [void]$owned.Add($WorkerProcessId)
    do {
        $changed = $false
        foreach ($entry in $all) {
            if ($owned.Contains([int]$entry.ParentProcessId) -and $owned.Add([int]$entry.ProcessId)) { $changed = $true }
        }
    } while ($changed)
    return @($owned | ForEach-Object { Get-WindowsProcess $_ } | Where-Object { $_.alive })
}
function Wait-OriginalProcessesGone([string]$Platform, [array]$Before) {
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        $after = @(Inspect-Processes $Platform @($Before | ForEach-Object { [int]$_.pid }))
        $remaining = @($after | Where-Object {
            $current = $_
            $prior = @($Before | Where-Object { $_.pid -eq $current.pid })[0]
            $current.alive -and $current.born -eq $prior.born
        })
        if ($remaining.Count -eq 0) { return $after }
        Start-Sleep -Milliseconds 150
    } while ([DateTime]::UtcNow -lt $deadline)
    Record "${Platform}_processes_still_alive" $remaining
    throw "$Platform left owned command processes alive: $($remaining.pid -join ',')"
}

function Ensure-Ready([string]$Platform) {
    $status = Invoke-Oic $Platform status
    if (-not $status.running) { [void](Invoke-Oic $Platform start) }
    $appCode = if ($Platform -eq 'linux') { 'bash' } else { 'powershell' }
    $apps = @(Invoke-Oic $Platform apps)
    $app = @($apps | Where-Object { $_.app -eq $appCode })
    if ($app.Count -gt 0 -and -not $app[0].ready) { [void](Invoke-Oic $Platform unload); $app = @() }
    if ($app.Count -eq 0) { [void](Invoke-Oic $Platform load) }
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $app = @((Invoke-Oic $Platform apps) | Where-Object { $_.app -eq $appCode })
        if ($app.Count -eq 1 -and $app[0].ready -and $app[0].registered) { return $app[0] }
        Start-Sleep -Milliseconds 200
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "$Platform App did not recover ready/registered"
}

$bashTree = @'
sleep 300 &
child=$!
printf 'A11_PARENT=%s A11_CHILD=%s\n' "$$" "$child"
wait "$child"
'@
$powershellTree = @'
$childInfo = [System.Diagnostics.ProcessStartInfo]::new((Get-Process -Id $PID).Path)
$childInfo.UseShellExecute = $false
$childInfo.CreateNoWindow = $true
foreach ($argument in @('-NoLogo','-NoProfile','-NonInteractive','-Command','Start-Sleep -Seconds 300')) { [void]$childInfo.ArgumentList.Add($argument) }
$child = [System.Diagnostics.Process]::Start($childInfo)
Write-Output ('A11_PARENT=' + $PID + ' A11_CHILD=' + $child.Id)
$child.WaitForExit()
'@

$connection = Connect-LoomMcp -Url $Endpoint -Token $secrets.WORKBENCH_TOKEN -CAFile $CAFile
try {
    foreach ($platform in @('linux', 'windows')) {
        foreach ($trigger in @('unload', 'worker_crash', 'stop')) {
            [void](Ensure-Ready $platform)
            $status = Invoke-Oic $platform status
            $worker = Find-Worker $platform ([int]$status.pid)
            $executions = @()
            $before = @()
            try {
                foreach ($tty in @($false, $true)) {
                    $command = if ($platform -eq 'linux') { $bashTree } else { $powershellTree }
                    $arguments = @{ cmd = $command; tty = $tty; yield_time_ms = 1000; max_output_tokens = 2000 }
                    $r = Invoke-LoomTool $connection "${platform}_exec_command" $arguments
                    $output = [string]$r.output
                    for ($attempt = 0; $output -notmatch 'A11_PARENT=(\d+) A11_CHILD=(\d+)' -and $attempt -lt 4; $attempt++) {
                        Assert ([bool]$r['session_id']) 'PID marker command ended before cleanup test'
                        $r = Invoke-LoomTool $connection "${platform}_write_stdin" @{ session_id = $r.session_id; chars = ''; yield_time_ms = 1000 }
                        $output += $r.output
                    }
                    Assert ($output -match 'A11_PARENT=(\d+) A11_CHILD=(\d+)') "$platform did not return owned PIDs"
                    $parentProcessId = [int]$Matches[1]
                    $childProcessId = [int]$Matches[2]
                    $observed = @(Inspect-Processes $platform @($parentProcessId, $childProcessId))
                    Assert ($observed.Count -eq 2 -and @($observed | Where-Object { -not $_.alive }).Count -eq 0) 'OS did not independently confirm both command PIDs alive'
                    Assert ($observed[1].parent -eq $parentProcessId) 'Reported child is not a child of the reported command'
                    $before += $observed
                    $executions += @{ tty = $tty; session_id = $r.session_id; pids = @($parentProcessId, $childProcessId); call_id = $r['_call_id'] }
                    Record "${platform}_${trigger}_created" @{ arguments = $arguments; result = $r; observed_processes = $observed; worker = $worker }
                }

                # Include supervisors and visible ConPTY host descendants in
                # addition to the four PIDs explicitly reported by commands.
                $ownedTree = @(Inspect-OwnedTree $platform ([int]$worker.pid))
                Assert ($ownedTree.Count -ge 5) 'OS snapshot did not find the expected worker and command trees'
                $allBefore = @($before + $ownedTree + @($worker) | Sort-Object -Property pid -Unique)
                Record "${platform}_${trigger}_owned_tree" $allBefore

                if ($trigger -eq 'worker_crash') {
                    if ($platform -eq 'linux') {
                        $killed = Invoke-LinuxProbe @{ action = 'kill_worker'; worker = $worker; exe = "$RemoteDirectory/apps/bash/runtime/oiapp-bash"; oic_pid = [int]$status.pid }
                    } else {
                        $current = Get-WindowsProcess ([int]$worker.pid)
                        Assert ($current.alive -and $current.born -eq $worker.born -and $current.exe -eq $localWorker -and $current.parent -eq $status.pid) 'Worker identity changed; refusing termination'
                        Stop-Process -Id ([int]$worker.pid) -Force -ErrorAction Stop
                        $killed = @{ killed = $worker.pid; born = $worker.born; exe = $worker.exe }
                    }
                    Record "${platform}_${trigger}_triggered" $killed
                } else {
                    $response = Invoke-Oic $platform $trigger
                    Record "${platform}_${trigger}_triggered" $response
                }

                $after = Wait-OriginalProcessesGone $platform $allBefore
                if ($trigger -eq 'worker_crash') {
                    # No recovery operation has run yet: the crashed worker
                    # must remain unavailable rather than automatically restart.
                    Start-Sleep -Milliseconds 400
                    $apps = @(Invoke-Oic $platform apps)
                    Assert ($apps.Count -eq 1 -and -not $apps[0].ready) 'Crashed App restarted or stayed ready'
                }
                if ($trigger -eq 'unload') { Assert (@(Invoke-Oic $platform apps).Count -eq 0) 'Unload retained the App registration' }
                if ($trigger -eq 'stop') { Assert (-not (Invoke-Oic $platform status).running) 'OIC stop did not stop the service' }
                Record "${platform}_${trigger}_PASS" @{ executions = $executions; before = $allBefore; after = $after }
                Write-Output "PASS A11 $platform $trigger (pipe + terminal parent/child PIDs reclaimed)"
            } catch {
                Record "${platform}_${trigger}_FAIL" @{ message = $_.Exception.Message; executions = $executions; before = $before }
                # Retain the failed evidence, then stop only this beta OIC to
                # recover any test-owned processes before restoring readiness.
                try { [void](Invoke-Oic $platform stop) } catch { Record "${platform}_cleanup_error" @{ message = $_.Exception.Message } }
                throw
            } finally {
                $recovered = Ensure-Ready $platform
                Record "${platform}_${trigger}_restored" $recovered
            }
        }
    }
    Write-Output "PASS A11 on Linux and Windows. Evidence: $report"
} catch {
    Record FAILURE @{ message = $_.Exception.Message; position = $_.InvocationInfo.PositionMessage }
    throw
} finally { $connection.Client.Dispose() }
