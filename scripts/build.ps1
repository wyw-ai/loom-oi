param([string]$OutputDirectory = (Join-Path $PSScriptRoot '..\dist'))
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$go = (Get-Command go -ErrorAction Stop).Source
$oldOS = $env:GOOS
$oldArch = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
Push-Location $root
try {
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    foreach ($platform in @('windows', 'linux')) {
        $env:GOOS = $platform
        $out = Join-Path $OutputDirectory "$platform-amd64"
        New-Item -ItemType Directory -Force $out | Out-Null
        foreach ($name in @('ois', 'oic', 'oiapp-terminal')) {
            $extension = if ($platform -eq 'windows') { '.exe' } else { '' }
            & $go build -trimpath -o (Join-Path $out "$name$extension") "./cmd/$name"
            if ($LASTEXITCODE -ne 0) { throw "Build failed: $platform/$name" }
        }
    }
} finally {
    $env:GOOS = $oldOS
    $env:GOARCH = $oldArch
    $env:CGO_ENABLED = $oldCGO
    Pop-Location
}
