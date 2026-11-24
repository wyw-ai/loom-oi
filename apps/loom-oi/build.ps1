param([string]$OutDir = "")
$ErrorActionPreference = "Stop"
Push-Location $PSScriptRoot
try {
    if (!$OutDir) { $OutDir = Join-Path $PSScriptRoot "dist/loom-oi-windows" }
    $runtimeDir = Join-Path $OutDir "runtime"
    New-Item -ItemType Directory -Path $runtimeDir -Force | Out-Null
    $env:GOWORK = "off"
    go build -trimpath -o (Join-Path $runtimeDir "oiapp-loom-oi.exe") ./cmd/worker
    if ($LASTEXITCODE -ne 0) { throw "Go build failed" }
    $manifest = (Get-Content -Raw -Encoding UTF8 -LiteralPath "app.yaml").Replace("./runtime/oiapp-loom-oi]", "./runtime/oiapp-loom-oi.exe]")
    [IO.File]::WriteAllText((Join-Path $OutDir "app.yaml"), $manifest, [Text.UTF8Encoding]::new($false))
} finally { Pop-Location }
