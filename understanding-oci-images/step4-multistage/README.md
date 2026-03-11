# Step 4 — Multi-stage builds

```bash
mkdir -p tmp
docker build -f Containerfile -t oci-demo:step4 .
docker run --rm oci-demo:step4

# Compare sizes against previous steps
docker images oci-demo
```

The final image is built `FROM scratch` — it contains **only** the compiled binary. No Alpine, no gcc, no libc, no build artefacts.

```bash
docker save oci-demo:step4 -o ./tmp/step4.tar
mkdir -p tmp/step4-inspect
tar xf tmp/step4.tar -C ./tmp/step4-inspect
ls ./tmp/step4-inspect

cat tmp/step4-inspect/manifest.json | jq -r '.[0].Layers[]' | while read layer_path; do
  digest="${layer_path##*/}"
  dest="tmp/step4-inspect/blobs/sha256/${digest}-rootfs"
  mkdir -p "$dest"
  tar xzf "tmp/step4-inspect/$layer_path" -C "$dest"
  echo "=== $digest ==="
  ls "$dest"
done
```

Use the workbench to inspect the merged filesystem:

```bash
container-hardening-work-bench inspect -f Containerfile -o ./tmp/merged-multistage
ls ./tmp/merged-multistage   # a single /app binary
```

> **Takeaway:** multi-stage builds give you the full power of a build environment without shipping any of it to production. Smaller attack surface, smaller image, faster pulls.
