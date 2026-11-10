param(
    [string]$DeploymentRoot = (Join-Path $PSScriptRoot '..\.local'),
    [string]$Endpoint = 'http://121.199.175.151:25100/mcp/workbench',
    [switch]$UseCredentials,
    [string]$Token,
    [string]$AdminToken,
    [string]$CAFile,
    [switch]$LongRunningOnly
)
$ErrorActionPreference = 'Stop'
Import-Module (Join-Path $PSScriptRoot 'McpClient.psm1') -Force
$DeploymentRoot = [IO.Path]::GetFullPath($DeploymentRoot)
$secrets = Get-LoomCredentials $DeploymentRoot -UseCredentials:$UseCredentials
if ($PSBoundParameters.ContainsKey('Token')) { $secrets.WORKBENCH_TOKEN = $Token }
if ($PSBoundParameters.ContainsKey('AdminToken')) { $secrets.OIS_ADMIN_TOKEN = $AdminToken }
$reports = Join-Path $DeploymentRoot 'reports'
New-Item -ItemType Directory -Force $reports | Out-Null
$suffix = if ($LongRunningOnly) { 'long' } else { 'commands' }
$report = Join-Path $reports "$suffix-$([DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss')).jsonl"
function Record([string]$Case, [string]$Step, $Evidence) {
    @{ timestamp = [DateTime]::UtcNow.ToString('o'); case = $Case; step = $Step; evidence = $Evidence } |
        ConvertTo-Json -Depth 64 -Compress | Add-Content -LiteralPath $report -Encoding utf8NoBOM
}
function Assert([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
$connection = Connect-LoomMcp -Url $Endpoint -Token $secrets.WORKBENCH_TOKEN -CAFile $CAFile
function Call([string]$Case, [string]$Name, [hashtable]$Arguments, [switch]$AllowError) {
    $result = Invoke-LoomTool $connection $Name $Arguments -AllowError:$AllowError
    Record $Case $Name @{ arguments = $Arguments; result = $result }
    return $result
}
function Finish([string]$Case, [string]$Platform, $Result) {
    $combined = [string]$Result.output
    for ($attempt = 0; $Result['session_id']; $attempt++) {
        if ($attempt -gt 30) { throw 'Short execution did not finish' }
        $Result = Call $Case "${Platform}_write_stdin" @{session_id=$Result.session_id;chars='';yield_time_ms=1000}
        $combined += $Result.output
    }
    return @{output=$combined;exit_code=$Result.exit_code;final=$Result}
}
try {
    if ($LongRunningOnly) {
        $states = @{}
        foreach ($platform in @('linux','windows')) {
            $cmd = if ($platform -eq 'linux') { "sleep 600; printf 'done\n'" } else { "Start-Sleep -Seconds 600; Write-Output 'done'" }
            $states[$platform] = @{start=[DateTime]::UtcNow;arguments=@{cmd=$cmd;yield_time_ms=1000};done=$false;output='';polls=0}
            $states[$platform].pending = Start-McpRequest $connection tools/call @{name="${platform}_exec_command";arguments=$states[$platform].arguments}
        }
        foreach ($platform in @('linux','windows')) {
            $s = $states[$platform]
            $r = Get-McpToolResult (Complete-LoomHttp $s.pending)
            Record A04 "${platform}_start" @{arguments=$s.arguments;result=$r}
            Assert ([bool]$r['session_id']) "$platform must return a continuation handle"
            Assert ($r.output -eq '') "$platform emitted unexpected output before 600 seconds"
            $s.session = $r.session_id
        }
        while (@($states.Values | Where-Object { -not $_.done }).Count -gt 0) {
            foreach ($platform in @('linux','windows')) {
                $s=$states[$platform]; if ($s.done) { continue }
                $s.pending = Start-McpRequest $connection tools/call @{name="${platform}_write_stdin";arguments=@{session_id=$s.session;chars='';yield_time_ms=30000}}
            }
            foreach ($platform in @('linux','windows')) {
                $s=$states[$platform]; if ($s.done) { continue }
                $r = Get-McpToolResult (Complete-LoomHttp $s.pending)
                $s.polls++;$s.output += $r.output
                $elapsed = ([DateTime]::UtcNow-$s.start).TotalSeconds
                Record A04 "${platform}_poll" @{elapsed_seconds=$elapsed;result=$r}
                if ($r['session_id']) {
                    Assert ($r.session_id -eq $s.session) 'Continuation handle changed'
                    Assert ($r.output -eq '') 'Unexpected output before long command completion'
                    Assert ($elapsed -lt 700) 'Long command failed to complete within 700 seconds'
                } else {
                    Assert ($r.exit_code -eq 0 -and $s.output.Trim() -eq 'done') "$platform long command final result is wrong"
                    Assert ($elapsed -ge 600) "$platform did not run for at least 600 seconds"
                    $s.done=$true
                    Record A04 "${platform}_PASS" @{elapsed_seconds=$elapsed;polls=$s.polls;session_id=$s.session;output=$s.output}
                }
                Write-Output ("A04 {0}: {1:N1}s, completed={2}" -f $platform,$elapsed,$s.done)
            }
        }
        Write-Output "PASS A04 on Linux and Windows. Evidence: $report"
        return
    }
    $list = Invoke-McpRequest $connection tools/list
    $expected = @('linux_exec_command','linux_write_stdin','windows_exec_command','windows_write_stdin')
    $names = @($list.Body.result.tools | ForEach-Object { $_.name })
    Assert ((($names | Sort-Object) -join ',') -eq (($expected | Sort-Object) -join ',')) 'Unexpected tool list'
    foreach ($tool in $list.Body.result.tools) { Assert (-not $tool.inputSchema.properties.ContainsKey('host_id')) 'Tool exposes host routing' }
    $base = $Endpoint.Substring(0,$Endpoint.IndexOf('/mcp/'))
    $status = Invoke-LoomHttp $connection.Client "$base/api/v1/status" -Token $secrets.OIS_ADMIN_TOKEN
    Assert ($status.Body.hosts.online -eq 2 -and $status.Body.actions.available -eq 4) 'Expected two hosts and four actions'
    Record A01 PASS @{tools=$list.Body;status=$status.Body}
    foreach ($platform in @('linux','windows')) {
        $cmd = if ($platform -eq 'linux') { "printf 'loom-linux-marker\n'; uname -s; pwd" } else { "Write-Output 'loom-windows-marker'; [Environment]::OSVersion.Platform; (Get-Location).Path" }
        $r=Finish A02 $platform (Call A02 "${platform}_exec_command" @{cmd=$cmd})
        Assert ($r.exit_code -eq 0 -and $r.output.Contains("loom-$platform-marker")) "$platform short command failed"
        Assert ($r.output.Contains($(if($platform -eq 'linux'){'Linux'}else{'Win32NT'}))) "$platform OS evidence absent"
        Record A02 "${platform}_PASS" $r
    }
    $linuxDir='/home/admin/loom-oi-beta/workspace'
    $windowsDir=Join-Path $DeploymentRoot 'windows/workspace'
    $r=Finish A03 linux (Call A03 linux_exec_command @{cmd='export LOOM_STATE=secret; cd /; printf "%s\n" "$PWD"';workdir=$linuxDir})
    Assert ($r.output.Trim() -eq '/') 'Bash cd failed'
    $r=Finish A03 linux (Call A03 linux_exec_command @{cmd='printf "%s|%s\n" "$PWD" "${LOOM_STATE-unset}"';workdir=$linuxDir})
    Assert ($r.output.Trim() -eq "$linuxDir|unset") 'Bash inherited previous invocation state'
    $r=Finish A03 windows (Call A03 windows_exec_command @{cmd='$env:LOOM_STATE="secret"; Set-Location C:\; (Get-Location).Path';workdir=$windowsDir})
    Assert ($r.output.Trim() -eq 'C:\') 'PowerShell Set-Location failed'
    $r=Finish A03 windows (Call A03 windows_exec_command @{cmd='Write-Output ((Get-Location).Path + "|" + [string]::IsNullOrEmpty($env:LOOM_STATE))';workdir=$windowsDir})
    Assert ($r.output.Trim() -eq "$windowsDir|True") 'PowerShell inherited previous invocation state'
    Record A03 PASS @{linux_workdir=$linuxDir;windows_workdir=$windowsDir}

    $sessions=@{}
    foreach ($platform in @('linux','windows')) {
        $cmd=if($platform -eq 'linux'){'printf "name: "; read -r name; printf "hello %s\n" "$name"'}else{'$name = Read-Host "name"; Write-Output "hello $name"'}
        $r=Call A05 "${platform}_exec_command" @{cmd=$cmd;tty=$true;yield_time_ms=1000}
        Assert ([bool]$r['session_id']) "$platform TTY did not return handle"
        $sessions[$platform]=$r.session_id
        $other=if($platform -eq 'linux'){'windows'}else{'linux'}
        $denied=Call A06 "${other}_write_stdin" @{session_id=$r.session_id;chars='';yield_time_ms=0} -AllowError
        Assert ($denied.error.code -eq 'SESSION_UNAVAILABLE') 'Cross-App session was accepted'
        $input=if($platform -eq 'linux'){"loom`n"}else{"loom`r"}
        $answer=Finish A05 $platform (Call A05 "${platform}_write_stdin" @{session_id=$r.session_id;chars=$input;yield_time_ms=1000})
        Assert ($answer.exit_code -eq 0 -and $answer.output.Contains('hello loom')) "$platform terminal input failed"
        Record A05 "${platform}_PASS" $answer
    }
    # A second vMCP establishes a different scope for the same Tool, including
    # anonymous mode. Preserve the live authentication/HTTPS policy rather than
    # accidentally enabling credentials just because an old local file exists.
    $otherVm='acceptance-isolation'
    $workbench=Invoke-LoomHttp $connection.Client "$base/api/v1/vmcps/workbench" -Token $secrets.OIS_ADMIN_TOKEN
    Assert ($workbench.Status -eq 200) 'Cannot read current workbench access policy'
    $access=if($workbench.Body['access']){$workbench.Body.access}else{@{}}
    $put=Invoke-LoomHttp $connection.Client "$base/api/v1/vmcps/$otherVm" -Method PUT -Token $secrets.OIS_ADMIN_TOKEN -Body @{tools=$expected;access=$access}
    Assert ($put.Status -in 200,201) 'Cannot create isolation vMCP'
    $second=$null
    try {
        $second=Connect-LoomMcp "$base/mcp/$otherVm" $secrets.WORKBENCH_TOKEN $CAFile
        foreach ($platform in @('linux','windows')) {
            $cmd=if($platform -eq 'linux'){'sleep 2; echo owner'}else{'Start-Sleep -Seconds 2; Write-Output owner'}
            $r=Call A06 "${platform}_exec_command" @{cmd=$cmd;yield_time_ms=0}
            $denied=Invoke-LoomTool $second "${platform}_write_stdin" @{session_id=$r.session_id;chars='';yield_time_ms=0} -AllowError
            Assert ($denied.error.code -eq 'SESSION_UNAVAILABLE') 'Cross-vMCP session was accepted'
            Record A06 "${platform}_scope_denied" @{result=$denied;access_policy=$access}
            [void](Finish A06 $platform $r)
        }
    } finally {
        if($second){$second.Client.Dispose()}
        [void](Invoke-LoomHttp $connection.Client "$base/api/v1/vmcps/$otherVm" -Method DELETE -Token $secrets.OIS_ADMIN_TOKEN)
    }
    foreach ($platform in @('linux','windows')) {
        $cmd=if($platform -eq 'linux'){'echo adapted'}else{'Write-Output adapted'}
        $r=Finish A07 $platform (Call A07 "${platform}_exec_command" @{cmd=$cmd;yield_time_ms='1000'})
        Assert ($r.exit_code -eq 0 -and $r.output.Contains('adapted')) 'Numeric string adaptation failed'
        $bad=Call A07 "${platform}_exec_command" @{cmd=$cmd;yield_time_ms=@{not='integer'}} -AllowError
        Assert ($bad.error.code -eq 'INVALID_ARGUMENT') 'Object was accepted as integer'
        $exitCmd=if($platform -eq 'linux'){'printf "nonzero\n"; exit 7'}else{'Write-Output nonzero; exit 7'}
        $r=Finish A08 $platform (Call A08 "${platform}_exec_command" @{cmd=$exitCmd})
        Assert ($r.exit_code -eq 7 -and $r.output.Contains('nonzero')) 'Nonzero command treated incorrectly'
        $outputCmd=if($platform -eq 'linux'){'printf "%010000d\n" 0; sleep 2; printf "tail\n"'}else{'Write-Output ("x" * 10000); Start-Sleep -Seconds 2; Write-Output tail'}
        $first=Call A08 "${platform}_exec_command" @{cmd=$outputCmd;yield_time_ms=1000;max_output_tokens=20}
        Assert ($first.output_truncated -and $first.omitted_bytes -gt 0) 'Missing output truncation metadata'
        Assert ([bool]$first['session_id']) 'Expected continuation after initial output budget'
        $r=Finish A08 $platform (Call A08 "${platform}_write_stdin" @{session_id=$first.session_id;chars='';yield_time_ms=3000})
        Assert ($r.output.Trim() -eq 'tail' -and $r.exit_code -eq 0) 'Omitted output replayed or tail missing'
        $badDir=if($platform -eq 'linux'){'/loom-beta-nonexistent-directory'}else{'Z:\loom-beta-nonexistent-directory'}
        $bad=Call A08 "${platform}_exec_command" @{cmd=$cmd;workdir=$badDir} -AllowError
        Assert ($bad.error.code -eq 'WORKDIR_UNAVAILABLE') 'Bad workdir did not fail before execution'
        Record A08 "${platform}_PASS" $r
    }
    Write-Output "PASS command scenarios A01/A02/A03/A05/A06 and parameter/output portions of A07/A08. Evidence: $report"
} catch {
    Record FAILURE exception @{message=$_.Exception.Message;position=$_.InvocationInfo.PositionMessage}
    throw
} finally { $connection.Client.Dispose() }
