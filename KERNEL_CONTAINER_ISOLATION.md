# Linux Kernel View: Container Isolation and Sandboxing

This document explains how Linux implements container isolation and sandboxing using real kernel data structures and permission checks.

Container security is not a single mechanism. It is built from:

- namespace objects and ownership
- credentials and capabilities
- seccomp filters
- LSM hooks (AppArmor/SELinux/etc.)
- cgroups/resource limits
- conditional checks during syscalls and exec

In plain kernel terms, that means a task carries a few state fields, and the kernel runs small `if` checks against them while handling syscalls. If a check fails, the kernel returns an error and stops the transition.

Concrete translation of the common words in this document:

- sandboxing: a set of syscall and exec checks that deny specific actions
- policy: the `if` statements that decide allow or deny
- boundary: the state check that keeps a task in its own namespace or ID mapping
- escape: a path that would let a task bypass `cred->user_ns`, `task_struct->nsproxy`, `no_new_privs`, or uid/gid mapping checks
- container isolation: the combined effect of those fields and checks

## 1. Core Kernel State and Key Terms

Most container behavior comes from a few fields in `task_struct` and `cred`, plus `if` statements in syscall paths that read those fields.

### `task_struct` and `nsproxy`

`task_struct` is the kernel object for a running task. It stores scheduling state, credentials, namespace pointers, seccomp state, cgroup membership, and other process state.

The namespace-related fields are:

- https://github.com/torvalds/linux/blob/master/include/linux/sched.h#L1193

```c
/* Namespaces: */
struct nsproxy *nsproxy;
```

The `nsproxy` object is a shared namespace bundle:

- https://github.com/torvalds/linux/blob/master/include/linux/nsproxy.h#L29-L39

```c
struct nsproxy {
    refcount_t count;
    struct uts_namespace *uts_ns;
    struct ipc_namespace *ipc_ns;
    struct mnt_namespace *mnt_ns;
    struct pid_namespace *pid_ns_for_children;
    struct net *net_ns;
    struct time_namespace *time_ns;
    struct time_namespace *time_ns_for_children;
    struct cgroup_namespace *cgroup_ns;
};
```

### `user_namespace`

- https://github.com/torvalds/linux/blob/master/include/linux/user_namespace.h#L76-L89

```c
struct user_namespace {
    struct uid_gid_map uid_map;
    struct uid_gid_map gid_map;
    struct uid_gid_map projid_map;
    struct user_namespace *parent;
    int level;
    kuid_t owner;
    kgid_t group;
    ...
    bool parent_could_setfcap;
    ...
};
```

`cred->user_ns` is expected to be valid in normal kernel state. The helper APIs are defensive: `get_user_ns()` and `put_user_ns()` accept `NULL`, and when `CONFIG_USER_NS` is disabled the inline helpers fall back to `&init_user_ns`.

That fallback does not mean "outside the container" in a privilege sense. It means the kernel uses the initial user namespace as the reference for capability checks because user-namespace isolation is unavailable in that configuration.

`init_user_ns` is the boot-time initial user namespace. It is the kernel's top-level reference namespace for credentials and capabilities. When a task is in `init_user_ns`, capability checks are evaluated against the initial namespace rather than a nested user namespace.

The fallback code is:

- `current_user_ns()` in [include/linux/cred.h](include/linux/cred.h#L390-L398)
- `get_user_ns()` in [include/linux/user_namespace.h](include/linux/user_namespace.h#L207-L229)

```c
#ifdef CONFIG_USER_NS
#define current_user_ns()    (current_cred_xxx(user_ns))
#else
static inline struct user_namespace *current_user_ns(void)
{
    return &init_user_ns;
}
#endif
```

```c
#ifdef CONFIG_USER_NS
static inline struct user_namespace *get_user_ns(struct user_namespace *ns)
{
    if (ns)
        ns_ref_inc(ns);
    return ns;
}
#else
static inline struct user_namespace *get_user_ns(struct user_namespace *ns)
{
    return &init_user_ns;
}
#endif
```

The `parent_could_setfcap` field records whether the creator had `CAP_SETFCAP` when the namespace was created.

### `cred`

- https://github.com/torvalds/linux/blob/master/include/linux/cred.h#L114-L145

```c
struct cred {
    ...
    kernel_cap_t cap_inheritable;
    kernel_cap_t cap_permitted;
    kernel_cap_t cap_effective;
    kernel_cap_t cap_bset;
    kernel_cap_t cap_ambient;
    ...
    struct user_namespace *user_ns;
    ...
};
```

### `no_new_privs`

- https://github.com/torvalds/linux/blob/master/include/linux/sched.h#L1052
- https://github.com/torvalds/linux/blob/master/include/linux/sched.h#L1827-L1849

```c
unsigned long atomic_flags;

#define PFA_NO_NEW_PRIVS 0
TASK_PFA_TEST(NO_NEW_PRIVS, no_new_privs)
TASK_PFA_SET(NO_NEW_PRIVS, no_new_privs)
```

In memory, this is a flag bit stored on `task_struct`.

## 2. How the Pieces Fit Together

The structs above hold the data. The code below reads that data and returns allow or deny.

Typical flow:

1. A task runs with a `task_struct`.
2. The task's `cred` says which `user_namespace` the credentials belong to.
3. The task's `nsproxy` says which mount, PID, UTS, network, IPC, and cgroup namespaces are active.
4. Syscalls such as `execve`, `prctl`, `clone`, `unshare`, and filesystem operations consult those fields before allowing a transition.
5. If the task set `no_new_privs`, exec-time privilege growth is blocked.

Container isolation adds a small cost on namespace-sensitive code paths: the kernel must read task state and compare it against the relevant namespace, and only for container isolated tasks perform a deeper namespace-specific check.

## File Creation Across Namespace Boundaries

Creating a file inside a container is a real-world example because the path passes through filesystem permission checks, namespace-aware ID translation, and capability checks.

The same pattern also shows up in exec and uid/gid map operations:

- `prctl(PR_SET_NO_NEW_PRIVS)` only accepts `arg2 == 1` and sets a task flag.
- `execve` marks the task with `LSM_UNSAFE_NO_NEW_PRIVS` when that flag is set.
- `create_user_ns()` and `verify_root_map()` gate uid 0 mappings with `CAP_SETFCAP` checks.

The create path is:

1. `vfs_create(...)`
2. `may_create_dentry(...)`
3. `inode_permission(...)`
4. `generic_permission(...)`

### Create entry point

- https://github.com/torvalds/linux/blob/master/fs/namei.c#L4162-L4188

`vfs_create()` first asks whether the parent directory is eligible for creation. If this early check fails, the filesystem-specific create method is never reached.

```c
error = may_create_dentry(idmap, dir, dentry);
if (error)
    return error;
```

- namespace objects and ownership

- https://github.com/torvalds/linux/blob/master/fs/namei.c#L3708-L3719

This is the first namespace-aware failure point. The kernel checks whether the directory's filesystem identity can be represented in the caller's idmap.

```c
if (!fsuidgid_has_mapping(dir->i_sb, idmap))
    return -EOVERFLOW;

return inode_permission(idmap, dir, MAY_WRITE | MAY_EXEC);
```

This returns `-EOVERFLOW` when the directory's filesystem identity cannot be represented in the mount idmap. The kernel is not saying "permission denied" here. It is saying that the uid/gid values for this directory cannot be translated into the caller's namespace context, so it cannot safely continue with the create path.

### `inode_permission`

- https://github.com/torvalds/linux/blob/master/fs/namei.c#L638-L646

If the create path reaches this point, the kernel is checking ordinary write permission, but it still refuses to proceed when the inode has unmapped IDs.

```c
if (mask & MAY_WRITE) {
    if (unlikely(HAS_UNMAPPED_ID(idmap, inode)))
        return -EACCES;
}
```

This returns `-EACCES` when the kernel cannot safely apply the write permission rules to an inode with unmapped IDs.

### Capability override path

- https://github.com/torvalds/linux/blob/master/fs/namei.c#L519-L546
- https://github.com/torvalds/linux/blob/master/kernel/capability.c#L455-L479

This path explains why being root inside a container is not enough by itself. The kernel only allows capability override when the inode's IDs are mapped in the current namespace.

```c
if (capable_wrt_inode_uidgid(idmap, inode, CAP_DAC_OVERRIDE))
    return 0;
return -EACCES;
```

and

```c
return ns_capable(ns, cap) &&
       privileged_wrt_inode_uidgid(ns, idmap, inode);
```

Even if the task has the capability bit, the override is still denied when the inode's IDs are not mapped in the current namespace.

### Example scenario

Consider a container process running as root inside a user namespace, with a host bind mount whose owner IDs are not mapped into that namespace.

In that case, the kernel can reject creation before the filesystem-specific create call, or later in the inode permission path, depending on which condition fails first.

Typical outcomes on file creation:

- `-EOVERFLOW` at `may_create_dentry()` mapping guard.
- `-EACCES` at `inode_permission()` or `generic_permission()` when the capability override path fails because ID mapping checks do not pass.

## Sandbox Model

Sandboxing means several independent checks run on different paths:

- seccomp (syscall filtering)
- no_new_privs (no privilege gain through exec transitions)
- LSM policy (object and transition controls)
- namespace and capability scoping

### Host vs namespace

"Host" means the machine outside the container boundary: the initial kernel namespaces and the resources they control.

- user namespace: `init_user_ns`
- mount namespace: `init_mnt_ns`
- PID namespace: `init_pid_ns`
- network namespace: `init_net`

The host is not a single namespace object. It is the outer, initial namespace context that container isolation is designed to separate from.

## What a Container Escape Means

A container escape is a failure of one of the boundaries above. In kernel terms, it usually means a task outside its expected isolation can act with the authority of the initial namespace context, or can influence resources that were supposed to stay out of reach.

Direct code-level hints for what this would look like:

- If a task could make `cred->user_ns` point at `&init_user_ns` without passing the normal credential transition path, namespace-based capability checks would no longer stay confined to the task's own user namespace.
- If a task could replace `task_struct->nsproxy` with an initial-namespace namespace bundle without `clone`, `unshare`, or `setns` permission checks, the task would stop being isolated from the initial mount, PID, network, or IPC state.
- If a task could bypass `task_no_new_privs(current)` and clear the `LSM_UNSAFE_NO_NEW_PRIVS` path during exec, it could regain privilege through a setuid or file-capability transition that should have been blocked.
- If a task could write a uid or gid map without the `CAP_SETFCAP` and mapping checks in `verify_root_map()`, it could create a namespace whose root mappings were more powerful than intended.

In other words, a container escape is not one special `if` statement. It is what happens when a task can cross a boundary that should have been held by fields like `cred->user_ns` and `task_struct->nsproxy`, or by checks like `no_new_privs`, `verify_root_map()`, and the filesystem permission path.

## Reading Order

1. `kernel/sys.c` no_new_privs prctl path
2. `fs/exec.c` `check_unsafe_exec()`
3. `security/commoncap.c` `cap_bprm_creds_from_file()`
4. `kernel/seccomp.c` `seccomp_prepare_filter()`
5. `kernel/user_namespace.c` `create_user_ns()` and `verify_root_map()`
6. `fs/namei.c` `vfs_create()` and `may_create_dentry()`
7. `kernel/capability.c` `capable_wrt_inode_uidgid()` and `privileged_wrt_inode_uidgid()`

## Observability Hooks

- `/proc/<pid>/status` `NoNewPrivs` field: https://github.com/torvalds/linux/blob/master/fs/proc/array.c#L334
- `PFA_NO_NEW_PRIVS` definition: https://github.com/torvalds/linux/blob/master/include/linux/sched.h#L1827-L1848
