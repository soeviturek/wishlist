# deploy.ps1 — Build, push, and deploy Wishlist Price Tracker to Azure
#
# Usage:
#   .\deploy.ps1
#
# Prerequisites:
#   - Azure CLI installed and logged in (az login --tenant <TENANT_ID>)
#   - Resource group, ACR, App Service already created (see DEPLOY.md)

$ErrorActionPreference = "Stop"

# --- Config ---
$ResourceGroup = "wishlist-rg"
$AcrName       = "wishlistacr"
$ImageName     = "wishlist-tracker"
$ImageTag      = "latest"
$AppName       = "wishlist-tracker-app"

Write-Host "=== Wishlist Price Tracker - Azure Deploy ===" -ForegroundColor Cyan
Write-Host ""

# Ensure az is on PATH
$env:Path = [System.Environment]::GetEnvironmentVariable("Path","Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path","User")

# 1. Build Docker image in Azure (cloud build — no local Docker needed)
Write-Host "Building Docker image in Azure ACR..." -ForegroundColor Yellow
az acr build --registry $AcrName --image "${ImageName}:${ImageTag}" --file Dockerfile .
if ($LASTEXITCODE -ne 0) { throw "ACR build failed" }

Write-Host ""
Write-Host "Image built: $AcrName.azurecr.io/${ImageName}:${ImageTag}" -ForegroundColor Green

# 2. Restart the web app to pull the new image
Write-Host ""
Write-Host "Restarting App Service..." -ForegroundColor Yellow
az webapp restart --name $AppName --resource-group $ResourceGroup
if ($LASTEXITCODE -ne 0) { throw "Restart failed" }

# 3. Wait for it to come up
Write-Host ""
Write-Host "Waiting for app to start (30s)..." -ForegroundColor Yellow
Start-Sleep -Seconds 30

# 4. Health check
Write-Host ""
Write-Host "Running health check..." -ForegroundColor Yellow
try {
    $url = "https://${AppName}.azurewebsites.net"
    $response = Invoke-WebRequest -Uri "$url/health" -TimeoutSec 30 -UseBasicParsing
    if ($response.StatusCode -eq 200) {
        Write-Host ""
        Write-Host "Deployed successfully!" -ForegroundColor Green
        Write-Host $url -ForegroundColor Cyan
    }
} catch {
    Write-Host ""
    Write-Host "Health check failed - app may still be starting." -ForegroundColor Yellow
    Write-Host "Check logs:  az webapp log tail --name $AppName --resource-group $ResourceGroup" -ForegroundColor Gray
    Write-Host "URL: $url" -ForegroundColor Cyan
}
