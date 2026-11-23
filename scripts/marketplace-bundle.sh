#!/bin/sh
# Builds the browser bundle in the layout the marketplace's builder takes:
# module.yaml and openapi.json at the root of marketplace-bundle/, the bundle
# under ui/. The builder runs it in a job with no secret and builds the
# program itself. pnpm is the version ui/package.json pins.
set -eu

cd "$(dirname "$0")/.."

PNPM=$(node -p "require('./ui/package.json').packageManager")
pnpm() { npx --yes "$PNPM" "$@"; }

(cd ui && pnpm install --frozen-lockfile && pnpm run build)

rm -rf marketplace-bundle
mkdir -p marketplace-bundle
cp -R ui/dist marketplace-bundle/ui
cp module.yaml openapi.json LICENSE README.md CHANGELOG.md marketplace-bundle/
