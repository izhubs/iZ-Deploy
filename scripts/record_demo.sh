#!/usr/bin/env bash
# Wrapper: record demo + convert to GIF automatically
# asciinema rec với --command tự exit khi script kết thúc

export PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
CAST=/tmp/izdeploy-demo.cast
GIF=/tmp/izdeploy-demo.gif
SCRIPT=/mnt/d/project/iz-deploy/scripts/demo_script.sh

echo "=== Recording demo ==="
# --command exits automatically when the script finishes (non-interactive)
asciinema rec "$CAST" \
    --command "bash $SCRIPT" \
    --cols 100 \
    --rows 32 \
    --overwrite \
    --quiet

echo "=== Converting to GIF ==="
agg "$CAST" "$GIF" \
    --speed 1.0 \
    --theme monokai \
    --font-size 14 \
    --line-height 1.4

ls -lh "$GIF"
echo "GIF_DONE: $GIF"

# Copy to project docs folder
cp "$GIF" /mnt/d/project/iz-deploy/docs/demo.gif
echo "COPIED to docs/demo.gif"
