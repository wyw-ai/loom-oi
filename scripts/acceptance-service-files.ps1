# Runs the published Host A tools and saves the returned MCP image for visual verification.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$McpUrl,
    [string]$Token = $env:WORKBENCH_TOKEN,
    [string]$CAFile,
    [string]$OutputRoot = (Join-Path $PSScriptRoot '../.local/reports/service-files')
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
Import-Module (Join-Path $PSScriptRoot 'McpClient.psm1') -Force

$runId = [guid]::NewGuid().ToString('N')
$evidenceDir = [IO.Directory]::CreateDirectory((Join-Path $OutputRoot $runId)).FullName
$connection = Connect-LoomMcp -Url $McpUrl -Token $Token -CAFile $CAFile
try {
    $toolList = Invoke-McpRequest $connection 'tools/list'
    $members = @($toolList.Body.result.tools | ForEach-Object { $_.name })
    foreach ($expected in 'capture_a','put_from_a','lookup_on_a','list_store_a','get_on_a','view_on_a') {
        if ($expected -notin $members) { throw "Service member missing: $expected" }
    }

    $capture = Invoke-LoomTool $connection 'capture_a' @{}
    $storeDir = "shots/host-a/$runId"
    $storagePath = "$storeDir/desktop.png"
    $uploaded = Invoke-LoomTool $connection 'put_from_a' @{path=$capture.path; store_path=$storagePath}
    $ref = $uploaded.file_ref
    if ($uploaded.store_path -cne $storagePath -or !$ref.file_id -or !$ref.store -or $ref.size_bytes -le 0 -or $ref.sha256 -notmatch '^[a-fA-F0-9]{64}$') {
        throw 'Upload did not return a complete file reference'
    }
    $found = Invoke-LoomTool $connection 'lookup_on_a' @{store_path=$storagePath}
    foreach ($key in 'file_id','store','size_bytes','sha256') {
        if ($found.file_ref[$key] -cne $ref[$key]) { throw "Lookup returned different $key" }
    }
    $page = Invoke-LoomTool $connection 'list_store_a' @{store_dir=$storeDir; limit=10}
    if (@($page.entries | Where-Object { $_.kind -ceq 'file' -and $_.store_path -ceq $storagePath }).Count -ne 1) {
        throw 'Uploaded screenshot missing from the dedicated run directory'
    }

    $dest = "returned-$runId.png"
    $download = Invoke-LoomTool $connection 'get_on_a' @{file_ref=$ref; dest=$dest}
    if ($download.path -cne $dest -or $download.path -ceq $capture.path) { throw 'Invalid readback path' }
    $preview = Invoke-LoomTool $connection 'view_on_a' @{path=$download.path}
    $images = @($preview.content | Where-Object { $_.type -eq 'image' })
    if ($images.Count -ne 1) { throw 'Expected one native MCP image' }
    $extension = switch ($images[0].mimeType) {
        'image/png' { 'png' }
        'image/jpeg' { 'jpg' }
        'image/webp' { 'webp' }
        default { throw "Unexpected MCP image type: $($images[0].mimeType)" }
    }
    $imagePath = Join-Path $evidenceDir "mcp-preview.$extension"
    [IO.File]::WriteAllBytes($imagePath,[Convert]::FromBase64String($images[0].data))
    $evidence = [ordered]@{
        utc = [DateTimeOffset]::UtcNow.ToString('o')
        run_id = $runId
        members = $members
        calls = [ordered]@{capture=$capture._call_id; upload=$uploaded._call_id; lookup=$found._call_id; list=$page._call_id; download=$download._call_id; view=$preview._meta.call_id}
        capture = @{path=$capture.path; width=$capture.width; height=$capture.height}
        store_path = $storagePath
        file_ref = $ref
        readback_path = $download.path
        preview_sha256 = (Get-FileHash -LiteralPath $imagePath -Algorithm SHA256).Hash.ToLowerInvariant()
        preview_file = [IO.Path]::GetFileName($imagePath)
    }
    [IO.File]::WriteAllText((Join-Path $evidenceDir 'run.json'),($evidence | ConvertTo-Json -Depth 10))
    Write-Output $imagePath
} finally {
    try {
        $headers = @{'Mcp-Session-Id'=$connection.Session; 'MCP-Protocol-Version'='2025-11-25'}
        $null = Invoke-LoomHttp -Client $connection.Client -Url $McpUrl -Method DELETE -Token $Token -Headers $headers
    } finally {
        $connection.Client.Dispose()
    }
}
