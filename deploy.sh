#!/bin/bash
# deploy.sh — Build, push, and deploy Wishlist Price Tracker to Azure
#
# Usage:
#   ./deploy.sh
#
# Prerequisites:
#   - Azure CLI installed and logged in (az login --tenant <TENANT_ID>)
#   - Resource group, ACR, App Service already created (see DEPLOY.md)

set -e

# --- Config ---
RESOURCE_GROUP="wishlist-rg"
ACR_NAME="wishlistacr"
IMAGE_NAME="wishlist-tracker"
IMAGE_TAG="latest"
APP_NAME="wishlist-tracker-app"

echo "=== Wishlist Price Tracker — Azure Deploy ==="
echo ""

# 1. Build Docker image in Azure (cloud build)
echo "📦 Building Docker image in Azure ACR..."
az acr build \
  --registry "$ACR_NAME" \
  --image "$IMAGE_NAME:$IMAGE_TAG" \
  --file Dockerfile \
  .

echo ""
echo "✅ Image built and pushed to $ACR_NAME.azurecr.io/$IMAGE_NAME:$IMAGE_TAG"

# 2. Restart the web app to pull the new image
echo ""
echo "🔄 Restarting App Service..."
az webapp restart \
  --name "$APP_NAME" \
  --resource-group "$RESOURCE_GROUP"

# 3. Wait for it to come up
echo ""
echo "⏳ Waiting for app to start..."
sleep 30

# 4. Health check
echo ""
echo "🏥 Health check..."
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "https://$APP_NAME.azurewebsites.net/health" || true)

if [ "$HTTP_STATUS" = "200" ]; then
  echo "✅ Deployed successfully!"
  echo "🌐 https://$APP_NAME.azurewebsites.net"
else
  echo "⚠️  Health check returned HTTP $HTTP_STATUS — app may still be starting."
  echo "   Check logs: az webapp log tail --name $APP_NAME --resource-group $RESOURCE_GROUP"
  echo "🌐 https://$APP_NAME.azurewebsites.net"
fi
