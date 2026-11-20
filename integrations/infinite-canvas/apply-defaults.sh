#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
    echo "Usage: sh apply-defaults.sh /path/to/infinite-canvas" >&2
    exit 1
fi

canvas_dir=$(CDPATH= cd "$1" && pwd)
canvas_dir=$(git -C "$canvas_dir" rev-parse --show-toplevel)
integration_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
expected_revision=dab19adc0847e32e39b7fc8ff90cb392561fb826
actual_revision=$(git -C "$canvas_dir" rev-parse HEAD)

if [ "$actual_revision" != "$expected_revision" ]; then
    echo "Expected canvas revision $expected_revision; found $actual_revision." >&2
    exit 1
fi

media_patch="$integration_dir/media-assets.patch"
subpath_patch="$integration_dir/subpath.patch"

if git -C "$canvas_dir" apply --reverse --check "$media_patch" 2>/dev/null; then
    echo "Gateway defaults, Seedance, sub-path and unified media support are already applied."
    exit 0
fi

# Upgrade an existing three-patch installation without resetting local work.
# git apply checks every hunk first and refuses conflicts before changing files.
if git -C "$canvas_dir" apply --reverse --check "$subpath_patch" 2>/dev/null; then
    git -C "$canvas_dir" apply --check "$media_patch"
    git -C "$canvas_dir" apply "$media_patch"
    echo "Unified media support applied to the existing canvas integration. Rebuild before deployment."
    exit 0
fi

if [ -n "$(git -C "$canvas_dir" status --porcelain)" ]; then
    echo "$canvas_dir is not a pristine or recognized integrated checkout at $expected_revision." >&2
    echo "Use a separate clean checkout at the pinned revision; preserve your current changes." >&2
    exit 1
fi

# Preflight the whole dependent stack in a disposable index before any writes.
media_index=$(mktemp)
rm "$media_index"
trap 'rm -f "$media_index"' EXIT HUP INT TERM
GIT_INDEX_FILE="$media_index" git -C "$canvas_dir" read-tree HEAD
for patch_name in defaults seedance subpath media-assets; do
    GIT_INDEX_FILE="$media_index" git -C "$canvas_dir" apply --cached "$integration_dir/$patch_name.patch"
done
for patch_name in defaults seedance subpath media-assets; do
    git -C "$canvas_dir" apply "$integration_dir/$patch_name.patch"
done
echo "Gateway defaults, Seedance, /canvas/ sub-path and unified media support applied. Rebuild before deployment."
