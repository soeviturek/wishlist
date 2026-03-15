# Deploying Wishlist Price Tracker on Azure App Service

This guide deploys the app as a Docker container on Azure App Service (B1 tier, ~$13 AUD/month).

The `/home` directory on App Service is **persistent** — your SQLite database survives restarts and redeployments.

## Prerequisites

- [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli) installed
- An Azure subscription (Visual Studio Enterprise, Pay-As-You-Go, etc.)
- Your code pushed to a GitHub repo

## 1. Login to Azure

```bash
az login --tenant <YOUR_TENANT_ID>
```

If you don't know your tenant ID, just run `az login` — it will show available tenants.

## 2. Create Resource Group

```bash
az group create --name wishlist-rg --location australiaeast
```

Pick a region close to you. Options: `australiaeast`, `southeastasia`, `westus2`, etc.

## 3. Create App Service Plan

```bash
az appservice plan create \
  --name wishlist-plan \
  --resource-group wishlist-rg \
  --sku B1 \
  --is-linux
```

**B1** is the cheapest tier that supports always-on + persistent storage (~$13 AUD/month).

## 4. Create Azure Container Registry

```bash
az acr create \
  --name wishlistacr \
  --resource-group wishlist-rg \
  --sku Basic \
  --admin-enabled true
```

Get the credentials (you'll need them later):

```bash
az acr credential show --name wishlistacr --query "{username:username, password:passwords[0].value}" -o table
```

## 5. Build and Push Docker Image

```bash
# Login to ACR
az acr login --name wishlistacr

# Build in Azure (no local Docker needed)
az acr build --registry wishlistacr --image wishlist-tracker:latest .
```

This sends your code to Azure, builds the Docker image in the cloud, and stores it in your registry.

## 6. Create the Web App

```bash
az webapp create \
  --name wishlist-tracker-app \
  --resource-group wishlist-rg \
  --plan wishlist-plan \
  --container-image-name wishlistacr.azurecr.io/wishlist-tracker:latest \
  --container-registry-url https://wishlistacr.azurecr.io
```

> **Note**: The app name must be globally unique. If `wishlist-tracker-app` is taken, pick another name.

## 7. Configure ACR Credentials

```bash
# Get ACR password
ACR_PASSWORD=$(az acr credential show --name wishlistacr --query "passwords[0].value" -o tsv)

az webapp config container set \
  --name wishlist-tracker-app \
  --resource-group wishlist-rg \
  --container-registry-url https://wishlistacr.azurecr.io \
  --container-registry-user wishlistacr \
  --container-registry-password $ACR_PASSWORD \
  --container-image-name wishlistacr.azurecr.io/wishlist-tracker:latest
```

## 8. Set Environment Variables

```bash
az webapp config appsettings set \
  --name wishlist-tracker-app \
  --resource-group wishlist-rg \
  --settings \
    SERVER_PORT=8080 \
    DATABASE_PATH=/home/data/wishlist.db \
    SMTP_HOST=smtp.gmail.com \
    SMTP_PORT=587 \
    SMTP_USERNAME=your-email@gmail.com \
    SMTP_PASSWORD="your-gmail-app-password" \
    SMTP_FROM=your-email@gmail.com \
    SCHEDULER_CRON="0 3 * * *" \
    WEBSITES_PORT=8080 \
    WEBSITES_ENABLE_APP_SERVICE_STORAGE=true
```

Replace the SMTP values with your actual Gmail credentials.

## 9. Enable Always-On

```bash
az webapp config set \
  --name wishlist-tracker-app \
  --resource-group wishlist-rg \
  --always-on true
```

This keeps the app running 24/7 so the scheduler works.

## 10. Verify

```bash
# Check the app URL
az webapp show --name wishlist-tracker-app --resource-group wishlist-rg --query "defaultHostName" -o tsv
```

Open `https://wishlist-tracker-app.azurewebsites.net` in your browser.

Check health:

```bash
curl https://wishlist-tracker-app.azurewebsites.net/health
# {"status":"ok"}
```

## Redeploying After Code Changes

After pushing code changes to GitHub:

```bash
# Rebuild the image
az acr build --registry wishlistacr --image wishlist-tracker:latest .

# Restart the app to pull the new image
az webapp restart --name wishlist-tracker-app --resource-group wishlist-rg
```

## Viewing Logs

```bash
# Stream live logs
az webapp log tail --name wishlist-tracker-app --resource-group wishlist-rg

# Or view in Azure Portal:
# App Service → your app → Log stream
```

## Cost Breakdown

| Resource | SKU | Cost (AUD/month) |
|---|---|---|
| App Service Plan | B1 (1 core, 1.75 GB RAM) | ~$13 |
| Container Registry | Basic | ~$7 |
| **Total** | | **~$20** |

Storage for SQLite is included in the App Service plan (1 GB persistent at `/home`).

## Cleanup

To delete everything and stop billing:

```bash
az group delete --name wishlist-rg --yes --no-wait
```

## Alternative: Fly.io

The project also includes a `fly.toml` for deploying on [Fly.io](https://fly.io) (free tier with persistent volumes):

```bash
fly auth signup
fly launch --copy-config --yes
fly volumes create wishlist_data --size 1 --region syd
fly secrets set SMTP_USERNAME=... SMTP_PASSWORD=... SMTP_FROM=... SMTP_HOST=smtp.gmail.com SMTP_PORT=587
fly deploy
```
