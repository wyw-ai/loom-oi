param([string]$TargetOS = "windows", [string]$Arch = "amd64", [string]$OutDir = "")
$ErrorActionPreference = "Stop"
Push-Location $PSScriptRoot
try {
    $env:GOWORK = "off"
    $options = @("-os", $TargetOS, "-arch", $Arch)
    if ($OutDir) { $options += @("-out", $OutDir) }
    go run ./cmd/package @options
    exit $LASTEXITCODE
} finally {
    Pop-Location
}

