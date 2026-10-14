#Requires -Version 7.0
param(
    [string]$BaseURL = 'http://localhost:8080',
    [string]$JaegerURL = 'http://localhost:16686',
    [string]$Username = 'admin',
    [string]$Password = 'admin123'
)

$ErrorActionPreference = 'Stop'
$traceIDs = [System.Collections.Generic.List[string]]::new()
$checks = [System.Collections.Generic.List[object]]::new()
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    $loginBody = @{ username = $Username; password = $Password } | ConvertTo-Json -Compress
    $login = Invoke-RestMethod "$BaseURL/api/auth/login" -Method Post -ContentType 'application/json' -Body $loginBody
    $cases = @(
        @{ name = 'REST'; path = '/api/gateway/invoke'; status = 200; error = $false; spans = @('POST /api/gateway/invoke', 'tool.invoke', 'HTTP GET'); body = @{tool_name='weather_current'; arguments=@{city='Shanghai'}} },
        @{ name = 'MCP'; path = '/mcp'; status = 200; error = $false; spans = @('POST /mcp', 'tool.invoke', 'mcp.tools/call', 'HTTP POST'); body = @{jsonrpc='2.0'; id=1; method='tools/call'; params=@{name='mcp_echo'; arguments=@{text='tracing verification'}}} },
        @{ name = 'MCP tool error'; path = '/mcp'; status = 200; error = $true; spans = @('POST /mcp'); body = @{jsonrpc='2.0'; id=2; method='tools/call'; params=@{name='qa_missing_tool'; arguments=@{}}} },
        @{ name = 'Downstream failure'; path = '/api/gateway/invoke'; status = 502; error = $true; spans = @('POST /api/gateway/invoke', 'tool.invoke', 'HTTP GET'); body = @{tool_name='weather_forecast'; arguments=@{city='Shanghai'; days='invalid'}} }
    )

    foreach ($case in $cases) {
        $traceID = '1' + [guid]::NewGuid().ToString('N').Substring(1)
        $parentID = '1' + [guid]::NewGuid().ToString('N').Substring(0,15)
        $traceIDs.Add($traceID)
        $headers = @{ Authorization = "Bearer $($login.token)"; traceparent = "00-$traceID-$parentID-01" }
        $body = $case.body | ConvertTo-Json -Depth 8 -Compress
        $response = Invoke-WebRequest "$BaseURL$($case.path)" -Method Post -ContentType 'application/json' -Headers $headers -Body $body -SkipHttpErrorCheck
        if ($response.StatusCode -ne $case.status -or $response.Headers['X-Trace-Id'][0] -ne $traceID) {
            throw "$($case.name): unexpected HTTP status or response trace ID"
        }
        $responseJSON = $response.Content | ConvertFrom-Json
        if (-not $case.error -and ($responseJSON.result.isError -or -not $responseJSON.result.content)) { throw "$($case.name): tool call did not succeed" }
        if ($case.name -eq 'REST' -and $responseJSON.trace_id -ne $traceID) { throw 'REST response body has a different trace ID' }
        if ($case.name -eq 'MCP tool error' -and -not $responseJSON.result.isError) { throw 'Expected an MCP tool error' }

        $traceData = $null
        for ($attempt = 0; $attempt -lt 20; $attempt++) {
            try {
                $traceResponse = Invoke-RestMethod "$JaegerURL/api/traces/$traceID"
                $traceData = $traceResponse.data[0]
            } catch { $traceData = $null }
            if ($traceData -and @($traceData.spans).Count -ge $case.spans.Count) { break }
            Start-Sleep -Milliseconds 500
        }
        if (-not $traceData) { throw "$($case.name): trace was not exported to Jaeger" }
        $names = @($traceData.spans.operationName)
        foreach ($required in $case.spans) {
            if ($names -notcontains $required) { throw "$($case.name): missing span $required" }
        }
        $root = @($traceData.spans | Where-Object operationName -eq $case.spans[0])[0]
        if (@($root.references | Where-Object { $_.spanID -eq $parentID -and $_.traceID -eq $traceID }).Count -ne 1) {
            throw "$($case.name): request span did not continue its remote parent"
        }
        foreach ($span in $traceData.spans | Where-Object spanID -ne $root.spanID) {
            $parent = @($span.references | Where-Object refType -eq 'CHILD_OF')[0]
            if (-not $parent -or $names.Count -gt 1 -and @($traceData.spans | Where-Object spanID -eq $parent.spanID).Count -ne 1) {
                throw "$($case.name): broken span parent relationship"
            }
        }
        $errorTags = @($root.tags | Where-Object { ($_.key -eq 'error' -and $_.value -eq $true) -or ($_.key -eq 'otel.status_code' -and $_.value -eq 'ERROR') })
        if ($case.error -and $errorTags.Count -eq 0) { throw "$($case.name): error was not marked in Jaeger" }
        $checks.Add([pscustomobject]@{name=$case.name; trace_id=$traceID; spans=$names; error_expected=$case.error; passed=$true})
    }

    $idSQL = ($traceIDs | ForEach-Object { "'$_'" }) -join ','
    $counts = "SELECT count(*) FROM tool_calls WHERE trace_id IN ($idSQL); SELECT count(*) FROM audit_logs WHERE trace_id IN ($idSQL);" | docker compose exec -T postgres psql -qAt -v ON_ERROR_STOP=1 -U mcp -d mcp_market
    if ($LASTEXITCODE -ne 0 -or [int]$counts[0] -ne 3 -or [int]$counts[1] -ne 4) {
        throw 'Call and audit log trace correlation failed'
    }
    $result = [pscustomobject]@{verified_at=[DateTimeOffset]::UtcNow.ToString('o'); checks=@($checks.ToArray()); call_log_rows=[int]$counts[0]; audit_log_rows=[int]$counts[1]}
} finally {
    try {
        if ($traceIDs.Count -gt 0) {
            $idSQL = ($traceIDs | ForEach-Object { "'$_'" }) -join ','
            "DELETE FROM tool_calls WHERE trace_id IN ($idSQL); DELETE FROM audit_logs WHERE trace_id IN ($idSQL);" | docker compose exec -T postgres psql -q -v ON_ERROR_STOP=1 -U mcp -d mcp_market
            if ($LASTEXITCODE -ne 0) { throw 'Temporary tracing logs could not be cleaned' }
        }
    } finally { Pop-Location }
}
$result | ConvertTo-Json -Depth 8
