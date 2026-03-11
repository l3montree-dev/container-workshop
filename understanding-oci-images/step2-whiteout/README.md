# Whiteout files and layer pollution

## The problem: `rm` in a separate layer

```bash
mkdir -p tmp
docker build -f Containerfile -t oci-demo:step2-whiteout .
docker save oci-demo:step2-whiteout -o ./tmp/step2-whiteout.tar
```

Inspect each layer individually:

```bash
mkdir -p tmp/whiteout-inspect
tar xf tmp/step2-whiteout.tar -C tmp/whiteout-inspect
ls tmp/whiteout-inspect

# Look at the second layer (the rm layer) — find whiteout markers
cat tmp/whiteout-inspect/manifest.json | jq -r '.[0].Layers[]' | while read layer_path; do
  digest="${layer_path##*/}"
  dest="tmp/whiteout-inspect/blobs/sha256/${digest}-rootfs"
  mkdir -p "$dest"
  tar xzf "tmp/whiteout-inspect/$layer_path" -C "$dest"
  echo "=== $digest ==="
  ls "$dest"
done
```

Whiteout files (`.wh.<name>`) tell the union filesystem to hide the file at runtime — but the **original file still lives in the lower layer** and is shipped with the image. Deleting a file in a later layer never reduces image size.

Use the workbench to see the merged view (whiteouts already resolved):

```bash
container-hardening-work-bench inspect -f Containerfile -o ./tmp/merged
```

---

## The fix: single `RUN` layer (`Containerfile.fix`)

Combine install and cleanup into one `RUN` so the deleted files never enter any layer at all.

```bash
docker build -f Containerfile.fix -t oci-demo:step2-combined .
docker save oci-demo:step2-combined -o ./tmp/step2-combined.tar
```

Confirm no whiteout markers exist:

```bash
mkdir -p tmp/combined-inspect
tar xf ./tmp/step2-combined.tar -C tmp/combined-inspect

cat tmp/combined-inspect/manifest.json | jq -r '.[0].Layers[]' | while read layer_path; do
  digest="${layer_path##*/}"
  dest="tmp/combined-inspect/blobs/sha256/${digest}-rootfs"
  mkdir -p "$dest"
  tar xzf "tmp/combined-inspect/$layer_path" -C "$dest"
  echo "=== $digest ==="
  ls "$dest"
done
```

> **Takeaway:** always combine related install and cleanup commands in a single `RUN` statement, chained with `&&`. A file deleted in a later layer still occupies space in the image.
