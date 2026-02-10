#!/bin/bash

# LingoFetch Cloud Run Deployment Script
# Usage: ./deploy.sh [PROJECT_ID] [REGION]

set -e

# Configuration
PROJECT_ID="${1:-your-gcp-project-id}"
SERVICE_NAME="lingofetch-api"
REGION="${2:-us-central1}"
IMAGE_NAME="gcr.io/${PROJECT_ID}/${SERVICE_NAME}"

echo "🚀 Deploying LingoFetch API to Cloud Run"
echo "Project: ${PROJECT_ID}"
echo "Region: ${REGION}"
echo "Service: ${SERVICE_NAME}"
echo ""

# Build and push the container image
echo "📦 Building container image..."
gcloud builds submit --tag ${IMAGE_NAME} --project ${PROJECT_ID}

echo ""
echo "🌐 Deploying to Cloud Run..."
gcloud run deploy ${SERVICE_NAME} \
  --image ${IMAGE_NAME} \
  --platform managed \
  --region ${REGION} \
  --allow-unauthenticated \
  --set-env-vars="ENVIRONMENT=production,GCP_PROJECT_ID=${PROJECT_ID}" \
  --set-secrets="OPENROUTER_API_KEY=openrouter-key:latest,GEMINI_API_KEY=gemini-key:latest,NOTION_API_KEY=notion-key:latest,NOTION_DATABASE_ID=notion-db:latest" \
  --memory 512Mi \
  --cpu 1 \
  --max-instances 10 \
  --min-instances 0 \
  --timeout 30s \
  --project ${PROJECT_ID}

echo ""
echo "✅ Deployment complete!"
echo ""
echo "Service URL:"
gcloud run services describe ${SERVICE_NAME} --platform managed --region ${REGION} --format 'value(status.url)' --project ${PROJECT_ID}
