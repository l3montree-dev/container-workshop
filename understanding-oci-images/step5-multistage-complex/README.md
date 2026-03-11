# Step 5 — Multi-stage as the universal fix (secrets + whiteouts)

The previous steps showed three problems in isolation. This step combines all of them into a single builder stage, then proves the final image is clean.

```bash
docker build -f Containerfile -t oci-demo:step5 .
docker run --rm oci-demo:step5
```

## Prove the secret is gone from the final image

```bash
docker save oci-demo:step5 -o step5.tar

CONFIG=$(tar xf step5.tar manifest.json -O | jq -r '.[0].Config')
tar xf step5.tar "$CONFIG" -O | jq '.config.Env'
# → null  (no ENV entries at all)
```

Compare with the builder stage — save it separately to show the contrast:

```bash
docker build -f Containerfile --target builder -t oci-demo:step5-builder .
docker save oci-demo:step5-builder -o step5-builder.tar

CONFIG=$(tar xf step5-builder.tar manifest.json -O | jq -r '.[0].Config')
tar xf step5-builder.tar "$CONFIG" -O | jq '.config.Env'
# → ["BUILD_TOKEN=ghp_super_secret_build_token_abc123"]  ← secret visible here
```

## Prove the whiteout files are gone from the final image

```bash
mkdir -p tmp/step5-inspect
tar xf step5.tar -C tmp/step5-inspect

# Search all layers for whiteout markers
for layer_dir in tmp/step5-inspect/*/; do
  tar tf "${layer_dir}layer.tar" 2>/dev/null | grep '\.wh\.'
done
# → (no output — zero whiteout files)
```

Do the same for the builder image and you'll see whiteout files from the separate `RUN rm` layer.

## Inspect the merged filesystem

```bash
container-hardening-work-bench inspect -f Containerfile -o ./merged-step5
ls ./merged-step5
# → just /app
```

The entire merged filesystem is a single binary — no secrets, no whiteout pollution, no build tooling.

> **Takeaway:** multi-stage builds don't just remove build tools — they discard every ENV, every layer, and every whiteout file from previous stages. Only what you explicitly `COPY --from=` crosses the boundary.
