# test_rate_limits.ps1

$BASE_URL = "http://localhost:3000"
$TOKEN = "YOUR_JWT_TOKEN_HERE"

Write-Host "`n=== Testing Register Rate Limit (5/hour per IP) ===" -ForegroundColor Cyan

for ($i = 1; $i -le 7; $i++) {
    $body = "{`"email`": `"test$i@test.com`", `"password`": `"password123`"}"
    $response = Invoke-WebRequest -Uri "$BASE_URL/register" `
        -Method POST `
        -ContentType "application/json" `
        -Body $body `
        -ErrorAction SilentlyContinue

    $status = $response.StatusCode
    $color = if ($status -eq 429) { "Red" } elseif ($status -eq 201) { "Green" } else { "Yellow" }
    Write-Host "Request $i : $status" -ForegroundColor $color
}

Write-Host "`n=== Testing Shorten Rate Limit (10/min per user) ===" -ForegroundColor Cyan

for ($i = 1; $i -le 12; $i++) {
    $body = "{`"url`": `"https://example.com/$i`"}"
    $response = Invoke-WebRequest -Uri "$BASE_URL/shorten" `
        -Method POST `
        -ContentType "application/json" `
        -Headers @{ "Authorization" = "Bearer $TOKEN" } `
        -Body $body `
        -ErrorAction SilentlyContinue

    $status = $response.StatusCode
    $color = if ($status -eq 429) { "Red" } elseif ($status -eq 201) { "Green" } else { "Yellow" }
    Write-Host "Request $i : $status" -ForegroundColor $color
}

Write-Host "`n=== Testing Redirect Rate Limit Headers ===" -ForegroundColor Cyan

$response = Invoke-WebRequest -Uri "$BASE_URL/YOUR_CODE" `
    -Method GET `
    -MaximumRedirection 0 `
    -ErrorAction SilentlyContinue

Write-Host "Status: $($response.StatusCode)"
Write-Host "X-RateLimit-Remaining: $($response.Headers['X-RateLimit-Remaining'])"
Write-Host "Retry-After: $($response.Headers['Retry-After'])"

Write-Host "`n=== Testing Login Rate Limit (10/hour per IP) ===" -ForegroundColor Cyan

for ($i = 1; $i -le 12; $i++) {
    $body = "{`"email`": `"test@test.com`", `"password`": `"wrongpassword`"}"
    $response = Invoke-WebRequest -Uri "$BASE_URL/login" `
        -Method POST `
        -ContentType "application/json" `
        -Body $body `
        -ErrorAction SilentlyContinue

    $status = $response.StatusCode
    $color = if ($status -eq 429) { "Red" } elseif ($status -eq 200) { "Green" } else { "Yellow" }
    Write-Host "Request $i : $status" -ForegroundColor $color
}

Write-Host "`nDone." -ForegroundColor Cyan