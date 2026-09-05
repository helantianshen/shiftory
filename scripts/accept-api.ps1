param(
    [string]$BaseUrl = "http://127.0.0.1:8080"
)

$ErrorActionPreference = "Stop"
$apiRoot = "$BaseUrl/api/v1"
$suffix = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$username = "accept$suffix"
$email = "$username@example.com"
$password = "correct horse battery staple"

function Invoke-JsonApi {
    param(
        [string]$Method,
        [string]$Path,
        [object]$Body,
        [string]$AccessToken
    )
    $headers = @{ Origin = "http://localhost:5173" }
    if ($AccessToken) { $headers.Authorization = "Bearer $AccessToken" }
    $arguments = @{
        Uri        = "$apiRoot$Path"
        Method     = $Method
        Headers    = $headers
        WebSession = $script:webSession
    }
    if ($null -ne $Body) {
        $arguments.ContentType = "application/json"
        $arguments.Body = $Body | ConvertTo-Json -Depth 12 -Compress
    }
    return Invoke-RestMethod @arguments
}

$health = Invoke-RestMethod -Uri "$BaseUrl/health"
if ($health.data.status -ne "ok") { throw "health endpoint did not return ok" }

$script:webSession = [Microsoft.PowerShell.Commands.WebRequestSession]::new()
Invoke-JsonApi -Method Post -Path "/auth/register" -Body @{
    username = $username; email = $email; displayName = "验收用户"; password = $password
} | Out-Null
$login = Invoke-JsonApi -Method Post -Path "/auth/login" -Body @{ login = $email; password = $password }
$accessToken = $login.data.accessToken
if (-not $accessToken) { throw "login did not return an Access JWT" }

$workspace = Invoke-JsonApi -Method Post -Path "/workspaces" -AccessToken $accessToken -Body @{
    name = "API验收工作区-$suffix"; timezone = "Asia/Shanghai"
}
$workspaceId = $workspace.data.id
$userId = $login.data.user.id
$shift = Invoke-JsonApi -Method Post -Path "/workspaces/$workspaceId/shifts" -AccessToken $accessToken -Body @{
    name = "验收班"; code = "ACCEPT"; startTime = "08:30"; endTime = "17:30";
    crossDay = $false; displayColor = "#22a06b"; aliases = @("验收")
}
$date = [DateTime]::Now.ToString("yyyy-MM-dd")
Invoke-JsonApi -Method Put -Path "/workspaces/$workspaceId/schedules/$userId/$date" -AccessToken $accessToken -Body @{
    status = "WORKING"; note = "API runtime acceptance"; version = 0;
    segments = @(@{ type = "SHIFT"; shiftId = $shift.data.id; crossDay = $false })
} | Out-Null
$calendar = Invoke-JsonApi -Method Get -Path "/workspaces/$workspaceId/calendar?start=$date&end=$date&memberIds=$userId" -AccessToken $accessToken
if ($calendar.data.days[0].members[0].note -ne "API runtime acceptance") {
    throw "calendar did not expose the complete confirmed schedule"
}

$preference = Invoke-JsonApi -Method Put -Path "/preferences" -AccessToken $accessToken -Body @{
    currentWorkspaceId = $workspaceId; theme = "lilac"
}
if ($preference.data.theme -ne "lilac") { throw "Pinia preference backing API failed" }

$cookieUri = [Uri]"$apiRoot/auth/refresh"
$csrfCookie = $script:webSession.Cookies.GetCookies($cookieUri) | Where-Object Name -eq "shiftory_csrf" | Select-Object -First 1
if (-not $csrfCookie) { throw "login did not set the CSRF companion cookie" }
$refresh = Invoke-RestMethod -Uri "$apiRoot/auth/refresh" -Method Post -ContentType "application/json" -Body "{}" `
    -Headers @{ Origin = "http://localhost:5173"; "X-CSRF-Token" = $csrfCookie.Value } -WebSession $script:webSession
if (-not $refresh.data.accessToken -or $refresh.data.accessToken -eq $accessToken) {
    throw "refresh did not rotate credentials"
}

$template = Invoke-WebRequest -Uri "$apiRoot/workspaces/$workspaceId/imports/template.xlsx" `
    -Headers @{ Origin = "http://localhost:5173"; Authorization = "Bearer $($refresh.data.accessToken)" } -WebSession $script:webSession
if ($template.StatusCode -ne 200 -or $template.RawContentLength -lt 1000) { throw "Excel template download failed" }

[pscustomobject]@{
    Status      = "PASS"
    WorkspaceId = $workspaceId
    UserId      = $userId
    Date        = $date
    Checks      = @("health", "register/login JWT", "workspace", "shift", "schedule", "member-visible calendar detail", "preferences", "refresh rotation + CSRF", "Excel template")
} | ConvertTo-Json -Depth 4
