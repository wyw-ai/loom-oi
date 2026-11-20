Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# Without an explicit CA, HttpClient uses normal system TLS validation. An
# explicit CA restricts trust to that root and still checks name/date/EKU.
# The callback is C# so asynchronous HTTP threads do not require a PS runspace.
if (-not ('LoomBeta.HttpTransportV2' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.Net.Http;
using System.Net.Security;
using System.Security.Cryptography.X509Certificates;
namespace LoomBeta {
    public static class HttpTransportV2 {
        public static HttpClient Create(string caPath) {
            var handler = new HttpClientHandler { UseProxy = false };
            if (!String.IsNullOrEmpty(caPath)) {
                var ca = X509CertificateLoader.LoadCertificateFromFile(caPath);
                handler.ServerCertificateCustomValidationCallback = (request, certificate, suppliedChain, errors) => {
                    if (certificate == null || (errors & SslPolicyErrors.RemoteCertificateNameMismatch) != 0 ||
                        (errors & SslPolicyErrors.RemoteCertificateNotAvailable) != 0) return false;
                    using var chain = new X509Chain();
                    chain.ChainPolicy.TrustMode = X509ChainTrustMode.CustomRootTrust;
                    chain.ChainPolicy.CustomTrustStore.Add(ca);
                    if (suppliedChain != null) {
                        foreach (var element in suppliedChain.ChainElements) {
                            chain.ChainPolicy.ExtraStore.Add(element.Certificate);
                        }
                    }
                    chain.ChainPolicy.RevocationMode = X509RevocationMode.NoCheck;
                    chain.ChainPolicy.ApplicationPolicy.Add(new System.Security.Cryptography.Oid("1.3.6.1.5.5.7.3.1"));
                    return chain.Build(certificate);
                };
            }
            return new HttpClient(handler) { Timeout = TimeSpan.FromSeconds(75) };
        }
    }
}
'@
}

function New-LoomHttpClient {
    param([string]$CAFile)
    $caPath = if ($CAFile) { (Resolve-Path -LiteralPath $CAFile -ErrorAction Stop).Path } else { $null }
    return [LoomBeta.HttpTransportV2]::Create($caPath)
}

function Get-LoomCredentials {
    param([Parameter(Mandatory)][string]$DeploymentRoot, [switch]$UseCredentials)
    $credentials = @{}
    foreach ($name in @('OIS_ADMIN_TOKEN', 'WORKBENCH_TOKEN', 'LINUX_CHANNEL_TOKEN', 'WINDOWS_CHANNEL_TOKEN')) {
        $credentials[$name] = ''
    }
    # Old deployment files must not silently put bearer credentials on the new
    # default HTTP connection. Loading stored/environment tokens is explicit.
    if (-not $UseCredentials) { return $credentials }
    foreach ($name in @($credentials.Keys)) { $credentials[$name] = [string][Environment]::GetEnvironmentVariable($name) }
    $path = Join-Path $DeploymentRoot 'credentials.json'
    if (Test-Path -LiteralPath $path) {
        $stored = Get-Content -LiteralPath $path -Raw | ConvertFrom-Json -AsHashtable
        if ($stored -isnot [System.Collections.IDictionary]) { throw 'credentials.json must contain a JSON object.' }
        foreach ($entry in $stored.GetEnumerator()) { $credentials[$entry.Key] = [string]$entry.Value }
    }
    return $credentials
}

function Start-LoomHttp {
    param([Parameter(Mandatory)]$Client, [Parameter(Mandatory)][string]$Url,
        [string]$Method = 'POST', [string]$Token, $Body, [hashtable]$Headers = @{})
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::new($Method), $Url)
    if ($Token) { $request.Headers.Authorization = [System.Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', $Token) }
    foreach ($key in $Headers.Keys) { [void]$request.Headers.TryAddWithoutValidation($key, [string]$Headers[$key]) }
    if ($null -ne $Body) {
        $request.Content = [System.Net.Http.StringContent]::new(($Body | ConvertTo-Json -Depth 64 -Compress), [System.Text.Encoding]::UTF8, 'application/json')
    }
    return @{ Task = $Client.SendAsync($request); Request = $request }
}

function Complete-LoomHttp {
    param([Parameter(Mandatory)]$Pending)
    try {
        $response = $Pending.Task.GetAwaiter().GetResult()
        try {
            $raw = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
            $headers = @{}
            foreach ($h in $response.Headers) { $headers[$h.Key] = $h.Value -join ',' }
            $parsed = if ($raw) { $raw | ConvertFrom-Json -AsHashtable -Depth 64 } else { $null }
            return @{ Status = [int]$response.StatusCode; Body = $parsed; Raw = $raw; Headers = $headers }
        } finally { $response.Dispose() }
    } finally { $Pending.Request.Dispose() }
}

function Invoke-LoomHttp {
    param([Parameter(Mandatory)]$Client, [Parameter(Mandatory)][string]$Url,
        [string]$Method = 'GET', [string]$Token, $Body, [hashtable]$Headers = @{})
    Complete-LoomHttp (Start-LoomHttp -Client $Client -Url $Url -Method $Method -Token $Token -Body $Body -Headers $Headers)
}

function Start-McpRequest {
    param([Parameter(Mandatory)]$Connection, [Parameter(Mandatory)][string]$Method,
        [hashtable]$Parameters = @{}, [switch]$Notification)
    $body = @{ jsonrpc = '2.0'; method = $Method; params = $Parameters }
    if (-not $Notification) { $Connection.NextId++; $body.id = $Connection.NextId }
    $headers = @{ Accept = 'application/json, text/event-stream'; 'MCP-Protocol-Version' = '2025-11-25' }
    if ($Connection.Session) { $headers['Mcp-Session-Id'] = $Connection.Session }
    $pending = Start-LoomHttp -Client $Connection.Client -Url $Connection.Url -Token $Connection.Token -Body $body -Headers $headers
    $pending.Id = $body['id']
    return $pending
}

function Invoke-McpRequest {
    param([Parameter(Mandatory)]$Connection, [Parameter(Mandatory)][string]$Method,
        [hashtable]$Parameters = @{}, [switch]$Notification)
    Complete-LoomHttp (Start-McpRequest @PSBoundParameters)
}

function Connect-LoomMcp {
    param([Parameter(Mandatory)][string]$Url, [string]$Token, [string]$CAFile)
    $connection = @{ Client = (New-LoomHttpClient $CAFile); Url = $Url; Token = $Token; Session = ''; NextId = 0 }
    try {
        $init = Invoke-McpRequest $connection initialize @{ protocolVersion = '2025-11-25'; capabilities = @{}; clientInfo = @{ name = 'loom-beta-powershell-acceptance'; version = '1.0' } }
        if ($init.Status -ne 200 -or $init.Body['error']) { throw "MCP initialize failed: $($init.Raw)" }
        if ($init.Body.result.protocolVersion -ne '2025-11-25') { throw 'Unexpected MCP protocol version' }
        $connection.Session = $init.Headers['Mcp-Session-Id']
        if (-not $connection.Session) { throw 'Missing MCP session header' }
        $notice = Invoke-McpRequest $connection notifications/initialized @{} -Notification
        if ($notice.Status -ne 202) { throw "MCP initialized notification failed: $($notice.Raw)" }
        return $connection
    } catch { $connection.Client.Dispose(); throw }
}

function Get-McpToolResult {
    param([Parameter(Mandatory)]$Response, [switch]$AllowError)
    if ($Response.Status -ne 200 -or $Response.Body['error']) { throw "MCP request failed ($($Response.Status)): $($Response.Raw)" }
    $r = $Response.Body.result
    if (-not $AllowError -and $r['isError']) { throw "Tool failed: $($Response.Raw)" }
    foreach ($block in $r['content']) {
        if ($block['type'] -eq 'image') { return $r }
    }
    if ($r['structuredContent']) {
        $value = $r.structuredContent
        if ($r['_meta'] -and $r._meta['call_id']) { $value['_call_id'] = $r._meta.call_id }
        return $value
    }
    if ($r['content'] -and $r.content[0]['text']) { return ($r.content[0].text | ConvertFrom-Json -AsHashtable -Depth 64) }
    throw "Tool result has no structured content: $($Response.Raw)"
}

function Invoke-LoomTool {
    param([Parameter(Mandatory)]$Connection, [Parameter(Mandatory)][string]$Name,
        [hashtable]$Arguments = @{}, [switch]$AllowError)
    $response = Invoke-McpRequest $Connection tools/call @{ name = $Name; arguments = $Arguments }
    Get-McpToolResult $response -AllowError:$AllowError
}

Export-ModuleMember -Function New-LoomHttpClient, Get-LoomCredentials, Start-LoomHttp, Complete-LoomHttp, Invoke-LoomHttp, Connect-LoomMcp, Start-McpRequest, Invoke-McpRequest, Get-McpToolResult, Invoke-LoomTool
