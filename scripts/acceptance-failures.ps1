param(
    [string]$DeploymentRoot = (Join-Path $PSScriptRoot '..\.local'),
    [string]$Endpoint = 'http://121.199.175.151:25100/mcp/workbench',
    [switch]$UseCredentials,
    [string]$Token,
    [string]$AdminToken,
    [string]$CAFile,
    [switch]$SkipDisconnection
)
$ErrorActionPreference='Stop'
Import-Module (Join-Path $PSScriptRoot 'McpClient.psm1') -Force
$DeploymentRoot=[IO.Path]::GetFullPath($DeploymentRoot)
$secrets=Get-LoomCredentials $DeploymentRoot -UseCredentials:$UseCredentials
if($PSBoundParameters.ContainsKey('Token')){$secrets.WORKBENCH_TOKEN=$Token}
if($PSBoundParameters.ContainsKey('AdminToken')){$secrets.OIS_ADMIN_TOKEN=$AdminToken}
$reports=Join-Path $DeploymentRoot 'reports';New-Item -ItemType Directory -Force $reports | Out-Null
$report=Join-Path $reports "failures-$([DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss')).jsonl"
$base=$Endpoint.Substring(0,$Endpoint.IndexOf('/mcp/'))
function Record([string]$Case,[string]$Step,$Evidence){@{timestamp=[DateTime]::UtcNow.ToString('o');case=$Case;step=$Step;evidence=$Evidence}|ConvertTo-Json -Depth 64 -Compress|Add-Content -LiteralPath $report -Encoding utf8NoBOM}
function Assert([bool]$Value,[string]$Message){if(-not $Value){throw $Message}}
function Remote-Service([string]$Command){
    & ssh -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=yes admin@121.199.175.151 "cd /home/admin/loom-oi-beta && { if [ -f secrets.env ]; then set -a; . ./secrets.env; set +a; fi; } && bin/ois --config ois.yaml $Command"
    if($LASTEXITCODE -ne 0){throw "Remote OIS $Command failed"}
}
function Wait-Ready($Connection){
    for($i=0;$i -lt 40;$i++){
        $s=Invoke-LoomHttp $Connection.Client "$base/api/v1/status" -Token $secrets.OIS_ADMIN_TOKEN
        if($s.Status -eq 200 -and $s.Body.tools.available -eq 4){return}
        Start-Sleep -Milliseconds 250
    }
    throw 'Four tools did not become available'
}
$c=Connect-LoomMcp $Endpoint $secrets.WORKBENCH_TOKEN $CAFile
$oisStopped=$false;$oicStopped=$false
$oic=Join-Path $DeploymentRoot 'windows/bin/oic.exe';$config=Join-Path $DeploymentRoot 'windows/oic.yaml'
try{
    foreach($platform in @('linux','windows')){
        # Overflow the 1 MiB collector before any snapshot is claimed.
        $cmd=if($platform -eq 'linux'){'printf "HEAD_MARKER\n"; head -c 2097152 /dev/zero | tr "\000" x; printf "\nTAIL_MARKER\n"'}else{'Write-Output "HEAD_MARKER"; Write-Output ("x" * 2097152); Write-Output "TAIL_MARKER"'}
        $r=Invoke-LoomTool $c "${platform}_exec_command" @{cmd=$cmd;yield_time_ms=1000;max_output_tokens=20000}
        while($r['session_id']){$r=Invoke-LoomTool $c "${platform}_write_stdin" @{session_id=$r.session_id;chars='';yield_time_ms=1000;max_output_tokens=20000}}
        Assert ($r.exit_code -eq 0 -and $r.output_truncated -and $r.omitted_bytes -gt 1048576) 'Buffer overflow was not reported'
        Assert (-not $r.output.Contains('HEAD_MARKER') -and $r.output.Contains('TAIL_MARKER')) 'Buffer did not retain the expected tail'
        Record A08 "${platform}_buffer_overflow_PASS" $r

        $cmd=if($platform -eq 'linux'){'sleep 4; printf "survived\n"'}else{'Start-Sleep -Seconds 4; Write-Output survived'}
        $r=Invoke-LoomTool $c "${platform}_exec_command" @{cmd=$cmd;yield_time_ms=0}
        $session=$r.session_id
        $pending=Start-McpRequest $c tools/call @{name="${platform}_write_stdin";arguments=@{session_id=$session;chars='';yield_time_ms=30000}}
        Start-Sleep -Milliseconds 300
        $cancel=Invoke-McpRequest $c notifications/cancelled @{requestId=$pending.Id;reason='acceptance cancel one read'} -Notification
        $cancelled=Complete-LoomHttp $pending
        Assert ($cancel.Status -eq 202 -and $cancelled.Status -eq 204) 'MCP read cancellation was not acknowledged'
        $r=Invoke-LoomTool $c "${platform}_write_stdin" @{session_id=$session;chars='';yield_time_ms=5000}
        Assert ($r.exit_code -eq 0 -and $r.output.Contains('survived')) 'Cancelling a read killed the original execution'
        Record A09 "${platform}_cancel_read_PASS" @{session_id=$session;cancel_http=$cancelled.Status;result=$r}

        # Input is committed before waiting; cancellation must not claim rollback.
        $cmd=if($platform -eq 'linux'){'printf "input: "; read -r name; sleep 3; printf "received %s\n" "$name"'}else{'$name=Read-Host "input"; Start-Sleep -Seconds 3; Write-Output "received $name"'}
        $r=Invoke-LoomTool $c "${platform}_exec_command" @{cmd=$cmd;tty=$true;yield_time_ms=1000}
        $session=$r.session_id;$chars=if($platform -eq 'linux'){"committed`n"}else{"committed`r"}
        $pending=Start-McpRequest $c tools/call @{name="${platform}_write_stdin";arguments=@{session_id=$session;chars=$chars;yield_time_ms=30000}}
        Start-Sleep -Milliseconds 300
        [void](Invoke-McpRequest $c notifications/cancelled @{requestId=$pending.Id;reason='acceptance cancel after input'} -Notification)
        $cancelled=Complete-LoomHttp $pending
        Assert ($cancelled.Status -eq 204) 'Write cancellation not acknowledged'
        $r=Invoke-LoomTool $c "${platform}_write_stdin" @{session_id=$session;chars='';yield_time_ms=5000}
        Assert ($r.exit_code -eq 0 -and $r.output.Contains('received committed')) 'Previously written input was lost or original process was killed'
        Record A09 "${platform}_input_not_rolled_back_PASS" @{session_id=$session;result=$r}
    }
    if($SkipDisconnection){Write-Output "PASS A08 buffer and A09 cancellation portions. Evidence: $report";return}

    # A real OIC stop proves no offline business queue; restore its saved loads.
    & $oic --config $config stop;if($LASTEXITCODE -ne 0){throw 'OIC stop failed'};$oicStopped=$true
    $r=Invoke-LoomTool $c windows_exec_command @{cmd='Write-Output should-not-run'} -AllowError
    Assert ($r.error.code -eq 'CHANNEL_UNAVAILABLE') 'Offline call was not rejected before delivery'
    Record A09 offline_not_delivered_PASS $r
    $old=$env:WINDOWS_CHANNEL_TOKEN
    try{if($UseCredentials){$env:WINDOWS_CHANNEL_TOKEN=$secrets.WINDOWS_CHANNEL_TOKEN};& $oic --config $config start;if($LASTEXITCODE -ne 0){throw 'OIC start failed'}}finally{$env:WINDOWS_CHANNEL_TOKEN=$old}
    $oicStopped=$false;Wait-Ready $c

    # Stop only OIS after delivery. The OIC worker survives; the accepted command
    # writes exactly once, and the response must say outcome unknown.
    $marker='disconnect-'+[Guid]::NewGuid().ToString('N')+'.txt'
    $command="Add-Content -LiteralPath '$marker' -Value started; Start-Sleep -Seconds 4; Add-Content -LiteralPath '$marker' -Value done"
    $pending=Start-McpRequest $c tools/call @{name='windows_exec_command';arguments=@{cmd=$command;yield_time_ms=30000}}
    Start-Sleep -Milliseconds 1000
    Remote-Service stop;$oisStopped=$true
    $lost=Get-McpToolResult (Complete-LoomHttp $pending) -AllowError
    Assert ($lost.error.code -eq 'CALL_RESULT_UNKNOWN') 'Delivered disconnect did not report unknown outcome'
    Record A09 delivered_result_unknown_PASS $lost
    Remote-Service start;$oisStopped=$false
    $c.Client.Dispose();$c=Connect-LoomMcp $Endpoint $secrets.WORKBENCH_TOKEN $CAFile;Wait-Ready $c
    Start-Sleep -Seconds 4
    $r=Invoke-LoomTool $c windows_exec_command @{cmd="Get-Content -LiteralPath '$marker'"}
    $lines=@($r.output -split '\r?\n' | Where-Object {$_})
    Assert ($r.exit_code -eq 0 -and $lines.Count -eq 2 -and $lines[0] -eq 'started' -and $lines[1] -eq 'done') 'Disconnected command was repeated or failed to continue'
    Record A09 no_automatic_retry_PASS @{marker=$marker;result=$r}
    Write-Output "PASS A08 buffer and A09 disconnect/cancellation. Evidence: $report"
}catch{Record FAILURE exception @{message=$_.Exception.Message;position=$_.InvocationInfo.PositionMessage};throw}
finally{
    if($oisStopped){Remote-Service start}
    if($oicStopped){$old=$env:WINDOWS_CHANNEL_TOKEN;try{if($UseCredentials){$env:WINDOWS_CHANNEL_TOKEN=$secrets.WINDOWS_CHANNEL_TOKEN};& $oic --config $config start}finally{$env:WINDOWS_CHANNEL_TOKEN=$old}}
    $c.Client.Dispose()
}
