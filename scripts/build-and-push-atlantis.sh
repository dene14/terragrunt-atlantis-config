#!/bin/bash
set -euo pipefail

# Script to build and push Atlantis Docker image with custom terragrunt-atlantis-config binary
# Usage: ./scripts/build-and-push-atlantis.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TERRAGRUNT_ATLANTIS_CONFIG_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ATLANTIS_DIR="$HOME/work/ctct/repos/terraform/atlantis"
DOCKER_IMAGE="dbcc/atlantis"
DOCKER_TAG="v0.37.1-alpine-stacks"

echo "=== Step 0: Pushing code to branch for CI validation ==="
cd "$TERRAGRUNT_ATLANTIS_CONFIG_DIR"

# Check if there are uncommitted changes (skip prompt if SKIP_PROMPT env var is set)
if ! git diff-index --quiet HEAD --; then
    if [ -z "${SKIP_PROMPT:-}" ]; then
        echo "Warning: You have uncommitted changes. Consider committing them first."
        read -p "Continue anyway? (y/n) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            exit 1
        fi
    else
        echo "Warning: You have uncommitted changes. Continuing anyway (SKIP_PROMPT set)."
    fi
fi

# Push to remote branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
echo "Pushing to branch: $CURRENT_BRANCH"
git push origin "$CURRENT_BRANCH" || {
    echo "Warning: Failed to push to remote. Continuing with build anyway..."
}

echo ""
echo "=== Step 1: Building terragrunt-atlantis-config binary ==="

# Get current commit SHA for version
COMMIT_SHA=$(git rev-parse --short HEAD)
VERSION="dev-${COMMIT_SHA}"

echo "Building binary with version: $VERSION"
VERSION="$VERSION" make build

# Find the built binary
BINARY_PATH=$(find build -name "terragrunt-atlantis-config_*_linux_amd64" -type f | head -1)
if [ -z "$BINARY_PATH" ]; then
    echo "Error: Binary not found in build directory"
    ls -la build/ || true
    exit 1
fi

echo "Binary built at: $BINARY_PATH"

# Convert to absolute path before changing directories
BINARY_ABS_PATH="$(cd "$(dirname "$BINARY_PATH")" && pwd)/$(basename "$BINARY_PATH")"
echo "Binary absolute path: $BINARY_ABS_PATH"

echo ""
echo "=== Step 2: Preparing Atlantis Docker build context ==="
cd "$ATLANTIS_DIR"

# Copy binary to atlantis directory for Docker build
BINARY_NAME="terragrunt-atlantis-config-binary"
cp "$BINARY_ABS_PATH" "./${BINARY_NAME}"
echo "Copied binary to Atlantis build context: ./${BINARY_NAME}"

echo ""
echo "=== Step 3: Building Docker image ==="
docker build \
    --no-cache \
    --build-arg TERRAGRUNT_ATLANTIS_CONFIG_BINARY="./${BINARY_NAME}" \
    -t "${DOCKER_IMAGE}:${DOCKER_TAG}" \
    -t "${DOCKER_IMAGE}:latest-stacks" \
    .

echo ""
echo "=== Step 4: Pushing Docker image ==="
if ! docker push "${DOCKER_IMAGE}:${DOCKER_TAG}"; then
    echo "ERROR: Failed to push ${DOCKER_IMAGE}:${DOCKER_TAG}"
    exit 1
fi
if ! docker push "${DOCKER_IMAGE}:latest-stacks"; then
    echo "ERROR: Failed to push ${DOCKER_IMAGE}:latest-stacks"
    exit 1
fi
echo "Successfully pushed both tags to DockerHub"

echo ""
echo "=== Cleanup ==="
rm -f "./${BINARY_NAME}"
echo "Removed temporary binary from build context"

echo ""
echo "✅ Successfully built and pushed ${DOCKER_IMAGE}:${DOCKER_TAG}"

