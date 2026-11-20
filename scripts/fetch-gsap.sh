#!/bin/sh
# Downloads GSAP from the npm registry into web/static/vendor. GSAP is free
# to use but not open source, so it is not kept in the repository; the
# build embeds it. The checksum makes sure it is the expected file.
#
#   scripts/fetch-gsap.sh            (from the root of the repository)
#
# To update GSAP: change VERSION and SHA256 here, in the Dockerfile and the
# file name in web/static/index.html and web/static/sw.js.
set -eu
VERSION=3.15.1
SHA256=3d1bea374ab1b703cc5c4105358eec63c4465efdc573931dc116e34a0a190c1b
DEST="web/static/vendor/gsap-$VERSION.min.js"

if [ -f "$DEST" ] && echo "$SHA256  $DEST" | sha256sum -c - >/dev/null 2>&1; then
  exit 0
fi
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
url="https://registry.npmjs.org/gsap/-/gsap-$VERSION.tgz"
if command -v curl >/dev/null 2>&1; then curl -fsSL "$url" -o "$tmp/gsap.tgz"; else wget -qO "$tmp/gsap.tgz" "$url"; fi
tar -xzf "$tmp/gsap.tgz" -C "$tmp" package/dist/gsap.min.js
echo "$SHA256  $tmp/package/dist/gsap.min.js" | sha256sum -c - >/dev/null
cp "$tmp/package/dist/gsap.min.js" "$DEST"
echo "GSAP $VERSION -> $DEST"
