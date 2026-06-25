#!/usr/bin/env bash
#
# Deploy a built static site to Firebase Hosting via the Hosting REST API, authenticating
# with the caller's Google access token from gcloud (`gcloud auth print-access-token`).
#
# Why this exists instead of `firebase deploy`: firebase-tools' CLI can only authenticate
# non-interactively from a `firebase login:ci` refresh token — it cannot use a service
# account key or the Workload Identity Federation credential that the rest of our pipeline
# runs on. Driving the REST API directly keeps the deploy keyless (the CI job's WIF access
# token is enough) and removes the firebase-tools dependency entirely. See docs/deploy.md.
#
# Usage: deploy-hosting.sh <site-id> [firebase-json-path]
#   <site-id>            the Hosting site id (the project's default site id == project id).
#   [firebase-json-path] defaults to ./firebase.json; its "hosting.public" gives the build
#                        dir and "hosting.rewrites" the rewrite rules (only rewrites are
#                        mapped — add redirects/headers here if firebase.json grows them).
#
# Deploy flow (https://firebase.google.com/docs/hosting/api-deploy):
#   create version -> populateFiles (declare path->hash) -> upload missing -> finalize -> release
set -euo pipefail

SITE="${1:?usage: deploy-hosting.sh <site-id> [firebase-json-path]}"
FIREBASE_JSON="${2:-firebase.json}"
API="https://firebasehosting.googleapis.com/v1beta1"

PUBLIC_DIR="$(jq -r '.hosting.public' "$FIREBASE_JSON")"
# firebase.json uses source/destination; the REST API's Rewrite uses glob/path.
REWRITES="$(jq -c '[.hosting.rewrites[]? | {glob: .source, path: .destination}]' "$FIREBASE_JSON")"
[ -d "$PUBLIC_DIR" ] || { echo "::error::public dir '$PUBLIC_DIR' not found" >&2; exit 1; }

# One token for the whole run (deploys finish well inside its lifetime). The quota project
# must be named explicitly: it's required when the token is a user credential (local runs)
# and harmless when it's a service-account/WIF token (CI). For our default site, the site
# id equals the GCP project id, so SITE doubles as the quota project.
TOKEN="$(gcloud auth print-access-token)"
AUTH="Authorization: Bearer ${TOKEN}"
QUOTA="x-goog-user-project: ${SITE}"

# api METHOD PATH [JSON_BODY] — a JSON REST call that fails the script on a non-2xx.
api() {
  local method="$1" path="$2" body="${3:-}"
  curl -sS --fail-with-body -X "$method" \
    -H "$AUTH" -H "$QUOTA" -H "Content-Type: application/json" \
    ${body:+-d "$body"} \
    "${API}/${path}"
}

echo "Creating a new Hosting version for site '${SITE}'…"
VERSION="$(api POST "sites/${SITE}/versions" "{\"config\":{\"rewrites\":${REWRITES}}}" | jq -r '.name')"
[ -n "$VERSION" ] && [ "$VERSION" != "null" ] || { echo "::error::failed to create version" >&2; exit 1; }
echo "  -> ${VERSION}"

# Manifest: each served path -> sha256 of its GZIPPED bytes (Hosting stores gzipped blobs
# keyed by that hash, so unchanged files are skipped on re-deploy). Stash each blob by hash.
echo "Hashing files under '${PUBLIC_DIR}'…"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
files_json="{}"
declare -A blob_by_hash
while IFS= read -r -d '' f; do
  gz="${tmp}/blob.$$"
  gzip -n -c "$f" > "$gz"
  hash="$(sha256sum "$gz" | cut -d' ' -f1)"
  mv "$gz" "${tmp}/${hash}"
  blob_by_hash["$hash"]="${tmp}/${hash}"
  rel="/${f#"${PUBLIC_DIR}"/}"
  files_json="$(jq -c --arg p "$rel" --arg h "$hash" '. + {($p): $h}' <<<"$files_json")"
done < <(find "$PUBLIC_DIR" -type f -print0)

echo "Declaring $(jq 'length' <<<"$files_json") files to Hosting…"
populate="$(api POST "${VERSION}:populateFiles" "{\"files\":${files_json}}")"
upload_url="$(jq -r '.uploadUrl' <<<"$populate")"
mapfile -t required < <(jq -r '.uploadRequiredHashes // [] | .[]' <<<"$populate")

echo "Uploading ${#required[@]} new/changed files…"
for h in "${required[@]}"; do
  curl -sS --fail-with-body -X POST \
    -H "$AUTH" -H "$QUOTA" -H "Content-Type: application/octet-stream" \
    --data-binary "@${blob_by_hash[$h]}" \
    "${upload_url}/${h}" > /dev/null
done

echo "Finalizing and releasing…"
api PATCH "${VERSION}?update_mask=status" '{"status":"FINALIZED"}' > /dev/null
api POST "sites/${SITE}/releases?versionName=${VERSION}" > /dev/null
echo "Released to https://${SITE}.web.app"
