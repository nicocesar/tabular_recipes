.PHONY: run demo test build docker deploy deploy-preflight

# Local development: filesystem store in ./data, no cloud credentials.
# Runs are goroutines (no WORK_URL). Owner passphrase: "owner".
run:
	OWNER_PASSWORD=owner SESSION_SECRET=dev-secret TICK_TOKEN=dev-tick \
	STORAGE=fs DATA_DIR=./data \
	go run ./cmd/server

# The same, with a free stand-in agent that files a demo card.
demo:
	DEMO_AGENT=1 OWNER_PASSWORD=owner SESSION_SECRET=dev-secret TICK_TOKEN=dev-tick \
	STORAGE=fs DATA_DIR=./data \
	go run ./cmd/server

test:
	go vet ./... && go test ./...

build:
	go build ./...

docker:
	docker buildx build -t tabular-recipes .

# Refuse to upload anything credential-shaped to Cloud Build.
deploy-preflight:
	@found=$$(gcloud meta list-files-for-upload | grep -iE '(^|/)\.env($$|\.)|(^|/)client_secret|credential[^/]*\.json$$|-key\.json$$|(^|/)(sa|token)\.json$$|\.(pem|p12)$$|(^|/)id_rsa|^(data|\.claude)/'); \
	if [ -n "$$found" ]; then \
		echo "deploy-preflight: refusing to upload these to Cloud Build:" >&2; \
		echo "$$found" | sed 's/^/  /' >&2; \
		exit 1; \
	fi

# Manual deploy. Normally a push to main deploys via the trigger; don't
# run this right after a push (it duplicates the trigger's build).
deploy: deploy-preflight
	gcloud builds submit --config cloudbuild.yaml \
		--substitutions=SHORT_SHA=$$(git rev-parse --short HEAD),_BUILT_AT=$$(date -u +%Y-%m-%dT%H:%M:%SZ)
