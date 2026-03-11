# Docker Namespaces + Privilege Escalation

A minimal Go program that demonstrates real vs effective UID — the kernel mechanism behind setuid binaries and why Docker chose a daemon model instead.

## Build and run

```bash
mkdir -p tmo
go build -o ./tmp/setuid-demo .

# Without setuid — both UIDs are yours
./tmp/setuid-demo
# real uid:      1000
# effective uid: 1000
# → both UIDs match — running as yourself, no privilege escalation
```

## Apply the setuid bit

> **VM / shared folder note:** the binary must be on a filesystem mounted *without* `nosuid`.
> - `/tmp` on Ubuntu is a `tmpfs` with `nosuid` — setuid is silently ignored there.
> - Your macOS home shared into Lima via virtiofs also cannot store Linux UID 0 ownership.
> - Use `/var/tmp` (ext4, no `nosuid`) instead.

```bash
# Inside a linux machine or VM, copy to /var/tmp first
cp ./tmp/setuid-demo /var/tmp && cd /var/tmp

sudo chown root setuid-demo
sudo chmod u+s  setuid-demo

ls -la setuid-demo
# -rwsr-xr-x  root  ...
#     ^
#     's' = setuid bit: run as file owner (root), not as the calling user

# Now run as your normal user
./setuid-demo
# real uid:      1000   ← still you
# effective uid: 0      ← kernel treats this process as root
```

## What is actually happening

When the kernel `exec`s a binary with the setuid bit set, it switches the **effective UID** to the file owner before your code runs. The real UID stays as the caller. The process therefore has the privileges of the owner (root here), not the user who ran it.

Classic real-world example: `/usr/bin/passwd`. A normal user can run it, but it needs root to write `/etc/shadow`. The setuid bit on the binary is what grants that.

## Why Docker uses a daemon instead

A setuid binary that manages overlay mounts, network namespaces, and cgroups is a huge privileged attack surface — every bug in it is a **local root exploit** available to any user on the machine. Docker's original answer was to move all the privileged code into one long-lived root process (the daemon) and expose a defined API over a Unix socket, so the attack surface is at least bounded.

### Why not start rootless from day one?

User namespaces — the kernel feature that makes rootless containers possible — were not ready when Docker launched.

| Date | Event | Source |
|---|---|---|
| 2013-02-18 | **Linux 3.8** released — user namespaces merged, but implementation incomplete and full of privilege escalation CVEs | [kernelnewbies.org/Linux_3.8](https://kernelnewbies.org/Linux_3.8) |
| 2013-03-23 | **Docker 0.1** released — user namespaces too new and dangerous to rely on | [github.com/moby/moby releases](https://github.com/moby/moby/releases/tag/v0.1.0) |
| 2014-12-07 | **Linux 3.18** — OverlayFS merged into mainline; Docker had been using out-of-tree AUFS | [kernelnewbies.org/Linux_3.18](https://kernelnewbies.org/Linux_3.18) |
| 2016–2022 | Most distros **disabled unprivileged user namespaces by default** due to ongoing CVEs; Ubuntu added `kernel.unprivileged_userns_clone=0` | [Ubuntu bug #1555338](https://bugs.launchpad.net/ubuntu/+source/linux/+bug/1555338) |
| 2019-07 | **Rootless Docker** released as experimental, using `fuse-overlayfs` as a workaround for missing kernel support | [moby/moby#38050](https://github.com/moby/moby/issues/38050) |
| 2021-02-14 | **Linux 5.11** — unprivileged OverlayFS merged; rootless containers no longer need `fuse-overlayfs` workaround | [kernelnewbies.org/Linux_5.11](https://kernelnewbies.org/Linux_5.11) |
| 2021-08 | **Rootless Docker graduates to stable** in Docker Engine 20.10 | [docs.docker.com/engine/security/rootless](https://docs.docker.com/engine/security/rootless/) |
| 2023-10 | **Ubuntu 23.10+** ships with `kernel.apparmor_restrict_unprivileged_userns=1` — even today, distros restrict this by default | [Ubuntu Blog: Unprivileged user namespace restrictions](https://ubuntu.com/blog/ubuntu-23-10-restricted-unprivileged-user-namespaces) |

The daemon model was the pragmatic choice in 2013: concentrate all privileged operations in one controlled root process rather than trusting a kernel feature that was unstable and disabled on most production systems.

The trade-off: anyone with write access to `/var/run/docker.sock` has effective root. Adding a user to the `docker` group is functionally equivalent to adding them to `sudo`.

> **Only trusted users should be allowed to control your Docker daemon.** This is a direct consequence of some powerful Docker features. Specifically, Docker allows you to share a directory between the Docker host and a guest container without limiting the access rights of the container. This means you can start a container where `/host` is the `/` directory on your host — and the container can alter your host filesystem without any restriction. This is similar to how virtualization systems allow filesystem resource sharing: nothing prevents you from sharing your root filesystem (or even your root block device) with a virtual machine. The difference is that with Docker there is no hypervisor boundary — it is just a process with a bind mount.
>
> Concretely: `docker run -v /:/host --rm alpine chroot /host` gives you a root shell on the host. No exploit required — just socket access.

The practical consequence: treat `/var/run/docker.sock` with the same sensitivity as an SSH private key for root.

---

## User namespaces: the modern solution

On Linux, UID 0 (root) is special — the kernel checks it everywhere. Containers share the host kernel, so a process that is UID 0 inside a container *is* UID 0 on the host kernel. That's why container escapes are so dangerous.

**User namespaces let the kernel lie about UIDs.** When you create a user namespace, you give it a mapping like:

```
container UID 0     →  host UID 100000
container UID 1     →  host UID 100001
...
container UID 65535 →  host UID 165535
```

Inside the namespace, the process *thinks* it is root (UID 0) and can do root-like things within its own namespace. But the host kernel sees it as UID 100000 — an ordinary unprivileged user. If it escapes the container, it lands on the host as UID 100000: harmless.

### Capability scoping: why they're safe

Every kernel resource (Files and directories, mounted filesystems, network interfaces, processes, cgroups, ipc objects) has an **owning user namespace** — the namespace that was current when the resource was created. The kernel's capability check is always:

> "Does the process have this capability **in the namespace that owns the target resource**?"

When Linux boots, there is one user namespace — the **initial namespace** (`init_user_ns`). Every file on disk, every network interface, every mounted filesystem was created under it. When you call `unshare --user`, you create a child namespace:

```
init_user_ns  ← owns everything that existed at boot
  │           ← files, mounts, network interfaces, host PIDs
  │
  └─ your_user_ns
       owner = UID 501 (your host UID)
       mapping: namespace UID 0 → host UID 501
```

Your `CAP_SYS_ADMIN` only reaches the subtree below `your_user_ns`. The host's resources are owned by `init_user_ns` — they are unreachable.

```bash
# On Ubuntu, apparmor may block unprivileged user namespaces — check first:
sysctl kernel.apparmor_restrict_unprivileged_userns
# If non-zero, enable with:
sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0

unshare --user --map-root-user sh
# You appear as root (UID 0), have CAP_SYS_ADMIN in your_user_ns

# ALLOWED — /var/tmp is world-writable, you effectively own it
touch /var/tmp/test

# REJECTED — /mnt is owned by host root (UID 0), which has no mapping
# here and therefore shows up as nobody/nogroup
touch /mnt/test
# touch: cannot touch '/mnt/test': Permission denied
```

The kernel never lets a child namespace's capabilities bubble up to affect resources owned by a parent namespace. Your "root" inside the namespace is **structurally cut off** from the host — not just by policy, but by the kernel's ownership model.

### Why older kernels got this wrong

The CVEs from 2013–2018 were mostly cases where a specific kernel subsystem forgot to check namespace ownership for some resource. Examples:

- `CAP_NET_ADMIN` inside a user namespace was used to manipulate host routing tables because the routing code skipped the ownership check.
- `ptrace` across namespace boundaries could read host process memory because the ptrace path didn't verify the namespace relationship.

Each CVE was a missing ownership check in one subsystem — not a flaw in the design, but in the implementation. The concept was sound; every code path had to be individually audited. That's why distros disabled the feature for nearly a decade while the kernel caught up.

---

## Rootless Docker and Podman

### Rootless Docker architecture

Traditional Docker has two privileged components — `dockerd` and `containerd` — both running as host root. Rootless mode moves the entire stack into a user namespace:

```
host (your user, UID 1000)
  └─ rootlesskit        ← sets up user namespace + network namespace
       └─ dockerd       ← runs inside namespace, thinks it's root
            └─ containerd
                 └─ your container (UID 0 inside = UID 100000+ on host)
```

`rootlesskit` calls `newuidmap`/`newgidmap` (small setuid helpers that ship with the OS) to write the UID maps, then drops its own privileges. After that, no setuid or root is involved anywhere.

### What you gain vs what you lose

| | Traditional Docker | Rootless Docker |
|---|---|---|
| Daemon runs as | host root | your UID |
| Socket compromise | full host root | your UID only |
| Container escape | full host root | your UID only |
| Overlay filesystem | native OverlayFS | `fuse-overlayfs` (slower) |
| Port binding < 1024 | works | needs `sysctl net.ipv4.ip_unprivileged_port_start` |

### Why Podman goes further

Podman has no daemon at all. Each `podman run` is just a regular process in your user namespace — there's nothing listening on a socket to compromise. The attack surface reduces to the process itself and the kernel's namespace implementation.

The fundamental insight: **containers don't need root, they need namespaces** — and user namespaces let unprivileged processes create all the other namespaces they need.

---

## Demonstrating rootless namespace creation (Linux only)

### Step 1 — Create all container namespaces unprivileged

```bash
# Combine user ns with pid, net, mount, and uts namespaces in one shot
unshare --user --map-root-user --pid --net --mount --uts --fork --mount-proc sh

# Inside: fully isolated namespace stack, no root required
id                          # uid=0(root)
hostname container-demo     # own UTS namespace — won't affect host
hostname                    # container-demo
ip link                     # only sees lo — isolated network namespace
ls /proc                    # own PID namespace — only sees its own processes

exit
# Back on host: hostname unchanged, network unchanged
```

### Step 2 — Inspect the UID mapping that newuidmap/newgidmap write

```bash
# Start a user namespace in the background
unshare --user --map-root-user sleep 60 &
PID=$!

# Read the UID and GID maps the kernel is using
cat /proc/$PID/uid_map
# 0  1000  1

cat /proc/$PID/gid_map
# 0  1000  1

# On the host, this process is still your UID
ps -o uid,pid,comm -p $PID
# UID   PID  COMMAND
# 1000  ...  sleep

kill $PID
```

### Step 3 — List all namespaces on the system

```bash
# lsns shows every namespace and which process owns it
lsns

# Filter to user namespaces only
lsns --type user
```

## References

### Linux kernel / syscalls
- [`execve(2)`](https://man7.org/linux/man-pages/man2/execve.2.html) — how the kernel applies the setuid bit when loading a binary
- [`getuid(2)` / `geteuid(2)`](https://man7.org/linux/man-pages/man2/getuid.2.html) — real vs effective UID syscalls
- [`credentials(7)`](https://man7.org/linux/man-pages/man7/credentials.7.html) — full explanation of process credentials (real, effective, saved UIDs/GIDs)
- [`user_namespaces(7)`](https://man7.org/linux/man-pages/man7/user_namespaces.7.html) — how rootless containers remap UIDs without privilege escalation
- [`capabilities(7)`](https://man7.org/linux/man-pages/man7/capabilities.7.html) — the modern replacement for blanket root: fine-grained privilege splitting

### Docker
- [Docker security: daemon attack surface](https://docs.docker.com/engine/security/#docker-daemon-attack-surface) — official docs on the socket risk
- [Run the Docker daemon as a non-root user (rootless mode)](https://docs.docker.com/engine/security/rootless/) — setup guide for rootless Docker
