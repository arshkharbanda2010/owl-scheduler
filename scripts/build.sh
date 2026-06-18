#!/usr/bin/env bash
# =============================================================================
# build.sh — Build and optionally push the owl-scheduler Docker image
# =============================================================================
set -euo pipefail

# ---- Configuration ----
IMAGE_REPO="${IMAGE_REPO:-owl-scheduler}"
IMAGE_TAG="${IMAGE_TAG:-0.1.0}"
IMAGE_FULL="${IMAGE_REPO}:${IMAGE_TAG}"
DOCKERFILE="${DOCKERFILE:-Dockerfile}"
CONTEXT="${CONTEXT:-.}"
PUSH="${PUSH:-false}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"

# ---- Colors ----
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

# ---- Preflight checks ----
log_info "Checking prerequisites..."

if ! command -v docker &>/dev/null; then
  log_error "Docker is not installed or not in PATH"
  exit 1
fi

if ! docker info &>/dev/null; then
  log_error "Docker daemon is not running"
  exit 1
fi

log_info "Docker is available"

# ---- Build ----
log_info "Building image: ${IMAGE_FULL}"
log_info "Dockerfile: ${DOCKERFILE}"
log_info "Build context: ${CONTEXT}"
log_info "Target platforms: ${PLATFORMS}"

# Check if buildx is available for multi-platform builds
if docker buildx version &>/dev/null; then
  log_info "Using Docker Buildx for multi-platform build"

  # Create a builder instance if it doesn't exist
  if ! docker buildx inspect owl-builder &>/dev/null; then
    log_info "Creating buildx builder: owl-builder"
    docker buildx create --name owl-builder --driver docker-container --use
  else
    docker buildx use owl-builder
  fi

  BUILD_ARGS=(
    --platform "${PLATFORMS}"
    --file "${DOCKERFILE}"
    --tag "${IMAGE_FULL}"
    --build-arg "BUILDKIT_INLINE_CACHE=1"
    --cache-from "type=local,src=/tmp/.buildx-cache"
    --cache-to "type=local,dest=/tmp/.buildx-cache-new,mode=max"
    --label "org.opencontainers.image.source=https://github.com/zoo/owl-scheduler"
    --label "org.opencontainers.image.version=${IMAGE_TAG}"
    --label "org.opencontainers.image.title=owl-scheduler"
  )

  if [[ "${PUSH}" == "true" ]]; then
    log_info "Push enabled — will push to registry after build"
    BUILD_ARGS+=(--push)
  else
    BUILD_ARGS+=(--load)
  fi

  docker buildx build "${BUILD_ARGS[@]}" "${CONTENTS:-${CONTEXT}}"

  # Move cache to avoid growing indefinitely
  rm -rf /tmp/.buildx-cache
  mv /tmp/.buildx-cache-new /tmp/.buildx-cache 2>/dev/null || true
else
  log_warn "Docker Buildx not available — falling back to standard docker build (single platform only)"

  docker build \
    --file "${DOCKERFILE}" \
    --tag "${IMAGE_FULL}" \
    --build-arg "GO_VERSION=1.22" \
    --label "org.opencontainers.image.source=https://github.com/zoo/owl-scheduler" \
    --label "org.opencontainers.image.version=${IMAGE_TAG}" \
    --label "org.opencontainers.image.title=owl-scheduler" \
    "${CONTEXT}"

  if [[ "${PUSH}" == "true" ]]; then
    log_info "Pushing image: ${IMAGE_FULL}"
    docker push "${IMAGE_FULL}"
  fi
fi

# ---- Verify ----
log_info "Verifying image..."
docker image inspect "${IMAGE_FULL}" &>/dev/null && \
  log_info "Image built successfully: ${IMAGE_FULL}" || \
  log_error "Image verification failed"

# ---- Summary ----
echo ""
log_info "Build complete!"
echo "  Image:    ${IMAGE_FULL}"
echo "  Tag:      ${IMAGE_TAG}"
echo "  Platforms: ${PLATFORMS}"
echo ""
echo "To deploy with Helm:"
echo "  helm install owl-scheduler ./helm/k8s-scheduler \\"
echo "    --namespace owl-scheduler-system \\"
echo "    --create-namespace \\"
echo "    --set image.repository=${IMAGE_REPO} \\"
echo "    --set image.tag=${IMAGE_TAG}"
