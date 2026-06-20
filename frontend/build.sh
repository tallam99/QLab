#!/usr/bin/env sh
# Minimal hello-world "build": copy the static page to dist/ and inject the API
# base URL for the target environment (so the page can call the API cross-origin
# and prove CORS). This placeholder is replaced by the real Vite build in Phase 9;
# until then it keeps the deploy target stable at frontend/dist.
#
# Usage: API_BASE_URL=https://api-staging-... frontend/build.sh
set -eu

dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
src="$dir/public"
out="$dir/dist"
api_base_url="${API_BASE_URL:-}"

rm -rf "$out"
mkdir -p "$out"
cp -R "$src/." "$out/"

# Inject the API base URL. The .bak form of -i works on both GNU and BSD sed.
sed -i.bak "s|__API_BASE_URL__|${api_base_url}|g" "$out/index.html"
rm -f "$out/index.html.bak"

echo "built frontend -> $out (API_BASE_URL=${api_base_url:-<unset>})"
