#!/bin/sh
# Build into a staging directory and swap it into place, so a request arriving
# mid-build never sees a half-written dist. Vite empties its output directory
# before writing; the swap below is two renames, microseconds apart.
set -e
cd "$(dirname "$0")"
rm -rf dist.new dist.old
npx vue-tsc --noEmit
npx vite build --outDir dist.new --emptyOutDir
test -f dist.new/index.html
if [ -d dist ]; then mv dist dist.old; fi
mv dist.new dist
rm -rf dist.old
echo "swapped into place: $(ls dist/assets | tr '\n' ' ')"
