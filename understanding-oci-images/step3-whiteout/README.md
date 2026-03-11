# Step 3 — Whiteout files and layer pollution

## The problem: `rm` in a separate layer

```bash
docker build -f Containerfile -t oci-demo:step3-whiteout .
docker save oci-demo:step3-whiteout -o step3-whiteout.tar
```

Inspect each layer individually:

```bash
mkdir -p tmp/whiteout-inspect
tar xf step3-whiteout.tar -C tmp/whiteout-inspect
ls tmp/whiteout-inspect

# Look at the second layer (the rm layer) — find whiteout markers
for layer_dir in tmp/whiteout-inspect/*/; do
  tar tf "${layer_dir}layer.tar" 2>/dev/null | grep -i '\.wh\.' && echo "  ^ in $layer_dir"
done
```

Whiteout files (`.wh.<name>`) tell the union filesystem to hide the file at runtime — but the **original file still lives in the lower layer** and is shipped with the image. Deleting a file in a later layer never reduces image size.

Use the workbench to see the merged view (whiteouts already resolved):

```bash
container-hardening-work-bench inspect -f Containerfile -o ./merged-whiteout
```

---

## The fix: single `RUN` layer (`Containerfile.fix`)

Combine install and cleanup into one `RUN` so the deleted files never enter any layer at all.

```bash
docker build -f Containerfile.fix -t oci-demo:step3-combined .
docker save oci-demo:step3-combined -o step3-combined.tar

# Compare sizes
docker images oci-demo
```

Confirm no whiteout markers exist:

```bash
mkdir -p tmp/combined-inspect
tar xf step3-combined.tar -C tmp/combined-inspect

for layer_dir in tmp/combined-inspect/*/; do
  tar tf "${layer_dir}layer.tar" 2>/dev/null | grep -i '\.wh\.'
done
# → (no output)
```

> **Takeaway:** always combine related install and cleanup commands in a single `RUN` statement, chained with `&&`. A file deleted in a later layer still occupies space in the image.
