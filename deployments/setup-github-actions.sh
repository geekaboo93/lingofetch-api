#!/bin/bash

# GitHub Actions Workload Identity Setup Script
# This script sets up Workload Identity Federation for GitHub Actions

set -e

# Configuration
PROJECT_ID="portfolio-dev-483614"
PROJECT_NUMBER="398671676472"
GITHUB_REPO="geekaboo93/lingofetch-api"  # Change this to your repo
GITHUB_OWNER="geekaboo93"                # Change this to your GitHub username/org

POOL_NAME="github-actions-pool"
PROVIDER_NAME="github-provider"
SERVICE_ACCOUNT_NAME="github-actions-sa"

echo "🚀 Setting up GitHub Actions Workload Identity Federation"
echo "=========================================================="
echo "Project ID: $PROJECT_ID"
echo "GitHub Repo: $GITHUB_REPO"
echo ""

# Step 1: Enable required APIs
echo "📋 Step 1: Enabling required APIs..."
gcloud services enable \
  iamcredentials.googleapis.com \
  cloudresourcemanager.googleapis.com \
  sts.googleapis.com \
  cloudbuild.googleapis.com \
  run.googleapis.com \
  --project=$PROJECT_ID

echo "✅ APIs enabled"
echo ""

# Step 2: Create Workload Identity Pool
echo "🔐 Step 2: Creating Workload Identity Pool..."
if gcloud iam workload-identity-pools describe $POOL_NAME \
    --project=$PROJECT_ID \
    --location=global &>/dev/null; then
  echo "⚠️  Workload Identity Pool already exists, skipping..."
else
  gcloud iam workload-identity-pools create $POOL_NAME \
    --project=$PROJECT_ID \
    --location=global \
    --display-name="GitHub Actions Pool"
  echo "✅ Workload Identity Pool created"
fi
echo ""

# Step 3: Create Workload Identity Provider
echo "🔐 Step 3: Creating Workload Identity Provider..."
if gcloud iam workload-identity-pools providers describe $PROVIDER_NAME \
    --project=$PROJECT_ID \
    --location=global \
    --workload-identity-pool=$POOL_NAME &>/dev/null; then
  echo "⚠️  Workload Identity Provider already exists, skipping..."
else
  gcloud iam workload-identity-pools providers create-oidc $PROVIDER_NAME \
    --project=$PROJECT_ID \
    --location=global \
    --workload-identity-pool=$POOL_NAME \
    --display-name="GitHub Provider" \
    --attribute-mapping="google.subject=assertion.sub,attribute.actor=assertion.actor,attribute.repository=assertion.repository,attribute.repository_owner=assertion.repository_owner" \
    --attribute-condition="assertion.repository_owner == '$GITHUB_OWNER'" \
    --issuer-uri="https://token.actions.githubusercontent.com"
  echo "✅ Workload Identity Provider created"
fi
echo ""

# Step 4: Create Service Account
echo "👤 Step 4: Creating Service Account..."
if gcloud iam service-accounts describe ${SERVICE_ACCOUNT_NAME}@${PROJECT_ID}.iam.gserviceaccount.com \
    --project=$PROJECT_ID &>/dev/null; then
  echo "⚠️  Service Account already exists, skipping..."
else
  gcloud iam service-accounts create $SERVICE_ACCOUNT_NAME \
    --display-name="GitHub Actions Service Account" \
    --project=$PROJECT_ID
  echo "✅ Service Account created"
  echo "⏳ Waiting for service account to propagate (15 seconds)..."
  sleep 15
fi
echo ""

# Step 5: Grant IAM roles to Service Account
echo "🔑 Step 5: Granting IAM roles to Service Account..."
SERVICE_ACCOUNT_EMAIL="${SERVICE_ACCOUNT_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

roles=(
  "roles/run.admin"
  "roles/iam.serviceAccountUser"
  "roles/cloudbuild.builds.editor"
  "roles/storage.admin"
  "roles/artifactregistry.writer"
)

for role in "${roles[@]}"; do
  echo "  Granting $role..."
  gcloud projects add-iam-policy-binding $PROJECT_ID \
    --member="serviceAccount:$SERVICE_ACCOUNT_EMAIL" \
    --role="$role" \
    --condition=None \
    --quiet
done

echo "✅ IAM roles granted"
echo ""

# Step 6: Allow GitHub Actions to impersonate Service Account
echo "🔗 Step 6: Binding Workload Identity..."
gcloud iam service-accounts add-iam-policy-binding \
  $SERVICE_ACCOUNT_EMAIL \
  --project=$PROJECT_ID \
  --role="roles/iam.workloadIdentityUser" \
  --member="principalSet://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/$POOL_NAME/attribute.repository/$GITHUB_REPO"

echo "✅ Workload Identity bound"
echo ""

# Step 7: Get Workload Identity Provider resource name
echo "📋 Step 7: Getting Workload Identity Provider details..."
PROVIDER_RESOURCE_NAME=$(gcloud iam workload-identity-pools providers describe $PROVIDER_NAME \
  --project=$PROJECT_ID \
  --location=global \
  --workload-identity-pool=$POOL_NAME \
  --format="value(name)")

echo ""
echo "=========================================================="
echo "✅ Setup Complete!"
echo "=========================================================="
echo ""
echo "📝 Add these secrets to your GitHub repository:"
echo ""
echo "1. GCP_WORKLOAD_IDENTITY_PROVIDER"
echo "   Value:"
echo "   $PROVIDER_RESOURCE_NAME"
echo ""
echo "2. GCP_SERVICE_ACCOUNT"
echo "   Value:"
echo "   $SERVICE_ACCOUNT_EMAIL"
echo ""
echo "🔗 Go to: https://github.com/$GITHUB_REPO/settings/secrets/actions"
echo ""
echo "=========================================================="
echo "Next Steps:"
echo "1. Add the above secrets to GitHub"
echo "2. Create a 'dev' branch: git checkout -b dev && git push origin dev"
echo "3. Test the workflow manually from GitHub Actions"
echo "4. Create a PR to dev branch and merge it to trigger auto-deployment"
echo "=========================================================="
