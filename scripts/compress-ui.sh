#!/bin/sh
# Gzip the dashboard's hashed text assets in place so the tarn binary embeds
# them compressed. internal/api/ui.go serves each .gz with Content-Encoding,
# inflating for the rare client that does not accept gzip. 200.html stays
# uncompressed: git tracks a placeholder copy of it.
set -eu

dir="${1:-internal/api/ui-dist}"
if [ ! -d "$dir/_app" ]; then
	echo "compress-ui: no $dir/_app, nothing to compress"
	exit 0
fi

find "$dir/_app" -type f \( -name '*.js' -o -name '*.css' -o -name '*.svg' -o -name '*.json' \) \
	-exec gzip -9 -n -f {} +
