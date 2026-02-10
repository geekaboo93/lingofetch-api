#!/bin/bash

# LingoFetch GCP Secrets Setup Script
# This script creates all necessary secrets in GCP Secret Manager

set -e

PROJECT_ID="${1:-portfolio-dev-483614}"
REGION="${2:-us-central1}"

echo "🔐 Setting up GCP Secret Manager for LingoFetch"
echo "Project: ${PROJECT_ID}"
echo ""

# Enable Secret Manager API
echo "📦 Enabling Secret Manager API..."
gcloud services enable secretmanager.googleapis.com --project=${PROJECT_ID}

# Function to create or update a secret
create_or_update_secret() {
    local secret_name=$1
    local secret_value=$2
    
    echo "Processing secret: ${secret_name}"
    
    # Check if secret exists
    if gcloud secrets describe ${secret_name} --project=${PROJECT_ID} &>/dev/null; then
        echo "  ↻ Secret exists, adding new version..."
        echo -n "${secret_value}" | gcloud secrets versions add ${secret_name} \
            --data-file=- \
            --project=${PROJECT_ID}
    else
        echo "  + Creating new secret..."
        echo -n "${secret_value}" | gcloud secrets create ${secret_name} \
            --data-file=- \
            --replication-policy="automatic" \
            --project=${PROJECT_ID}
    fi
    echo "  ✓ Done"
    echo ""
}

# Read values from .env file or prompt user
if [ -f .env ]; then
    echo "📄 Reading values from .env file..."
    source .env
else
    echo "⚠️  No .env file found. You'll need to enter values manually."
fi

# Prompt for values if not set
read -p "OpenRouter API Key [${OPENROUTER_API_KEY}]: " input
OPENROUTER_API_KEY="${input:-$OPENROUTER_API_KEY}"

read -p "Gemini API Key [${GEMINI_API_KEY}]: " input
GEMINI_API_KEY="${input:-$GEMINI_API_KEY}"

read -p "Notion OAuth Client ID [${NOTION_OAUTH_CLIENT_ID}]: " input
NOTION_OAUTH_CLIENT_ID="${input:-$NOTION_OAUTH_CLIENT_ID}"

read -p "Notion OAuth Client Secret [${NOTION_OAUTH_CLIENT_SECRET}]: " input
NOTION_OAUTH_CLIENT_SECRET="${input:-$NOTION_OAUTH_CLIENT_SECRET}"

read -p "Encryption Key [${ENCRYPTION_KEY}]: " input
ENCRYPTION_KEY="${input:-$ENCRYPTION_KEY}"

# Create/update all secrets
echo ""
echo "🔧 Creating/updating secrets..."
echo ""

create_or_update_secret "openrouter-api-key" "${OPENROUTER_API_KEY}"
create_or_update_secret "gemini-api-key" "${GEMINI_API_KEY}"
create_or_update_secret "notion-oauth-client-id" "${NOTION_OAUTH_CLIENT_ID}"
create_or_update_secret "notion-oauth-client-secret" "${NOTION_OAUTH_CLIENT_SECRET}"
create_or_update_secret "encryption-key" "${ENCRYPTION_KEY}"

# Grant Cloud Run service account access to secrets
echo "🔑 Granting Cloud Run service account access to secrets..."
PROJECT_NUMBER=$(gcloud projects describe ${PROJECT_ID} --format="value(projectNumber)")
SERVICE_ACCOUNT="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"

echo "Note: If the service account doesn't exist yet, permissions will be set during deployment."
echo ""

for secret in openrouter-api-key gemini-api-key notion-oauth-client-id notion-oauth-client-secret encryption-key; do
    if gcloud secrets add-iam-policy-binding ${secret} \
        --member="serviceAccount:${SERVICE_ACCOUNT}" \
        --role="roles/secretmanager.secretAccessor" \
        --project=${PROJECT_ID} \
        --quiet 2>/dev/null; then
        echo "  ✓ Granted access to ${secret}"
    else
        echo "  ⚠ Could not grant access to ${secret} (will be set during deployment)"
    fi
done

echo ""
echo "✅ All secrets configured successfully!"
echo ""
echo "Next steps:"
echo "1. Run: ./deployments/deploy-dev.sh"
echo "2. Or push to GitHub to trigger automated deployment"
