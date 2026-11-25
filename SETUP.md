# One-time setup

Everything here is manual and happens once. A deploy swaps the image and
nothing else (`cloudbuild.yaml`), so none of this is set by a deploy and a
deploy can never undo it.

```sh
export PROJECT_ID=nicocesar-recipes
export REGION=us-east4
export SITE=recipes                 # recipes.nicocesar.com
export REPO=tabular_recipes         # github.com/nicocesar/tabular_recipes
export IMAGE_REPO=tabular-recipes   # Artifact Registry repo + image (repo name, hyphenated)
export APP=recipes                  # Cloud Run service
export DOMAIN=${SITE}.nicocesar.com
export SA=${APP}@${PROJECT_ID}.iam.gserviceaccount.com
```

No GCS bucket: this project stores no files (ADR 0010). When one is needed
it will be `gs://recipes.nicocesar.com`.

## 0. Project

```sh
gcloud projects create ${PROJECT_ID}
gcloud billing projects link ${PROJECT_ID} --billing-account=<ACCOUNT>
gcloud config set project ${PROJECT_ID}
```

## 1. APIs

```sh
gcloud services enable run.googleapis.com cloudbuild.googleapis.com \
  datastore.googleapis.com secretmanager.googleapis.com \
  cloudscheduler.googleapis.com artifactregistry.googleapis.com
```

## 2. Datastore and Artifact Registry

```sh
gcloud firestore databases create --location=${REGION} --type=datastore-mode
gcloud artifacts repositories create ${IMAGE_REPO} --repository-format=docker --location=${REGION}
```

## 3. Runtime service account

```sh
gcloud iam service-accounts create ${APP}
gcloud projects add-iam-policy-binding ${PROJECT_ID} \
  --member=serviceAccount:${SA} --role=roles/datastore.user
```

## 4. Anthropic workspace (ADRs 0008, 0012)

In the Claude Console (platform.claude.com), by hand:

1. Create a workspace named **recipes**. Note its id (`ANTHROPIC_WORKSPACE_ID`,
   used only to link Traces to sessions).
2. Set the workspace's **monthly spend limit** (≈ $20). This is the hard
   stop that holds even if every guard in the app has a bug.
3. Create an API key in that workspace. It goes in Secret Manager below.

No admin key (ADR 0008 / rule 12): `/admin` shows meters and the session's
own list cost; dollars are in the Console.

## 5. Secrets

One secret per shared secret, named like its env var.

```sh
for s in SESSION_SECRET TICK_TOKEN; do
  openssl rand -hex 32 | tr -d '\n' | gcloud secrets create $s --data-file=-
done
# Typed by the owner, silently, so the values never land in shell history:
read -rsp "Owner passphrase: " v && printf '%s' "$v" | gcloud secrets create OWNER_PASSWORD --data-file=-; unset v
read -rsp "Anthropic API key: " v && printf '%s' "$v" | gcloud secrets create ANTHROPIC_API_KEY --data-file=-; unset v
for s in SESSION_SECRET TICK_TOKEN OWNER_PASSWORD ANTHROPIC_API_KEY; do
  gcloud secrets add-iam-policy-binding $s \
    --member=serviceAccount:${SA} --role=roles/secretmanager.secretAccessor
done
```

## 6. Cloud Build

New projects run Cloud Build as the **Compute Engine default** service
account, not the legacy Cloud Build one, so both get the deploy roles, and
the compute one also needs to push images and write logs:

```sh
export PROJECT_NUMBER=$(gcloud projects describe ${PROJECT_ID} --format='value(projectNumber)')
export CB=${PROJECT_NUMBER}@cloudbuild.gserviceaccount.com
export CE=${PROJECT_NUMBER}-compute@developer.gserviceaccount.com
for m in ${CB} ${CE}; do
  gcloud projects add-iam-policy-binding ${PROJECT_ID} --member=serviceAccount:${m} --role=roles/run.admin --condition=None
  gcloud iam service-accounts add-iam-policy-binding ${SA} --member=serviceAccount:${m} --role=roles/iam.serviceAccountUser
done
gcloud projects add-iam-policy-binding ${PROJECT_ID} --member=serviceAccount:${CE} --role=roles/artifactregistry.writer --condition=None
gcloud projects add-iam-policy-binding ${PROJECT_ID} --member=serviceAccount:${CE} --role=roles/logging.logWriter --condition=None
```

The very first `gcloud builds submit` on a minutes-old project may fail with
`serviceusage.services.use` PERMISSION_DENIED even for an Owner: that is
propagation lag after enabling APIs. Retry after a minute.

The first image (before the service exists) is built with
`gcloud builds submit --region=${REGION} --tag ${REGION}-docker.pkg.dev/${PROJECT_ID}/${IMAGE_REPO}/tabular-recipes:<sha> .`

Connect the GitHub repo `nicocesar/tabular_recipes` in Cloud Build and create
a push-to-`main` trigger that runs `cloudbuild.yaml`.

## 7. First deploy and service settings

The first deploy creates the service, so it is the one time a deploy sets
more than the image. Build the first image with `make deploy`'s build step
(or let the trigger run once; its `services update` fails until the
service exists — that's expected the very first time).

```sh
gcloud run deploy ${APP} --region=${REGION} \
  --image=${REGION}-docker.pkg.dev/${PROJECT_ID}/${IMAGE_REPO}/tabular-recipes:<sha> \
  --service-account=${SA} --allow-unauthenticated \
  --cpu-throttling --min-instances=0 --timeout=900 \
  --update-secrets=SESSION_SECRET=SESSION_SECRET:latest,TICK_TOKEN=TICK_TOKEN:latest,OWNER_PASSWORD=OWNER_PASSWORD:latest,ANTHROPIC_API_KEY=ANTHROPIC_API_KEY:latest \
  --set-env-vars=STORAGE=datastore,GOOGLE_CLOUD_PROJECT=${PROJECT_ID},ANTHROPIC_WORKSPACE_ID=<id>

export RUN_URL=$(gcloud run services describe ${APP} --region=${REGION} --format='value(status.url)')
gcloud run services update ${APP} --region=${REGION} --update-env-vars=WORK_URL=${RUN_URL}
```

- `--cpu-throttling` + `--min-instances=0`: request-based billing, scale to
  zero (rule 6). Never `--no-cpu-throttling` (ADR 0013).
- `--timeout=900`: a Run's ceiling is 10 min plus slack (ADR 0013).
- `WORK_URL` is the `run.app` URL, not the custom domain: the self-dispatched
  request is held open for the whole Run.

## 8. The Tick

```sh
gcloud scheduler jobs create http Tick --location=${REGION} \
  --schedule="0,15,30,45 * * * *" --time-zone=Etc/UTC \
  --uri="${RUN_URL}/tick" --http-method=POST \
  --headers="Authorization=Bearer $(gcloud secrets versions access latest --secret=TICK_TOKEN)" \
  --attempt-deadline=180s
```

The token sits in the job's config, so grant `roles/cloudscheduler.viewer`
carefully. After the first quarter hour, `/admin` should show the last pass
instead of "never".

## 9. Domain

```sh
gcloud beta run domain-mappings create --service=${APP} --domain=${DOMAIN} --region=${REGION}
```

Add a CNAME `recipes` → `ghs.googlehosted.com` in the nicocesar.com zone;
the certificate provisions itself.
