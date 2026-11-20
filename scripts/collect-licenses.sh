#!/bin/sh
# Copies the license files of every Go module compiled into the binary to
# a folder (default: third_party), as their licenses ask for binary
# distributions. Used by the Dockerfile and the release workflow.
#
#   scripts/collect-licenses.sh [dest]
set -eu
DEST=${1:-third_party}
mkdir -p "$DEST"
go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}} {{.Dir}}{{end}}{{end}}' ./cmd/ebi-furai | sort -u |
while read -r mod dir; do
  [ -n "$dir" ] || continue
  out="$DEST/$(echo "$mod" | tr '/@' '__')"
  found=0
  for f in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE* "$dir"/PATENTS*; do
    [ -f "$f" ] || continue
    mkdir -p "$out"
    cp "$f" "$out/"
    found=1
  done
  [ "$found" = 1 ] || echo "warning: no license file for $mod" >&2
done
# The Go standard library and runtime are compiled in too.
mkdir -p "$DEST/go"
cp "$(go env GOROOT)/LICENSE" "$DEST/go/LICENSE"
[ -f "$(go env GOROOT)/PATENTS" ] && cp "$(go env GOROOT)/PATENTS" "$DEST/go/PATENTS"
echo "licenses in $DEST"
