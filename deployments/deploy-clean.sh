#!/bin/bash

# Configuration
REGION="${REGION:-asia-southeast1}"  # Default to asia-southeast1, can be overridden with REGION env var
PROJECT_ID="portfolio-dev-483614"
SERVICE_NAME="lingofetch-api-dev"

echo "🌍 Deploying to region: $REGION"

# Go to project root
cd "$(dirname "$0")/.."

# Create a clean build directory
BUILD_DIR="/tmp/lingofetch-build"
rm -rf $BUILD_DIR
mkdir -p $BUILD_DIR

# Copy necessary files
echo "📦 Copying files to build directory..."
cp -r cmd $BUILD_DIR/
cp -r internal $BUILD_DIR/
cp go.mod go.sum $BUILD_DIR/
mkdir -p $BUILD_DIR/deployments
cp deployments/Dockerfile $BUILD_DIR/deployments/
cp deployments/cloudbuild.yaml $BUILD_DIR/deployments/

# Verify cmd directory exists
if [ ! -d "$BUILD_DIR/cmd" ]; then
    echo "❌ ERROR: cmd directory not copied!"
    exit 1
fi

echo "✅ Files copied successfully"
ls -la $BUILD_DIR/

# Deploy from the clean directory
cd $BUILD_DIR
gcloud builds submit \
    --config=deployments/cloudbuild.yaml \
    --project $PROJECT_ID \
    --timeout=10m \
    .

if [ $? -ne 0 ]; then
    echo "❌ Build failed!"
    exit 1
fi

echo ""
echo "🚀 Deploying to Cloud Run..."

# Deploy to Cloud Run
gcloud run deploy $SERVICE_NAME \
    --image gcr.io/$PROJECT_ID/$SERVICE_NAME:latest \
    --platform managed \
    --region $REGION \
    --allow-unauthenticated \
    --set-env-vars="ENVIRONMENT=development,GCP_PROJECT_ID=$PROJECT_ID,FIRESTORE_DATABASE_ID=lingofetch-dev" \
    --set-secrets="GEMINI_API_KEY=gemini-api-key:latest,OPENROUTER_API_KEY=openrouter-api-key:latest,NOTION_OAUTH_CLIENT_ID=notion-oauth-client-id:latest,NOTION_OAUTH_CLIENT_SECRET=notion-oauth-client-secret:latest,ENCRYPTION_KEY=encryption-key:latest" \
    --memory 512Mi \
    --cpu 1 \
    --timeout 300 \
    --max-instances 10 \
    --min-instances 0 \
    --port 8080 \
    --project $PROJECT_ID

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Deployment successful!"
    echo ""
    echo "📋 Getting service URL..."
    SERVICE_URL=$(gcloud run services describe $SERVICE_NAME \
        --region $REGION \
        --project $PROJECT_ID \
        --format 'value(status.url)')
    
    echo ""
    echo "🎉 Deployment complete!"
    echo "Service URL: $SERVICE_URL"
    echo ""
    echo "Next steps:"
    echo "1. Update Notion OAuth redirect URI to: ${SERVICE_URL}/api/v1/auth/notion/callback"
    echo "2. Update Chrome extension API endpoint to: ${SERVICE_URL}"
    echo "3. Test the health endpoint: curl ${SERVICE_URL}/health"
else
    echo "❌ Deployment failed!"
    exit 1
fi
