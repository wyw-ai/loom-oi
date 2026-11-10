param(
    [string]$DeploymentRoot = (Join-Path $PSScriptRoot '..\.local'),
    [string]$Remote = 'admin@121.199.175.151',
    [string]$RemoteDirectory = '/home/admin/loom-oi-beta',
    [switch]$SkipBuild,
    [switch]$RequireToken,
    [switch]$EnableHttps,
    [string]$CertFile,
    [string]$KeyFile,
    [string]$CAFile
)
$ErrorActionPreference = 'Stop'
Import-Module (Join-Path $PSScriptRoot 'McpClient.psm1') -Force
if ([bool]$CertFile -ne [bool]$KeyFile -or (($CertFile -or $CAFile) -and -not $EnableHttps)) { throw 'Supply CertFile and KeyFile together with EnableHttps; CAFile is HTTPS-only.' }
$root = Split-Path $PSScriptRoot -Parent
$DeploymentRoot = [System.IO.Path]::GetFullPath($DeploymentRoot)
# This script's remote shell snippets accept only this limited path alphabet.
if ($RemoteDirectory -notmatch '^/home/[A-Za-z0-9_-]+/[A-Za-z0-9_-]+$') { throw 'Use a dedicated directory immediately under the SSH user home.' }
function Check-Exit([string]$Operation) { if ($LASTEXITCODE -ne 0) { throw "$Operation failed (exit $LASTEXITCODE)" } }
$sshOptions = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
if (-not $SkipBuild) { & (Join-Path $PSScriptRoot 'build.ps1') }
# Check the complete deployment input before changing an existing service.
foreach ($artifact in @(
    'linux-amd64/ois', 'linux-amd64/oic', 'windows-amd64/oic.exe',
    'apps/bash/app.yaml', 'apps/bash/runtime/oiapp-bash',
    'apps/powershell/app.yaml', 'apps/powershell/runtime/oiapp-powershell.exe'
)) {
    if (-not (Test-Path -LiteralPath (Join-Path $root "dist/$artifact") -PathType Leaf)) {
        throw "Missing deployment artifact: dist/$artifact. Run scripts/build.ps1 first."
    }
}
New-Item -ItemType Directory -Force $DeploymentRoot | Out-Null
$secretPath = Join-Path $DeploymentRoot 'credentials.json'
if ($RequireToken -and -not (Test-Path -LiteralPath $secretPath)) {
    $credentials = @{}
    foreach ($name in @('OIS_ADMIN_TOKEN', 'WORKBENCH_TOKEN', 'LINUX_CHANNEL_TOKEN', 'WINDOWS_CHANNEL_TOKEN')) {
        $credentials[$name] = [Convert]::ToHexString([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32)).ToLowerInvariant()
    }
    $credentials | ConvertTo-Json | Set-Content -LiteralPath $secretPath -Encoding utf8NoBOM
}
$credentials = if(Test-Path -LiteralPath $secretPath){Get-Content -LiteralPath $secretPath -Raw | ConvertFrom-Json -AsHashtable}else{@{}}
$tls = Join-Path $DeploymentRoot 'tls'
if ($EnableHttps -and -not $CertFile -and -not (Test-Path -LiteralPath (Join-Path $tls 'ca.crt'))) {
    Push-Location $root
    try { & go run ./scripts/devcert -out $tls; Check-Exit 'Certificate generation' } finally { Pop-Location }
}
if($EnableHttps -and -not $CertFile){$CertFile=Join-Path $tls 'server.crt';$KeyFile=Join-Path $tls 'server.key';$CAFile=Join-Path $tls 'ca.crt'}
$baseUrl=if($EnableHttps){'https://121.199.175.151:25100'}else{'http://121.199.175.151:25100'}
$linux = Join-Path $DeploymentRoot 'linux'
$windows = Join-Path $DeploymentRoot 'windows'
$previousBaseUrl=''
if(Test-Path -LiteralPath (Join-Path $linux 'ois.yaml')){
    $previousConfig=Get-Content -LiteralPath (Join-Path $linux 'ois.yaml') -Raw
    if($previousConfig -match '(?m)^  public_url:\s*(\S+)'){$previousBaseUrl=$Matches[1]}
}
$previousCA=''
if($previousBaseUrl -like 'https:*'){
    if(Test-Path -LiteralPath (Join-Path $DeploymentRoot 'deployment.json')){$previousCA=(Get-Content (Join-Path $DeploymentRoot 'deployment.json') -Raw|ConvertFrom-Json).ca_file}
    elseif(Test-Path -LiteralPath (Join-Path $tls 'ca.crt')){$previousCA=Join-Path $tls 'ca.crt'}
}
foreach ($dir in @($linux, $windows)) {
    foreach ($sub in @('bin', 'tls', 'workspace', 'state')) { New-Item -ItemType Directory -Force (Join-Path $dir $sub) | Out-Null }
    if($EnableHttps -and $CAFile){Copy-Item -LiteralPath $CAFile -Destination (Join-Path $dir 'tls/ca.crt')}
}
Copy-Item -LiteralPath (Join-Path $root 'examples/ois.yaml') -Destination (Join-Path $linux 'ois.yaml')
Copy-Item -LiteralPath (Join-Path $root 'examples/oic.linux.yaml') -Destination (Join-Path $linux 'oic.yaml')
if($EnableHttps){Copy-Item -LiteralPath $CertFile -Destination (Join-Path $linux 'tls/server.crt');Copy-Item -LiteralPath $KeyFile -Destination (Join-Path $linux 'tls/server.key')}
$serverConfig=(Get-Content -LiteralPath (Join-Path $linux 'ois.yaml') -Raw).Replace('http://121.199.175.151:25100',$baseUrl)
if($EnableHttps){$serverConfig=$serverConfig.Replace("  listen: ':25100'","  listen: ':25100'`n  tls_cert: ./tls/server.crt`n  tls_key: ./tls/server.key")}
if($RequireToken){
    $serverConfig=[regex]::Replace($serverConfig,'(?m)^# admin:\r?\n#   token_env: OIS_ADMIN_TOKEN',"admin:`n  token_env: OIS_ADMIN_TOKEN")
    $serverConfig=$serverConfig.Replace('    # credential_env: LINUX_CHANNEL_TOKEN','    credential_env: LINUX_CHANNEL_TOKEN').Replace('    # credential_env: WINDOWS_CHANNEL_TOKEN','    credential_env: WINDOWS_CHANNEL_TOKEN')
}
$serverConfig+="`n    access:`n      require_https: $($EnableHttps.IsPresent.ToString().ToLowerInvariant())`n"
if($RequireToken){$serverConfig+="      credential_env: WORKBENCH_TOKEN`n"}
[IO.File]::WriteAllText((Join-Path $linux 'ois.yaml'),$serverConfig)
$linuxConfig = (Get-Content -LiteralPath (Join-Path $linux 'oic.yaml') -Raw).Replace('/home/admin/loom-oi-beta', $RemoteDirectory)
$linuxConfig=$linuxConfig.Replace('http://121.199.175.151:25100',$baseUrl)
if($RequireToken){$linuxConfig=$linuxConfig.Replace('  # credential_env:','  credential_env:')}
if($EnableHttps -and $CAFile){$linuxConfig=$linuxConfig.Replace('  # ca_file:','  ca_file:')}
[IO.File]::WriteAllText((Join-Path $linux 'oic.yaml'), $linuxConfig)
foreach ($name in @('ois', 'oic')) { Copy-Item -LiteralPath (Join-Path $root "dist/linux-amd64/$name") -Destination (Join-Path $linux "bin/$name") }
$appLinux = Join-Path $linux 'apps/bash'
New-Item -ItemType Directory -Force (Join-Path $appLinux 'runtime') | Out-Null
Copy-Item -LiteralPath (Join-Path $root 'dist/apps/bash/app.yaml') -Destination (Join-Path $appLinux 'app.yaml')
Copy-Item -LiteralPath (Join-Path $root 'dist/apps/bash/runtime/oiapp-bash') -Destination (Join-Path $appLinux 'runtime/oiapp-bash')
# Remove only the obsolete shared worker files from this deployment's staging.
foreach ($obsolete in @((Join-Path $linux 'bin/oiapp-terminal'), (Join-Path $appLinux 'runtime/oiapp-terminal'))) {
    if (Test-Path -LiteralPath $obsolete -PathType Leaf) { Remove-Item -LiteralPath $obsolete -Force }
}
$envText = ($credentials.GetEnumerator() | Sort-Object Name | ForEach-Object {
    if($_.Name -notmatch '^[A-Z_]+$' -or [string]$_.Value -match "['`r`n]"){throw 'Unexpected credential environment encoding'}
    "$($_.Name)='$($_.Value)'"
}) -join "`n"
[IO.File]::WriteAllText((Join-Path $linux 'secrets.env'), $envText + "`n")

# Only beta's own service control endpoints are stopped. Existing OIS API state
# and OIC load records remain in state/ across subsequent deployments.
if(-not $EnableHttps -and ($previousBaseUrl -like 'https:*' -or $previousConfig -match '(?m)^  https:')){
    # A persisted HTTPS-only vMCP cannot load on an HTTP-only server. Relax this
    # deployment-owned policy through the old live API before replacing TLS.
    $oldHttp=New-LoomHttpClient -CAFile $previousCA
    try{
        $all=Invoke-LoomHttp $oldHttp "$previousBaseUrl/api/v1/vmcps" -Token $credentials['OIS_ADMIN_TOKEN']
        if($all.Status -ne 200){throw 'Cannot inspect existing HTTPS policies before migration'}
        foreach($vm in $all.Body.items){if($vm.id -ne 'workbench' -and $vm.access['require_https']){throw "vMCP $($vm.id) still requires HTTPS; retain its HTTPS listener before switching this deployment."}}
        $r=Invoke-LoomHttp $oldHttp "$previousBaseUrl/api/v1/vmcps/workbench" -Token $credentials['OIS_ADMIN_TOKEN']
        if($r.Status -ne 200){throw "Read previous workbench failed: $($r.Raw)"}
        if($r.Body.access['require_https']){
            $r.Body.access.require_https=$false
            $updated=Invoke-LoomHttp $oldHttp "$previousBaseUrl/api/v1/vmcps/workbench" -Method PUT -Token $credentials['OIS_ADMIN_TOKEN'] -Body @{tools=$r.Body.tools;access=$r.Body.access}
            if($updated.Status -ne 200){throw "Previous HTTPS policy update failed: $($updated.Raw)"}
        }
    }finally{$oldHttp.Dispose()}
}
$localOic = Join-Path $windows 'bin/oic.exe'
$localConfig = Join-Path $windows 'oic.yaml'
if((Test-Path $localOic) -and (Test-Path $localConfig)){
    $previousStatus=& $localOic --config $localConfig status | ConvertFrom-Json
    if($previousStatus.running){& $localOic --config $localConfig stop;Check-Exit 'Windows OIC stop';$previousProcess=Get-Process -Id $previousStatus.pid -ErrorAction SilentlyContinue;if($previousProcess){$previousProcess|Wait-Process -Timeout 15}}
}
$remoteStop = "if [ -d '$RemoteDirectory' ]; then cd '$RemoteDirectory' && { if [ -f secrets.env ]; then set -a; . ./secrets.env; set +a; fi; if [ -x bin/oic ]; then bin/oic --config oic.yaml stop || true; fi; if [ -x bin/ois ]; then bin/ois --config ois.yaml stop || true; fi; rm -f -- bin/oiapp-terminal apps/bash/runtime/oiapp-terminal; }; fi; mkdir -p '$RemoteDirectory'; chmod 700 '$RemoteDirectory'"
& ssh @sshOptions $Remote $remoteStop
Check-Exit 'Remote preparation'
& scp @sshOptions -r (Join-Path $linux 'bin') (Join-Path $linux 'apps') (Join-Path $linux 'tls') (Join-Path $linux 'ois.yaml') (Join-Path $linux 'oic.yaml') (Join-Path $linux 'secrets.env') "${Remote}:$RemoteDirectory/"
Check-Exit 'Linux upload'
$remoteStart = "cd '$RemoteDirectory' && mkdir -p workspace && chmod 600 secrets.env && { if [ -f tls/server.key ]; then chmod 600 tls/server.key; fi; } && chmod +x bin/* apps/bash/runtime/* && set -a && . ./secrets.env && set +a && bin/ois --config ois.yaml start"
& ssh @sshOptions $Remote $remoteStart
Check-Exit 'Linux startup'

# Change only deployment-owned authentication policies through the authoritative
# API, with both OICs stopped. Existing Tool bindings and other data are retained.
$http=New-LoomHttpClient -CAFile $CAFile
try{
    $adminToken=if($RequireToken){$credentials.OIS_ADMIN_TOKEN}else{''}
    foreach($channel in @('linux-151','windows-local')){
        $r=Invoke-LoomHttp $http "$baseUrl/api/v1/channels/$channel" -Token $adminToken
        if($r.Status -ne 200){throw "Read channel failed: $($r.Raw)"}
        $body=@{description=$r.Body.description;expected_host_id=$r.Body.expected_host_id}
        if($RequireToken){$body.credential_env=if($channel -eq 'linux-151'){'LINUX_CHANNEL_TOKEN'}else{'WINDOWS_CHANNEL_TOKEN'}}
        $r=Invoke-LoomHttp $http "$baseUrl/api/v1/channels/$channel" -Method PUT -Token $adminToken -Body $body
        if($r.Status -ne 200){throw "Update channel failed: $($r.Raw)"}
    }
    $r=Invoke-LoomHttp $http "$baseUrl/api/v1/vmcps/workbench" -Token $adminToken
    if($r.Status -ne 200){throw "Read workbench failed: $($r.Raw)"}
    $access=@{require_https=$EnableHttps.IsPresent};if($RequireToken){$access.credential_env='WORKBENCH_TOKEN'}
    $r=Invoke-LoomHttp $http "$baseUrl/api/v1/vmcps/workbench" -Method PUT -Token $adminToken -Body @{tools=$r.Body.tools;access=$access}
    if($r.Status -ne 200){throw "Update workbench failed: $($r.Raw)"}
    $otherCredentialReferences=$false
    foreach($kind in @('channels','vmcps')){
        $all=Invoke-LoomHttp $http "$baseUrl/api/v1/$kind" -Token $adminToken
        if($all.Status -ne 200){throw 'Cannot inspect remaining credential references'}
        foreach($item in $all.Body.items){$reference=if($kind -eq 'channels'){$item['credential_env']}else{$item.access['credential_env']};if($reference){$otherCredentialReferences=$true}}
    }
}finally{$http.Dispose()}
if(-not $RequireToken -and -not $otherCredentialReferences){
    [IO.File]::WriteAllText((Join-Path $linux 'secrets.env'),"# No Token is required by this deployment.`n")
    & scp @sshOptions (Join-Path $linux 'secrets.env') "${Remote}:$RemoteDirectory/secrets.env";Check-Exit 'Clear inactive environment'
    & ssh @sshOptions $Remote "cd '$RemoteDirectory' && chmod 600 secrets.env && bin/ois --config ois.yaml stop && bin/ois --config ois.yaml start";Check-Exit 'OIS start without token environment'
}
& ssh @sshOptions $Remote "cd '$RemoteDirectory' && set -a && . ./secrets.env && set +a && bin/oic --config oic.yaml start && bin/oic --config oic.yaml load apps/bash/app.yaml"
Check-Exit 'Linux OIC startup'

$localOic = Join-Path $windows 'bin/oic.exe'
$localConfig = Join-Path $windows 'oic.yaml'
if ((Test-Path -LiteralPath $localOic) -and (Test-Path -LiteralPath $localConfig)) {
    $previousStatus = & $localOic --config $localConfig status | ConvertFrom-Json
    if ($previousStatus.running) {
        & $localOic --config $localConfig stop; Check-Exit 'Windows OIC stop'
        $previousProcess = Get-Process -Id $previousStatus.pid -ErrorAction SilentlyContinue
        if ($previousProcess) { $previousProcess | Wait-Process -Timeout 15 -ErrorAction Stop }
    }
}
Copy-Item -LiteralPath (Join-Path $root 'dist/windows-amd64/oic.exe') -Destination $localOic
$appWindows = Join-Path $windows 'apps/powershell'
New-Item -ItemType Directory -Force (Join-Path $appWindows 'runtime') | Out-Null
Copy-Item -LiteralPath (Join-Path $root 'dist/apps/powershell/runtime/oiapp-powershell.exe') -Destination (Join-Path $appWindows 'runtime/oiapp-powershell.exe')
Copy-Item -LiteralPath (Join-Path $root 'dist/apps/powershell/app.yaml') -Destination (Join-Path $appWindows 'app.yaml')
$obsoleteWorker = Join-Path $appWindows 'runtime/oiapp-terminal.exe'
if (Test-Path -LiteralPath $obsoleteWorker -PathType Leaf) { Remove-Item -LiteralPath $obsoleteWorker -Force }
$template = Get-Content -LiteralPath (Join-Path $root 'examples/oic.windows.yaml') -Raw
$template=$template.Replace('http://121.199.175.151:25100',$baseUrl)
if($RequireToken){$template=$template.Replace('  # credential_env:','  credential_env:')}
if($EnableHttps -and $CAFile){$template=$template.Replace('  # ca_file:','  ca_file:')}
$actualCwd = (Join-Path $windows 'workspace').Replace("'", "''")
$template = [regex]::Replace($template, "(?m)^    default_cwd:.*$", "    default_cwd: '$actualCwd'")
[IO.File]::WriteAllText($localConfig, $template)
$oldToken = $env:WINDOWS_CHANNEL_TOKEN
try {
    $env:WINDOWS_CHANNEL_TOKEN = if($RequireToken){$credentials.WINDOWS_CHANNEL_TOKEN}else{$null}
    & $localOic --config $localConfig start; Check-Exit 'Windows OIC start'
    & $localOic --config $localConfig load (Join-Path $appWindows 'app.yaml'); Check-Exit 'Windows App load'
} finally { $env:WINDOWS_CHANNEL_TOKEN = $oldToken }
@{base_url=$baseUrl;endpoint="$baseUrl/mcp/workbench";https=$EnableHttps.IsPresent;require_token=$RequireToken.IsPresent;ca_file=$CAFile}|ConvertTo-Json|Set-Content (Join-Path $DeploymentRoot 'deployment.json') -Encoding utf8NoBOM
Write-Output "Deployed $baseUrl/mcp/workbench (HTTPS=$($EnableHttps.IsPresent), Token=$($RequireToken.IsPresent))."
