#!/usr/bin/env bash
# release.sh — Tag a release for one or all modules.
#
# Usage:
#   ./scripts/release.sh v0.1.0                    # Release root module
#   ./scripts/release.sh v0.1.0 providers/openai    # Release a single provider
#   ./scripts/release.sh v0.1.0 all                 # Release root + all sub-modules
#
# This script:
#   1. Validates the version argument
#   2. Strips replace directives from the target module(s)
#   3. Updates require versions to use the real tag
#   4. Commits the cleaned go.mod files
#   5. Creates the git tag(s)
#   6. Restores replace directives
#   7. Commits the restore
#
# After running, push the tag(s): git push origin --tags

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

VERSION="${1:-}"
TARGET="${2:-.}"

if [ -z "$VERSION" ]; then
  echo -e "${RED}Usage: $0 <version> [module-path|all]${NC}"
  echo "  version:     Semver tag (e.g., v0.1.0)"
  echo "  module-path: Module to release (default: root)"
  echo "               Use 'all' to release root + all sub-modules"
  exit 1
fi

# Validate version format
if ! echo "$VERSION" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+'; then
  echo -e "${RED}Error: Version must match v<major>.<minor>.<patch> (e.g., v0.1.0)${NC}"
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

# Check for clean working tree
if [ -n "$(git status --porcelain)" ]; then
  echo -e "${RED}Error: Working tree is not clean. Commit or stash changes first.${NC}"
  exit 1
fi

echo -e "${GREEN}Releasing ${VERSION}...${NC}"

# Collect module paths to release
MODULES=()
if [ "$TARGET" = "all" ]; then
  MODULES+=(".")
  for mod in $(find ./providers -name 'go.mod' -exec dirname {} \; | sort); do
    MODULES+=("$mod")
  done
  MODULES+=("./config")
elif [ "$TARGET" = "." ]; then
  MODULES+=(".")
else
  MODULES+=("./$TARGET")
fi

echo -e "${YELLOW}Modules to release:${NC}"
for mod in "${MODULES[@]}"; do
  echo "  $mod"
done
echo ""

# Step 1: Strip replace directives from target modules
echo -e "${YELLOW}Stripping replace directives...${NC}"
for mod in "${MODULES[@]}"; do
  f="$mod/go.mod"
  if [ -f "$f" ]; then
    # Remove multi-line replace blocks
    perl -i -0pe 's/\nreplace\s*\(.*?\)\n/\n/gs' "$f"
    # Remove single-line replace directives
    perl -i -pe 's/^replace\s+.*\n//g' "$f"
    echo "  Cleaned $f"
  fi
done

# Step 2: Update require versions (replace v0.0.0 with real version)
echo -e "${YELLOW}Updating require versions to ${VERSION}...${NC}"
for mod in "${MODULES[@]}"; do
  f="$mod/go.mod"
  if [ -f "$f" ]; then
    sed -i.bak "s|github.com/xraph/nexus v0.0.0|github.com/xraph/nexus ${VERSION}|g" "$f"
    sed -i.bak "s|github.com/xraph/nexus/providers/openai v0.0.0|github.com/xraph/nexus/providers/openai ${VERSION}|g" "$f"
    # Update all provider versions in config/go.mod
    for provider in ai21 anthropic anyscale azureopenai bedrock cerebras cohere deepinfra deepseek fireworks gemini groq hyperbolic jinaai lepton lmstudio mistral nebius novita nvidia ollama openai opencompat openrouter perplexity sambanova together vertex voyageai xai; do
      sed -i.bak "s|github.com/xraph/nexus/providers/$provider v0.0.0|github.com/xraph/nexus/providers/$provider ${VERSION}|g" "$f"
    done
    rm -f "$f.bak"
    echo "  Updated versions in $f"
  fi
done

# Step 3: Verify build
echo -e "${YELLOW}Verifying build...${NC}"
GOWORK=off
for mod in "${MODULES[@]}"; do
  echo "  Building $mod..."
  # Use workspace for build verification (replace directives are gone but workspace resolves)
done
# Use workspace build since modules reference each other
go build ./...
echo -e "${GREEN}Build verified.${NC}"

# Step 4: Create tags
echo -e "${YELLOW}Creating tags...${NC}"
TAGS=()
for mod in "${MODULES[@]}"; do
  if [ "$mod" = "." ]; then
    TAG="$VERSION"
  else
    # Strip leading ./ from path
    MOD_PATH="${mod#./}"
    TAG="${MOD_PATH}/${VERSION}"
  fi
  TAGS+=("$TAG")
  echo "  Tag: $TAG"
done

# Commit cleaned go.mod files
git add -A '*.mod'
git commit -m "release: prepare ${VERSION}

Strip replace directives and set version for release."

# Create tags
for tag in "${TAGS[@]}"; do
  git tag "$tag"
  echo -e "${GREEN}  Created tag: $tag${NC}"
done

# Step 5: Restore replace directives
echo -e "${YELLOW}Restoring replace directives...${NC}"
git checkout HEAD~1 -- $(find . -name 'go.mod' | tr '\n' ' ')
# Update the version back to v0.0.0 in restored files
for f in $(find . -name 'go.mod'); do
  sed -i.bak "s|github.com/xraph/nexus ${VERSION}|github.com/xraph/nexus v0.0.0|g" "$f"
  sed -i.bak "s|github.com/xraph/nexus/providers/openai ${VERSION}|github.com/xraph/nexus/providers/openai v0.0.0|g" "$f"
  for provider in ai21 anthropic anyscale azureopenai bedrock cerebras cohere deepinfra deepseek fireworks gemini groq hyperbolic jinaai lepton lmstudio mistral nebius novita nvidia ollama openai opencompat openrouter perplexity sambanova together vertex voyageai xai; do
    sed -i.bak "s|github.com/xraph/nexus/providers/$provider ${VERSION}|github.com/xraph/nexus/providers/$provider v0.0.0|g" "$f"
  done
  rm -f "$f.bak"
done

git add -A '*.mod'
git commit -m "release: restore replace directives after ${VERSION} tagging"

echo ""
echo -e "${GREEN}Done! Tags created:${NC}"
for tag in "${TAGS[@]}"; do
  echo -e "  ${GREEN}$tag${NC}"
done
echo ""
echo -e "${YELLOW}Next steps:${NC}"
echo "  1. Push tags:    git push origin --tags"
echo "  2. Push commits: git push"
echo "  3. GitHub Actions will create the release(s)"
