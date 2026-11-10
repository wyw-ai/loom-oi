param([string]$OutputDirectory = (Join-Path $PSScriptRoot 'runtime'))
$ErrorActionPreference = 'Stop'
$go = (Get-Command go -ErrorAction Stop).Source
if (-not [IO.Path]::IsPathRooted($OutputDirectory)) {
    $OutputDirectory = Join-Path $PSScriptRoot $OutputDirectory
}
$oldOS = $env:GOOS
$oldArch = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
$oldWork = $env:GOWORK
Push-Location $PSScriptRoot
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    $env:GOWORK = 'off'
    New-Item -ItemType Directory -Force $OutputDirectory | Out-Null
    & $go build -trimpath -o (Join-Path $OutputDirectory 'oiapp-powershell.exe') ./cmd/worker
    if ($LASTEXITCODE -ne 0) { throw 'PowerShell App build failed' }
} finally {
    $env:GOOS = $oldOS
    $env:GOARCH = $oldArch
    $env:CGO_ENABLED = $oldCGO
    $env:GOWORK = $oldWork
    Pop-Location
}
