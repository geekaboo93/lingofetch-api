#!/bin/bash

# Setup GCP Secret Manager secrets for LingoFetch
# Usage: ./setup-secrets.sh [PROJECT_ID]

set -e

PROJECT_ID="${1:-your-gcp-project-id}"

echo "🔐 Setting up GCP Secret Manager for LingoFetch"
echo "Project: ${PROJECT_ID}"
echo ""

# Function to create or update secret
create_or_update_secret() {
  local secret_name=$1
  local secret_value=$2
  
  if gcloud secrets describe ${secret_name} --project ${PROJECT_ID} &>/dev/null; then
    echo "Updating existing secret: ${secret_name}"
    echo -n "${secret_value}" | gcloud secrets versions add ${secret_name} --data-file=- --project ${PROJECT_ID}
  else
    echo "Creating new secret: ${secret_name}"
    echo -n "${secret_value}" | gcloud secrets create ${secret_name} --data-file=- --project ${PROJECT_ID}
  fi
}

# Prompt for secrets
read -p "Enter Llama API Key: " LLAMA_KEY
read -p "Enter Gemini API Key (optional): " GEMINI_KEY
read -p "Enter Notion API Key: " NOTION_KEY
read -p "Enter Notion Database ID: " NOTION_DB

# Create secrets
create_or_update_secret "llama-key" "${LLAMA_KEY}"
create_or_update_secret "gemini-key" "${GEMINI_KEY}"
create_or_update_secret "notion-key" "${NOTION_KEY}"
create_or_update_secret "notion-db" "${NOTION_DB}"

echo ""
echo "🔑 Granting Cloud Run access to secrets..."

# Get the Cloud Run service account
PROJECT_NUMBER=$(gcloud projects describe ${PROJECT_ID} --format='value(projectNumber)')
SERVICE_ACCOUNT="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"

# Grant access to each secret
for secret in "llama-key" "gemini-key" "notion-key" "notion-db"; do
  gcloud secrets add-iam-policy-binding ${secret} \
    --member="serviceAccount:${SERVICE_ACCOUNT}" \
    --role="roles/secretmanager.secretAccessor" \
    --project ${PROJECT_ID}
  echo "✓ Granted access to ${secret}"
done

echo ""
echo "✅ Secret Manager setup complete!"
