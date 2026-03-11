# Understanding OCI Images

A hands-on walkthrough of how container images actually work under the hood.

---

## Steps

| Step | Topic | Folder |
|---|---|---|
| 1 | OCI images are just tarballs — explore the layout | [step1-basic/](step1-basic/README.md) |
| 2 | Secrets in ENV vars — and how to fix it with BuildKit secret mounts | [step2-secrets/](step2-secrets/README.md) |
| 3 | Whiteout files and layer pollution — and the single-`RUN` fix | [step3-whiteout/](step3-whiteout/README.md) |
| 4 | Multi-stage builds — shipping only what you need | [step4-multistage/](step4-multistage/README.md) |
| 5 | Multi-stage as the universal fix — secrets + whiteouts gone | [step5-multistage-complex/](step5-multistage-complex/README.md) |

---

## Summary

| Anti-pattern | What goes wrong | Fix |
|---|---|---|
| `ENV SECRET=...` | Secret baked into image manifest forever | `--mount=type=secret` or runtime injection |
| `RUN install` then `RUN rm` | Deleted files still in lower layer → whiteout bloat | Combine into one `RUN` |
| Single-stage build with dev tools | Compiler, headers, caches shipped to prod | Multi-stage build |
| All of the above | Bloated, leaky, oversized image | Multi-stage: only `COPY` what you need into a clean final stage |
