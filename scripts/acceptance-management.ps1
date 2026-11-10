param(
    [string]$DeploymentRoot = (Join-Path $PSScriptRoot '..\.local'),
    [string]$BaseUrl = 'https://121.199.175.151:25100',
    [string]$Remote = 'admin@121.199.175.151',
    [string]$RemoteDirectory = '/home/admin/loom-oi-beta',
    # Run only after all other execution tests have completed. This restarts OIS.
    [switch]$RestartOIS
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Import-Module (Join-Path $PSScriptRoot 'McpClient.psm1') -Force
$DeploymentRoot = [IO.Path]::GetFullPath($DeploymentRoot)
$BaseUrl = $BaseUrl.TrimEnd('/')
if ($BaseUrl -notmatch '^https://') { throw 'HTTPS is required for management acceptance.' }
if ($RemoteDirectory -notmatch '^/home/[A-Za-z0-9_-]+/[A-Za-z0-9_-]+$') { throw 'Use the isolated beta deployment directory.' }
$secretsPath = Join-Path $DeploymentRoot 'credentials.json'
$secrets = Get-Content -LiteralPath $secretsPath -Raw | ConvertFrom-Json -AsHashtable
$ca = Join-Path $DeploymentRoot 'tls/ca.crt'
$client = New-LoomHttpClient $ca
$reports = Join-Path $DeploymentRoot 'reports'
New-Item -ItemType Directory -Force $reports | Out-Null
$report = Join-Path $reports "management-$([DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss')).jsonl"
$id = 'management_' + [Guid]::NewGuid().ToString('N')
$resources = @{ vmcps = @(); tools = @(); channels = @() }
function Record([string]$Case, [string]$Step, $Evidence) {
    @{ timestamp = [DateTime]::UtcNow.ToString('o'); case = $Case; step = $Step; evidence = $Evidence } |
        ConvertTo-Json -Depth 64 -Compress | Add-Content -LiteralPath $report -Encoding utf8NoBOM
}
function Assert([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Api([string]$Method, [string]$Path, $Body = $null, [int]$Status = 200, [string]$Code = '') {
    $result = Invoke-LoomHttp -Client $client -Url "$BaseUrl/api/v1/$Path" -Method $Method -Token $secrets.OIS_ADMIN_TOKEN -Body $Body
    Assert ($result.Status -eq $Status) "$Method $Path returned $($result.Status): $($result.Raw)"
    if ($Code) { Assert ($result.Body.error.code -eq $Code) "$Method $Path returned the wrong error code: $($result.Raw)" }
    foreach ($secret in $secrets.Values) { Assert (-not $result.Raw.Contains([string]$secret)) 'An API response exposed a credential value.' }
    Record A12 "$Method $Path" @{ status=$result.Status; body=$result.Body }
    return $result
}
function Wait-Ready {
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    do {
        try {
            $r = Invoke-LoomHttp -Client $client -Url "$BaseUrl/api/v1/status" -Token $secrets.OIS_ADMIN_TOKEN
            if ($r.Status -eq 200 -and $r.Body.tools.available -ge 4 -and $r.Body.hosts.online -ge 2) { return $r }
        } catch { }
        Start-Sleep -Milliseconds 300
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'Both original channels did not become ready after OIS restart.'
}
try {
    foreach ($token in @($secrets.WORKBENCH_TOKEN, $secrets.LINUX_CHANNEL_TOKEN, $secrets.WINDOWS_CHANNEL_TOKEN)) {
        $r = Invoke-LoomHttp -Client $client -Url "$BaseUrl/api/v1/status" -Token $token
        Assert ($r.Status -eq 401) 'Non-management credentials accessed management APIs.'
    }
    $paths = @('status', 'channels', 'tools', 'vmcps', 'vmcps/workbench', 'hosts', 'actions')
    foreach ($binding in @(
        @{host='linux-151';app='bash';prefix='linux'},
        @{host='windows-local';app='powershell';prefix='windows'}
    )) {
        $ch = $binding.host; $app = $binding.app; $prefix = $binding.prefix
        $paths += @("channels/$ch", "channels/$ch/apps", "hosts/$ch", "hosts/$ch/apps", "hosts/$ch/actions",
            "channels/$ch/apps/$app/actions", "actions?host_id=$ch&channel=$ch&app=$app")
        foreach ($action in @('exec_command','write_stdin')) {
            $paths += "tools/${prefix}_$action", "channels/$ch/apps/$app/actions/$action"
        }
    }
    foreach ($path in $paths) { $null = Api GET $path }
    $null = Api PUT hosts/cannot-create @{} 404 NOT_FOUND
    $null = Api PUT actions/cannot-create @{} 404 NOT_FOUND

    $channel = @{description='temporary management acceptance';expected_host_id=$id;credential_env='LINUX_CHANNEL_TOKEN'}
    $resources.channels += $id
    $null = Api PUT "channels/$id" $channel 201
    $channel.description = 'API update must survive restart'
    $null = Api PUT "channels/$id" $channel 200
    $stored = Api GET "channels/$id"
    Assert ($stored.Body.description -eq $channel.description) 'Channel replacement did not persist the new description.'
    $tool = @{description='temporary command tool';input_schema=@{type='object'};source=@{channel=$id;app='probe';action='echo'}}
    $resources.tools += $id
    $null = Api PUT "tools/$id" $tool 201
    $tool.description = 'updated tool'
    $null = Api PUT "tools/$id" $tool 200
    $null = Api GET "tools/$id"
    $vmcp = @{tools=@($id);access=@{credential_env='WORKBENCH_TOKEN'}}
    $resources.vmcps += $id
    $null = Api PUT "vmcps/$id" $vmcp 201
    $null = Api PUT "vmcps/$id" $vmcp 200
    $null = Api GET "vmcps/$id"
    $null = Api DELETE "channels/$id" $null 409 RESOURCE_IN_USE
    $null = Api DELETE "tools/$id" $null 409 RESOURCE_IN_USE
    $changed = $tool | ConvertTo-Json -Depth 20 | ConvertFrom-Json -AsHashtable
    $changed.source.channel = 'linux-151'
    $null = Api PUT "tools/$id" $changed 409 BINDING_IMMUTABLE
    $invalid = @{tools=@('does-not-exist');access=@{credential_env='WORKBENCH_TOKEN'}}
    $null = Api PUT "vmcps/$id" $invalid 400 INVALID_REQUEST
    $stillBound = Api GET "vmcps/$id"
    Assert ($stillBound.Body.tools.Count -eq 1 -and $stillBound.Body.tools[0] -eq $id) 'Failed update changed persisted configuration.'
    $null = Api DELETE "vmcps/$id" $null 204
    $null = Api GET "vmcps/$id" $null 404 NOT_FOUND
    $null = Api DELETE "tools/$id" $null 204
    $null = Api GET "tools/$id" $null 404 NOT_FOUND

    # This executable creates its own private channel/App/Tool/vMCP. It does not
    # disconnect or modify linux-151/windows-local/workbench.
    $probeDir = Join-Path $DeploymentRoot 'probes'
    New-Item -ItemType Directory -Force $probeDir | Out-Null
    $probe = Join-Path $probeDir 'channelprobe.exe'
    Push-Location (Split-Path $PSScriptRoot -Parent)
    try {
        & go build -o $probe ./scripts/channelprobe
        Assert ($LASTEXITCODE -eq 0) 'Channel probe build failed.'
    } finally { Pop-Location }
    $probeOutput = & $probe -url $BaseUrl -ca $ca -credentials $secretsPath
    Assert ($LASTEXITCODE -eq 0) 'Authenticated channel probe failed.'
    $probeEvidence = ($probeOutput -join "`n") | ConvertFrom-Json -AsHashtable -Depth 64
    Assert ($probeEvidence.status -eq 'PASS') 'Channel probe did not report success.'
    Record A13 PASS $probeEvidence
    foreach ($kind in @('channels','tools','vmcps')) { $null = Api GET "$kind/$($probeEvidence.probe)" $null 404 NOT_FOUND }

    if ($RestartOIS) {
        # The API-created marker is absent from the original bootstrap YAML.
        # Only OIS is restarted; OIC workers continue and reconnect themselves.
        $sshOptions = @('-o','BatchMode=yes','-o','ConnectTimeout=10','-o','StrictHostKeyChecking=yes')
        $command = "cd '$RemoteDirectory' && set -a && . ./secrets.env && set +a && bin/ois --config ois.yaml stop && bin/ois --config ois.yaml start"
        & ssh @sshOptions $Remote $command
        Assert ($LASTEXITCODE -eq 0) 'Isolated beta OIS restart failed.'
        $ready = Wait-Ready
        $persisted = Api GET "channels/$id"
        Assert ($persisted.Body.description -eq $channel.description -and $persisted.Body.expected_host_id -eq $id) 'OIS restart replaced API state with bootstrap YAML.'
        Record A12 restart_persistence_PASS @{channel=$persisted.Body;status=$ready.Body}
        Record A12 PASS @{management_endpoints=$paths.Count;probe=$probeEvidence.probe;restart_verified=$true}
        Write-Output "PASS A12 and A13. Evidence: $report"
    } else {
        Record A12 PENDING_restart @{management_endpoints=$paths.Count;reason='Run with -RestartOIS after long-running tests finish.'}
        Write-Output "PASS management CRUD, in-flight configuration and A13. A12 restart persistence remains pending; rerun with -RestartOIS after other tests finish. Evidence: $report"
    }
} catch {
    Record acceptance FAIL @{message=$_.Exception.Message}
    throw
} finally {
    foreach ($kind in @('vmcps','tools','channels')) {
        foreach ($name in $resources[$kind]) {
            try {
                $r = Invoke-LoomHttp -Client $client -Url "$BaseUrl/api/v1/$kind/$name" -Method DELETE -Token $secrets.OIS_ADMIN_TOKEN
                if ($r.Status -notin @(204,404)) { Write-Warning "Cleanup $kind/$name returned $($r.Status)." }
            } catch { Write-Warning "Cleanup $kind/$name failed: $($_.Exception.Message)" }
        }
    }
    $client.Dispose()
}
