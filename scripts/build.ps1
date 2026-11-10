param([string]$OutputDirectory = (Join-Path $PSScriptRoot '..\dist'))
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
$go = (Get-Command go -ErrorAction Stop).Source
$oldOS = $env:GOOS
$oldArch = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
$oldWorkspace = $env:GOWORK
Push-Location $root
try {
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    $env:GOWORK = 'off'
    foreach ($platform in @('windows', 'linux')) {
        $env:GOOS = $platform
        $out = Join-Path $OutputDirectory "$platform-amd64"
        New-Item -ItemType Directory -Force $out | Out-Null
        foreach ($name in @('ois', 'oic')) {
            $extension = if ($platform -eq 'windows') { '.exe' } else { '' }
            & $go build -trimpath -o (Join-Path $out "$name$extension") "./cmd/$name"
            if ($LASTEXITCODE -ne 0) { throw "Build failed: $platform/$name" }
        }
    }
    # Each App is an independent module. Ship only its declaration and worker,
    # preserving the relative runtime command declared by app.yaml.
    foreach ($app in @(
        @{ Name = 'bash'; Platform = 'linux'; Binary = 'oiapp-bash' },
        @{ Name = 'powershell'; Platform = 'windows'; Binary = 'oiapp-powershell.exe' }
    )) {
        $env:GOOS = $app.Platform
        $source = Join-Path $root "apps/$($app.Name)"
        $package = Join-Path $OutputDirectory "apps/$($app.Name)"
        $runtime = Join-Path $package 'runtime'
        New-Item -ItemType Directory -Force $runtime | Out-Null
        Push-Location $source
        try {
            & $go build -trimpath -o (Join-Path $runtime $app.Binary) ./cmd/worker
            if ($LASTEXITCODE -ne 0) { throw "Build failed: $($app.Platform)/$($app.Name) App" }
        } finally { Pop-Location }
        Copy-Item -LiteralPath (Join-Path $source 'app.yaml') -Destination (Join-Path $package 'app.yaml')
    }
} finally {
    $env:GOOS = $oldOS
    $env:GOARCH = $oldArch
    $env:CGO_ENABLED = $oldCGO
    $env:GOWORK = $oldWorkspace
    Pop-Location
}
