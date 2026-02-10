# Quick Deployment Guide

## Deploy to Cloud Run (Dev Environment)

### Prerequisites
- Install gcloud CLI: https://cloud.google.com/sdk/docs/install
- Authenticate: `gcloud auth login`

### Quick Start (3 steps)

```bash
# 1. Set up secrets (first time only)
cd deployments
./setup-secrets.sh

# 2. Deploy to Cloud Run
./deploy-dev.sh

# 3. Test the deployment
curl https://YOUR-SERVICE-URL/health
```

### What Gets Deployed?

- **Service**: `lingofetch-api-dev`
- **Region**: `us-central1`
- **Environment**: `development`
- **Resources**: 512MB RAM, 1 CPU
- **Scaling**: 0-10 instances (scales to zero when idle)

### After Deployment

1. **Update Notion OAuth Redirect URI**
   - Go to: https://www.notion.so/my-integrations
   - Update redirect URI to: `https://YOUR-SERVICE-URL/api/v1/auth/notion/callback`

2. **Update Chrome Extension**
   - Update API endpoint in extension settings

3. **Monitor Logs**
   ```bash
   gcloud run services logs tail lingofetch-api-dev --region us-central1
   ```

### Automated Deployment (GitHub Actions)

Push to `develop` branch to automatically deploy:

```bash
git add .
git commit -m "Deploy to dev"
git push origin develop
```

---

For detailed instructions, see [DEPLOYMENT.md](./DEPLOYMENT.md)
