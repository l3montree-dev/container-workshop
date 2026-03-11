# Step 4 — Multi-stage builds

```bash
docker build -f Containerfile -t oci-demo:step4 .
docker run --rm oci-demo:step4

# Compare sizes against previous steps
docker images oci-demo
```

The final image is built `FROM scratch` — it contains **only** the compiled binary. No Alpine, no gcc, no libc, no build artefacts.

```bash
docker save oci-demo:step4 -o step4.tar
tar tf step4.tar   # just one tiny layer
```

Use the workbench to inspect the merged filesystem:

```bash
container-hardening-work-bench inspect -f Containerfile -o ./merged-multistage
ls ./merged-multistage   # a single /app binary
```

> **Takeaway:** multi-stage builds give you the full power of a build environment without shipping any of it to production. Smaller attack surface, smaller image, faster pulls.
