param([string]$DeploymentRoot=(Join-Path $PSScriptRoot '..\.local'))
# Reconfigures only the agreed beta deployment, tests optional access modes,
# and always restores the default HTTP/anonymous deployment afterwards.
$ErrorActionPreference='Stop'
Import-Module (Join-Path $PSScriptRoot 'McpClient.psm1') -Force
$DeploymentRoot=[IO.Path]::GetFullPath($DeploymentRoot)
$reports=Join-Path $DeploymentRoot 'reports';New-Item -ItemType Directory -Force $reports|Out-Null
$report=Join-Path $reports "access-modes-$([DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss')).jsonl"
function Assert([bool]$Value,[string]$Message){if(-not $Value){throw $Message}}
function Record([string]$Mode,$Evidence){@{timestamp=[DateTime]::UtcNow.ToString('o');mode=$Mode;evidence=$Evidence}|ConvertTo-Json -Depth 40 -Compress|Add-Content -LiteralPath $report -Encoding utf8NoBOM}
function Verify-Mode([bool]$TLS,[bool]$Token){
    $base=if($TLS){'https://121.199.175.151:25100'}else{'http://121.199.175.151:25100'}
    $ca=if($TLS){Join-Path $DeploymentRoot 'tls/ca.crt'}else{''}
    $credentials=Get-LoomCredentials $DeploymentRoot -UseCredentials:$Token
    $http=New-LoomHttpClient $ca;$connection=$null
    try{
        $anonymous=Invoke-LoomHttp $http "$base/api/v1/status"
        Assert ($anonymous.Status -eq $(if($Token){401}else{200})) 'Unexpected management authentication policy'
        $init=@{jsonrpc='2.0';id=1;method='initialize';params=@{protocolVersion='2025-11-25';capabilities=@{};clientInfo=@{name='access-probe';version='1'}}}
        $unauthed=Invoke-LoomHttp $http "$base/mcp/workbench" -Method POST -Body $init -Headers @{Accept='application/json, text/event-stream'}
        Assert ($unauthed.Status -eq $(if($Token){401}else{200})) 'Unexpected MCP authentication policy'
        $adminToken=$credentials.OIS_ADMIN_TOKEN
        $ready=$null
        for($i=0;$i -lt 40;$i++){
            $ready=Invoke-LoomHttp $http "$base/api/v1/status" -Token $adminToken
            if($ready.Status -eq 200 -and $ready.Body.tools.available -eq 4){break}
            Start-Sleep -Milliseconds 250
        }
        Assert ($ready.Status -eq 200 -and $ready.Body.tools.available -eq 4) 'Four tools are not ready'
        $vm=Invoke-LoomHttp $http "$base/api/v1/vmcps/workbench" -Token $adminToken
        Assert ($vm.Body.endpoint -eq "$base/mcp/workbench") 'vMCP returned the wrong endpoint'
        Assert ([bool]$vm.Body.access.require_https -eq $TLS) 'Persisted HTTPS policy differs from deployment'
        $connection=Connect-LoomMcp "$base/mcp/workbench" $credentials.WORKBENCH_TOKEN $ca
        $results=@{}
        foreach($platform in @('linux','windows')){
            $command=if($platform -eq 'linux'){'printf "access-mode-ok\n"'}else{'Write-Output access-mode-ok'}
            $r=Invoke-LoomTool $connection "${platform}_exec_command" @{cmd=$command}
            Assert ($r.exit_code -eq 0 -and $r.output.Trim() -eq 'access-mode-ok') 'Command did not traverse the full chain'
            $results[$platform]=$r
        }
        if($TLS){
            $system=New-LoomHttpClient
            try{$rejected=$false;try{[void](Invoke-LoomHttp $system "$base/healthz")}catch{$rejected=$true};Assert $rejected 'Private deployment CA was accepted without explicit trust'}finally{$system.Dispose()}
        }
        Record "$base token=$Token PASS" @{anonymous_management_status=$anonymous.Status;anonymous_mcp_status=$unauthed.Status;vmcp=$vm.Body;topology=$ready.Body;commands=$results}
    }finally{if($connection){$connection.Client.Dispose()};$http.Dispose()}
}
try{
    Verify-Mode $false $false
    foreach($profile in @(@{RequireToken=$true},@{EnableHttps=$true},@{EnableHttps=$true;RequireToken=$true})){
        & (Join-Path $PSScriptRoot 'deploy.ps1') -DeploymentRoot $DeploymentRoot -SkipBuild @profile
        Verify-Mode ([bool]$profile['EnableHttps']) ([bool]$profile['RequireToken'])
    }
}catch{Record FAIL @{message=$_.Exception.Message};throw}
finally{
    & (Join-Path $PSScriptRoot 'deploy.ps1') -DeploymentRoot $DeploymentRoot -SkipBuild
    Verify-Mode $false $false
}
Write-Output "PASS four access modes and HTTPS-to-HTTP migration. Restored anonymous HTTP. Evidence: $report"
