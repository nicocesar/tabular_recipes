# syntax=docker/dockerfile:1
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The build stamp: version.txt ships as written; the commit and the build
# time are link-time facts (rule 19). Empty args are a no-op.
ARG COMMIT=""
ARG BUILT_AT=""
RUN BUILT="${BUILT_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}" && \
    CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -X main.commit=${COMMIT} -X main.builtAt=$BUILT" \
    -o /out/server ./cmd/server

# Distroless, static, nonroot (rule 3). Fonts, templates and static files
# are inside the binary; nothing else is needed.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
ENV PORT=8080
ENTRYPOINT ["/server"]
