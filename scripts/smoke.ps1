param([string]$Base = 'http://localhost:8080/api/v1')
$ErrorActionPreference = 'Stop'
$stamp = [guid]::NewGuid().ToString('N').Substring(0, 10)
$ownerEmail = "owner-$stamp@example.test"
$memberEmail = "member-$stamp@example.test"

function CallApi([string]$Method, [string]$Path, [object]$Body, [hashtable]$Headers = @{}) {
    $args = @{Method = $Method; Uri = "$Base$Path"; Headers = $Headers}
    if ($null -ne $Body) {
        $args.ContentType = 'application/json'
        $args.Body = ($Body | ConvertTo-Json -Depth 8 -Compress)
    }
    Invoke-RestMethod @args
}
function Assert-Status([scriptblock]$Action, [int]$Expected) {
    try {
        & $Action | Out-Null
        throw "Expected HTTP $Expected but request succeeded"
    } catch {
        $response = $_.Exception.Response
        if ($null -eq $response -or [int]$response.StatusCode -ne $Expected) { throw }
    }
}

$owner = CallApi 'POST' '/register' @{email=$ownerEmail;password='smoke-secret-123';name='Smoke Owner'}
$member = CallApi 'POST' '/register' @{email=$memberEmail;password='smoke-secret-123';name='Smoke Member'}
$ownerToken = (CallApi 'POST' '/login' @{email=$ownerEmail;password='smoke-secret-123'}).access_token
$memberToken = (CallApi 'POST' '/login' @{email=$memberEmail;password='smoke-secret-123'}).access_token
$authOwner = @{Authorization="Bearer $ownerToken"}
$authMember = @{Authorization="Bearer $memberToken"}

$team = CallApi 'POST' '/teams' @{name='Smoke Team'} $authOwner
$task = CallApi 'POST' '/tasks' @{team_id=$team.id;title='Task from smoke';description='Initial';assignee_id=$null} $authOwner
Assert-Status { CallApi 'GET' "/tasks?team_id=$($team.id)" $null $authMember } 404
CallApi 'POST' "/teams/$($team.id)/invite" @{user_id=$member.id;role='member'} $authOwner | Out-Null
$assigned = CallApi 'PUT' "/tasks/$($task.id)" @{expected_version=$task.version;assignee_id=$member.id} $authOwner
Assert-Status { CallApi 'PUT' "/tasks/$($task.id)" @{expected_version=$assigned.version;title='Unauthorized'} $authMember } 403
$done = CallApi 'PUT' "/tasks/$($task.id)" @{expected_version=$assigned.version;status='done'} $authMember
Assert-Status { CallApi 'PUT' "/tasks/$($task.id)" @{expected_version=$assigned.version;status='todo'} $authOwner } 409
$history = CallApi 'GET' "/tasks/$($task.id)/history" $null $authOwner
if ($history.Count -ne 3) { throw "Expected 3 history events, got $($history.Count)" }
CallApi 'POST' "/tasks/$($task.id)/comments" @{content='Smoke comment'} $authMember | Out-Null
$report = CallApi 'GET' "/teams/$($team.id)/stats" $null $authOwner
if ($report.by_status.done -ne 1 -or $report.comments_count -ne 1) {throw "Incorrect report: $($report | ConvertTo-Json -Depth 8)"}
Assert-Status { CallApi 'GET' "/teams/$($team.id)/stats" $null $authMember } 403
Write-Host "PASS: registration, JWT, team isolation, roles, version conflict, history, comments, report. Team ID $($team.id)." -ForegroundColor Green
