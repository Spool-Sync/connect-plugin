#!/bin/sh
# Builds the release Prusa Connect installs: one .tar.gz with module.yaml,
# openapi.json, one binary per platform module.yaml lists, and checksums.txt,
# all at the root of the archive (a wrapping directory is refused).
#
#   sh scripts/release.sh            -> dist/spool-sync-prusa-connect-<version>.tar.gz
#   TAG=v0.1.0 sh scripts/release.sh -> also fails unless the tag is v<version>
#   SUFFIX=dev.42 sh scripts/release.sh -> version <version>-dev.42, written
#     into the archive's module.yaml, for a build of main that is no release
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

if [ -n "${SUFFIX:-}" ]; then
  if [ -n "${TAG:-}" ]; then
    echo "release: a tag is a release, and SUFFIX is for builds that are not" >&2
    exit 1
  fi
  VERSION="$VERSION-$SUFFIX"
fi

OUT=dist/release
ARCHIVE=dist/$NAME-$VERSION.tar.gz

rm -rf dist
mkdir -p "$OUT"
cp openapi.json "$OUT/"
sed "s/^version: .*/version: $VERSION/" module.yaml > "$OUT/module.yaml"
echo "$VERSION" > dist/version

for p in $PLATFORMS; do
  os=${p%_*}
  arch=${p#*_}
  ext=
  [ "$os" = windows ] && ext=.exe
  echo "build $NAME for $os/$arch"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags="-s -w" -o "$OUT/${NAME}_$p$ext" .
done

# checksums.txt lists every other file, so it is written last.
(cd "$OUT" && sha256sum -- * > checksums.txt && tar -czf "../$NAME-$VERSION.tar.gz" -- *)

sha256sum "$ARCHIVE" | cut -d' ' -f1 > "$ARCHIVE.sha256"
echo "wrote $ARCHIVE"
echo "sha256 $(cat "$ARCHIVE.sha256")"
