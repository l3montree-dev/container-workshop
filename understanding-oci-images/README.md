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

## Root privileges in Docker builds and associated risks

### The daemon model

The Docker daemon (`dockerd`) runs as **root** by default. Every `docker build` sends the build context and all instructions to that daemon. Consequences:

- Any user in the `docker` group has **effective root access to the host** — adding someone to `docker` is equivalent to adding them to `sudo`.
- The Unix socket `/var/run/docker.sock` is the attack surface. A process (or container) with access to it can start privileged containers, mount host paths, and fully escape to the host.
- `RUN` instructions inside a build execute as root inside the build container by default. A supply-chain attack through a malicious base image or package can do anything root can do within that container.

### Secrets and the daemon

Even with `--mount=type=secret`, the secret is decrypted and available to the daemon process (running as root) while the `RUN` step executes. It is never written to a layer, but it does pass through the root-owned daemon process. On a shared multi-tenant build host this matters.

`--build-arg` and `ENV` are worse: they are sent to the daemon in plain text over the socket, stored in the layer, and logged in the build history.

### Mitigations

| Approach | How it helps |
|---|---|
| **Rootless Docker** (`dockerd-rootless-setuptool.sh`) | Runs the daemon as a non-root user using user namespaces. Greatly reduces host impact if the daemon is compromised. |
| **Rootless Podman** | Daemon-less; each build runs fully in the calling user's namespace without any privileged process. |
| **Buildah** | Builds OCI images without a daemon, rootless by design. |
| **Kaniko** | Runs builds inside an unprivileged container (used in CI). No host daemon required. |
| **Nix** | Hermetic, reproducible builds in a sandboxed environment; no Docker daemon involved at all. |

For CI/CD pipelines in particular, prefer a daemonless or rootless builder so that a compromised build step cannot escalate to host root.

---

## Summary

| Anti-pattern | What goes wrong | Fix |
|---|---|---|
| `ENV SECRET=...` | Secret baked into image manifest forever | `--mount=type=secret` or runtime injection |
| `RUN install` then `RUN rm` | Deleted files still in lower layer → whiteout bloat | Combine into one `RUN` |
| Single-stage build with dev tools | Compiler, headers, caches shipped to prod | Multi-stage build |
| All of the above | Bloated, leaky, oversized image | Multi-stage: only `COPY` what you need into a clean final stage |
