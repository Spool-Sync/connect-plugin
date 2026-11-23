#!/bin/sh
# Builds the release Prusa Connect installs: one .tar.gz with module.yaml,
# openapi.json, one binary per platform module.yaml lists, and checksums.txt,
# all at the root of the archive (a wrapping directory is refused).
#
#   sh scripts/release.sh            -> dist/spool-sync-prusa-connect-<version>.tar.gz
#   TAG=v0.1.0 sh scripts/release.sh -> also fails unless the tag is v<version>
#   SIGNING_KEY=private_key.pem sh scripts/release.sh -> also signs checksums.txt
#   DEV_BUILD=42 sh scripts/release.sh -> for a build of main that is no
#     release: version MAJOR.MINOR.(100000+42), such as 0.1.100042, written
#     into the archive's module.yaml and the binary. The host's handshake
#     takes MAJOR.MINOR.PATCH only, so a prerelease suffix cannot be used.
#
# The UI bundle is built first (pnpm install --frozen-lockfile, then pnpm build
# in ui/), and its ui/dist ships as the release's ui/. pnpm is required.
#
# Install it with
#   prusa-connect module add gitlab.honex.life/spoolsync/spool-sync-prusa-connect@<version> -from dist/<archive> -approve
set -eu

cd "$(dirname "$0")/.."

NAME=$(sed -n 's/^name: *//p' module.yaml | tr -d '"')
VERSION=$(sed -n 's/^version: *//p' module.yaml | tr -d '"')

if [ -z "$NAME" ] || [ -z "$VERSION" ]; then
  echo "release: module.yaml names no name or version" >&2
  exit 1
fi

if [ -n "${TAG:-}" ] && [ "$TAG" != "v$VERSION" ]; then
  echo "release: the tag is $TAG and module.yaml says version $VERSION. Tag v$VERSION, or change module.yaml" >&2
  exit 1
fi

# The platforms block in module.yaml: every "  - goos_goarch" line under it.
PLATFORMS=$(awk '/^platforms:/{on=1; next} on && /^ *- /{sub(/^ *- */,""); print; next} on && /^[^ #]/{on=0}' module.yaml)

if [ -z "$PLATFORMS" ]; then
  echo "release: module.yaml lists no platforms" >&2
  exit 1
fi

case "$VERSION" in
  *[!0-9.]* | *.*.*.* | .* | *.)
    echo "release: module.yaml's version $VERSION is not MAJOR.MINOR.PATCH" >&2
    exit 1 ;;
esac

if [ -n "${DEV_BUILD:-}" ]; then
  if [ -n "${TAG:-}" ]; then
    echo "release: a tag is a release, and DEV_BUILD is for builds that are not" >&2
    exit 1
  fi
  case "$DEV_BUILD" in
    '' | *[!0-9]*) echo "release: DEV_BUILD must be a whole number, not $DEV_BUILD" >&2; exit 1 ;;
  esac
  VERSION="${VERSION%.*}.$((100000 + DEV_BUILD))"
fi

OUT=dist/release
ARCHIVE=dist/$NAME-$VERSION.tar.gz

rm -rf dist
mkdir -p "$OUT"
cp openapi.json "$OUT/"
sed "s/^version: .*/version: $VERSION/" module.yaml > "$OUT/module.yaml"
echo "$VERSION" > dist/version

# The browser half, built from ui/ into ui/dist. It ships as the release's ui/,
# so module.yaml's ui.entry (ui/index.js) is a file of the archive.
if ! command -v pnpm >/dev/null 2>&1; then
  echo "release: pnpm is not on the PATH, and the UI bundle needs it (corepack enable)" >&2
  exit 1
fi
echo "build the UI bundle"
(cd ui && pnpm install --frozen-lockfile && pnpm build)
cp -R ui/dist "$OUT/ui"
if [ ! -f "$OUT/ui/index.js" ]; then
  echo "release: the UI build wrote no ui/index.js, the entry module.yaml names" >&2
  exit 1
fi

for p in $PLATFORMS; do
  os=${p%_*}
  arch=${p#*_}
  ext=
  [ "$os" = windows ] && ext=.exe
  echo "build $NAME for $os/$arch"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o "$OUT/${NAME}_$p$ext" .
done

# checksums.txt lists every other file, so it is written last.
# Every file under the release, the ui/ tree included, by its path from the
# root: the installer holds each listed file to its digest and refuses an
# unlisted one.
(cd "$OUT" && find . -type f ! -name checksums.txt ! -name checksums.txt.sig -exec sha256sum {} + \
  | sed 's#  \./#  #' | LC_ALL=C sort -k2 > checksums.txt)

# With SIGNING_KEY naming the author's RSA private key (PEM), checksums.txt is
# signed into checksums.txt.sig as Prusa Connect and the marketplace read it:
# an RSA-SHA256 signature, base64 on one line. The signature is checked with
# signing/public_key.pem, the key the marketplace holds, before it ships.
if [ -n "${SIGNING_KEY:-}" ]; then
  SIG=$(mktemp)
  openssl dgst -sha256 -sign "$SIGNING_KEY" -out "$SIG" "$OUT/checksums.txt"
  if ! openssl dgst -sha256 -verify signing/public_key.pem -signature "$SIG" "$OUT/checksums.txt" > /dev/null; then
    rm -f "$SIG"
    echo "release: SIGNING_KEY is not the private half of signing/public_key.pem" >&2
    exit 1
  fi
  { openssl base64 -A < "$SIG"; echo; } > "$OUT/checksums.txt.sig"
  rm -f "$SIG"
  echo "signed checksums.txt"
fi

(cd "$OUT" && tar -czf "../$NAME-$VERSION.tar.gz" -- *)

sha256sum "$ARCHIVE" | cut -d' ' -f1 > "$ARCHIVE.sha256"
echo "wrote $ARCHIVE"
echo "sha256 $(cat "$ARCHIVE.sha256")"
