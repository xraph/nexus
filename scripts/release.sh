#!/usr/bin/env bash
# release.sh — Tag a release for one or more modules.
#
# Usage:
#   ./scripts/release.sh v0.1.0                          # Release root module
#   ./scripts/release.sh v0.1.0 providers/openai          # Release a single provider (full path)
#   ./scripts/release.sh v0.1.0 openai                    # Release a single provider (short name)
#   ./scripts/release.sh v0.1.0 openai,anthropic,gemini   # Release multiple providers
#   ./scripts/release.sh v0.1.0 config                    # Release config module
#   ./scripts/release.sh v0.1.0 all                       # Release root + config + all providers
#
# This script:
#   1. Validates the version argument
#   2. Resolves short provider names to full paths
#   3. Strips replace directives from the target module(s)
#   4. Updates require versions to use the real tag
#   5. Commits the cleaned go.mod files
#   6. Creates the git tag(s)
#   7. Restores replace directives
#   8. Commits the restore
#
# After running, push the tag(s): git push origin --tags

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

KNOWN_PROVIDERS="ai21 anthropic anyscale azureopenai bedrock cerebras cohere deepinfra deepseek fireworks gemini groq hyperbolic jinaai lepton lmstudio mistral nebius novita nvidia ollama openai opencompat openrouter perplexity sambanova together vertex voyageai xai"

VERSION="${1:-}"
TARGET="${2:-.}"

if [ -z "$VERSION" ]; then
  echo -e "${RED}Usage: $0 <version> [modules]${NC}"
  echo ""
  echo "  version:  Semver tag (e.g., v0.1.0)"
  echo "  modules:  What to release (default: root)"
  echo ""
  echo "  Examples:"
  echo "    $0 v0.1.0                          # Root module only"
  echo "    $0 v0.1.0 openai                   # Single provider (short name)"
  echo "    $0 v0.1.0 providers/openai          # Single provider (full path)"
  echo "    $0 v0.1.0 openai,anthropic,gemini   # Multiple providers"
  echo "    $0 v0.1.0 config                    # Config module"
  echo "    $0 v0.1.0 all                       # Root + config + all providers"
  echo ""
  echo "  Available providers:"
  echo "    $KNOWN_PROVIDERS"
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

# Resolve a module name to its directory path.
# Short provider names (e.g., "openai") are expanded to "./providers/openai".
resolve_module() {
  local name="$1"
  case "$name" in
    .)          echo "." ;;
    config)     echo "./config" ;;
    providers/*) echo "./$name" ;;
    ./*)        echo "$name" ;;
    *)
      # Assume it's a short provider name
      if echo "$KNOWN_PROVIDERS" | grep -qw "$name"; then
        echo "./providers/$name"
      else
        echo -e "${RED}Error: Unknown module '$name'. Must be 'config', 'all', a provider name, or a path like 'providers/openai'.${NC}" >&2
        echo -e "Available providers: $KNOWN_PROVIDERS" >&2
        exit 1
      fi
      ;;
  esac
}

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
  # Support comma-separated list: "openai,anthropic,gemini"
  IFS=',' read -ra TARGETS <<< "$TARGET"
  for t in "${TARGETS[@]}"; do
    t=$(echo "$t" | xargs) # trim whitespace
    MODULES+=("$(resolve_module "$t")")
  done
fi

# Verify all module directories exist
for mod in "${MODULES[@]}"; do
  if [ ! -f "$mod/go.mod" ]; then
    echo -e "${RED}Error: go.mod not found at $mod${NC}"
    exit 1
  fi
done

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
    for provider in $KNOWN_PROVIDERS; do
      sed -i.bak "s|github.com/xraph/nexus/providers/$provider v0.0.0|github.com/xraph/nexus/providers/$provider ${VERSION}|g" "$f"
    done
    sed -i.bak "s|github.com/xraph/nexus/config v0.0.0|github.com/xraph/nexus/config ${VERSION}|g" "$f"
    rm -f "$f.bak"
    echo "  Updated versions in $f"
  fi
done

# Step 3: Verify build
echo -e "${YELLOW}Verifying build...${NC}"
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
  for provider in $KNOWN_PROVIDERS; do
    sed -i.bak "s|github.com/xraph/nexus/providers/$provider ${VERSION}|github.com/xraph/nexus/providers/$provider v0.0.0|g" "$f"
  done
  sed -i.bak "s|github.com/xraph/nexus/config ${VERSION}|github.com/xraph/nexus/config v0.0.0|g" "$f"
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
