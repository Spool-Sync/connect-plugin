#!/bin/sh
# Builds the release Prusa Connect installs: one .tar.gz with module.yaml,
# openapi.json, one binary per platform module.yaml lists, and checksums.txt,
# all at the root of the archive (a wrapping directory is refused).
#
#   sh scripts/release.sh            -> dist/spool-sync-prusa-connect-<version>.tar.gz
#   TAG=v0.1.0 sh scripts/release.sh -> also fails unless the tag is v<version>
#   DEV_BUILD=42 sh scripts/release.sh -> for a build of main that is no
#     release: version MAJOR.MINOR.(100000+42), such as 0.1.100042, written
#     into the archive's module.yaml and the binary. The host's handshake
#     takes MAJOR.MINOR.PATCH only, so a prerelease suffix cannot be used.
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

for p in $PLATFORMS; do
  os=${p%_*}
  arch=${p#*_}
  ext=
  [ "$os" = windows ] && ext=.exe
  echo "build $NAME for $os/$arch"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o "$OUT/${NAME}_$p$ext" .
done

# checksums.txt lists every other file, so it is written last.
(cd "$OUT" && sha256sum -- * > checksums.txt && tar -czf "../$NAME-$VERSION.tar.gz" -- *)

sha256sum "$ARCHIVE" | cut -d' ' -f1 > "$ARCHIVE.sha256"
echo "wrote $ARCHIVE"
echo "sha256 $(cat "$ARCHIVE.sha256")"
