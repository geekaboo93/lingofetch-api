#!/bin/bash

# LingoFetch Cloud Run Deployment Script (Dev Environment)
# Usage: ./deploy-dev.sh

set -e

# Configuration
PROJECT_ID="portfolio-dev-483614"
SERVICE_NAME="lingofetch-api-dev"
REGION="us-central1"
IMAGE_NAME="gcr.io/${PROJECT_ID}/${SERVICE_NAME}"

echo "🚀 Deploying LingoFetch API to Cloud Run (Dev Environment)"
echo "============================================================"
echo "Project:  ${PROJECT_ID}"
echo "Region:   ${REGION}"
echo "Service:  ${SERVICE_NAME}"
echo ""

# Check if gcloud is installed
if ! command -v gcloud &> /dev/null; then
    echo "❌ Error: gcloud CLI is not installed"
    echo "Please install it from: https://cloud.google.com/sdk/docs/install"
    exit 1
fi

# Check if authenticated
if ! gcloud auth list --filter=status:ACTIVE --format="value(account)" | grep -q .; then
    echo "❌ Error: Not authenticated with gcloud"
    echo "Please run: gcloud auth login"
    exit 1
fi

# Set the project
echo "📋 Setting GCP project..."
gcloud config set project ${PROJECT_ID}

# Enable required APIs
echo ""
echo "📦 Enabling required GCP APIs..."
gcloud services enable \
    run.googleapis.com \
    cloudbuild.googleapis.com \
    secretmanager.googleapis.com \
    firestore.googleapis.com \
    --project ${PROJECT_ID}

# Build and push the container image
echo ""
echo "🏗️  Building container image..."
cd ..  # Go to project root
gcloud builds submit \
    --config=deployments/cloudbuild.yaml \
    --project ${PROJECT_ID} \
    --timeout=10m \
    .

# Deploy to Cloud Run
echo ""
echo "🌐 Deploying to Cloud Run..."
gcloud run deploy ${SERVICE_NAME} \
    --image ${IMAGE_NAME}:latest \
    --platform managed \
    --region ${REGION} \
    --allow-unauthenticated \
    --set-env-vars="ENVIRONMENT=development,GCP_PROJECT_ID=${PROJECT_ID},FIRESTORE_DATABASE_ID=lingofetch-dev" \
    --set-secrets="OPENROUTER_API_KEY=openrouter-api-key:latest,GEMINI_API_KEY=gemini-api-key:latest,NOTION_OAUTH_CLIENT_ID=notion-oauth-client-id:latest,NOTION_OAUTH_CLIENT_SECRET=notion-oauth-client-secret:latest,ENCRYPTION_KEY=encryption-key:latest" \
    --memory 512Mi \
    --cpu 1 \
    --max-instances 10 \
    --min-instances 0 \
    --timeout 60s \
    --project ${PROJECT_ID}

# Get the service URL
echo ""
echo "📍 Getting service URL..."
SERVICE_URL=$(gcloud run services describe ${SERVICE_NAME} \
    --platform managed \
    --region ${REGION} \
    --format 'value(status.url)' \
    --project ${PROJECT_ID})

echo ""
echo "✅ Deployment complete!"
echo "============================================================"
echo "Service URL: ${SERVICE_URL}"
echo ""
echo "Test the deployment:"
echo "  curl ${SERVICE_URL}/health"
echo ""
echo "View logs:"
echo "  gcloud run services logs read ${SERVICE_NAME} --region ${REGION} --project ${PROJECT_ID}"
echo ""

# Optional: Run health check
read -p "Run health check now? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "🏥 Running health check..."
    sleep 5
    if curl -f "${SERVICE_URL}/health"; then
        echo ""
        echo "✅ Health check passed!"
    else
        echo ""
        echo "❌ Health check failed!"
        exit 1
    fi
fi
