# Cloud Run Deployment Scenario

This scenario demonstrates deploying the ADK Cloud Proxy to Google Cloud Run, using GCP Pub/Sub for JIT activation.

## Architecture

1.  **GCP Pub/Sub:** Used for JIT invitations.
2.  **Router Proxy:** Deployed as a Cloud Run service. Handles both HTTP and gRPC on the same port (using h2c).
3.  **Connector:** Deployed as another service (Cloud Run, GKE, or even a local server) that dials the Router Proxy's URL.

## Prerequisites

- **Google Cloud Project** with billing enabled.
- **gcloud CLI** installed and authenticated.
- **Docker** installed for building images.
- **Artifact Registry** repository to store images.

## Step-by-Step Guide

### 1. Setup GCP Resources
```bash
export PROJECT_ID=$(gcloud config get-value project)
export REGION=us-central1

# Enable APIs
gcloud services enable pubsub.googleapis.com run.googleapis.com artifactregistry.googleapis.com

# Create Pub/Sub Topic (e.g., for AppID 'myapp')
gcloud pubsub topics create invites.myapp

# Create a Service Account for the Proxy
gcloud iam service-accounts create cloud-proxy-sa
gcloud projects add-iam-policy-binding $PROJECT_ID 
    --member="serviceAccount:cloud-proxy-sa@$PROJECT_ID.iam.gserviceaccount.com" 
    --role="roles/pubsub.publisher"

# Create a Service Account for the Connector
gcloud iam service-accounts create connector-sa
gcloud projects add-iam-policy-binding $PROJECT_ID 
    --member="serviceAccount:connector-sa@$PROJECT_ID.iam.gserviceaccount.com" 
    --role="roles/pubsub.subscriber"
```

### 2. Deploy Router Proxy
1. Build the image:
```bash
docker build -f Dockerfile.router-proxy -t $REGION-docker.pkg.dev/$PROJECT_ID/adk/router-proxy .
docker push $REGION-docker.pkg.dev/$PROJECT_ID/adk/router-proxy
```
2. Deploy to Cloud Run:
```bash
gcloud run deploy router-proxy 
    --image $REGION-docker.pkg.dev/$PROJECT_ID/adk/router-proxy 
    --service-account cloud-proxy-sa@$PROJECT_ID.iam.gserviceaccount.com 
    --set-env-vars "ISSUER_PUBLIC_KEY=your_pub_key,PROXY_URL=router-proxy-xxxx.a.run.app:443,TLS_ENABLED=true" 
    --region $REGION 
    --allow-unauthenticated 
    --use-http2
```
*Note: The `PROXY_URL` should be the final URL of your Cloud Run service without the `https://` prefix.*

### 3. Deploy Connector
1. Build the image:
```bash
docker build -f Dockerfile.connector -t $REGION-docker.pkg.dev/$PROJECT_ID/adk/connector .
docker push $REGION-docker.pkg.dev/$PROJECT_ID/adk/connector
```
2. Deploy (or run anywhere):
```bash
gcloud run deploy connector 
    --image $REGION-docker.pkg.dev/$PROJECT_ID/adk/connector 
    --service-account connector-sa@$PROJECT_ID.iam.gserviceaccount.com 
    --set-env-vars "NKEY_SEED=your_seed,APP_ID=myapp,TARGET_ADK_SERVER_URL=http://your-adk-target,TLS_ENABLED=true" 
    --region $REGION 
    --no-allow-unauthenticated
```

## Configuration Files

- `config.yaml`: Configuration for GCP Pub/Sub.
