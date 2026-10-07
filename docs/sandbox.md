# Python sandbox (run_python)

`run_python` lets the model run Python for work it cannot do reliably by hand: exact arithmetic,
counting, date math, text transforms, analysis of an attached CSV or Excel file, charts. The user
never sees the sandbox, only the trace row and the result. Files the code writes to `out/` become
artifacts in the chat.

The `sandbox` service is a plain container that `docker compose up` starts like the other
sidecars; nothing is installed on the host. loom offers the tool only while the sidecar answers
its health probe (every minute while up, every 10 s while down); otherwise the tool and its prompt
guidance are absent and chat works as before.

## What isolates a job

The container runs with the default runtime (runc). Untrusted code therefore meets the host kernel
behind Docker's normal isolation, like any container; a kernel or runc exploit would reach the
host. That is accepted for a deployment shared with people the operator trusts. Inside the
container each layer holds on its own:

- **Own uid, own directory**: each job runs as a per-slot uid with no groups, `no_new_privs` and
  `umask 077`, in its own `0700` directory under `/work/jobs` (not listable). It cannot read the
  other slot's files or processes, nor the runner's environment.
- **One process**: a seccomp filter allows threads but refuses fork, vfork and process clones
  (multiprocessing and subprocess fail with `BlockingIOError`).
- **No network**: the filter allows only `AF_UNIX` sockets and refuses io_uring. The `sandbox`
  compose network is `internal` (no egress) and holds only loom and the sidecar.
- **No way around the limits**: the filter also refuses `memfd_create`, new namespaces, mount,
  ptrace and other kernel surfaces a job never needs; the image has no setuid programs; the
  container has no `SYS_ADMIN` and only the capabilities the runner needs to switch uids.
- **Fixed memory per job**: one process with a 1.25 GiB address space and a 320 MiB share of the
  `/work` tmpfs. Two slots plus the 64 MiB `/dev/shm` stay within `SANDBOX_TOTAL_MEMORY_MB`
  (3.5 GiB; the runner refuses to start otherwise) inside the container's 4 GiB. A job that wants
  more gets a `MemoryError`; the other job is unaffected.
- **Time**: 60 s wall clock and CPU; files over 64 MiB are refused.
- **Stateless**: every call starts from nothing; the job's directory and its `/dev/shm` files are
  removed afterwards. No user id, path or volume reaches the sidecar: loom sends the bytes of
  in-scope uploads and stores the outputs itself.
- **Outputs**: only regular files with an allowed type (png, csv, xlsx, json, txt, md; no svg)
  come back, at most 10 and 25 MiB; loom checks the type and PNG dimensions again.

`make sandbox-escape-test` runs hostile snippets against the image with the compose settings
(network, other jobs, the runner, privilege, fork, memory, disk, output tricks, cancel). CI runs it
on every pull request.

## Configuration

| Variable | Default | |
|---|---|---|
| `BACKEND_SANDBOX_TOKEN` | empty | optional shared secret between loom and the sidecar |
| `BACKEND_SANDBOX_TIMEOUT` | `60s` | longest a job may run, at most 60 s |
| `SANDBOX_SLOTS` | `2` | concurrent jobs in the sidecar |
| `SANDBOX_MEM_LIMIT_MB` | `1280` | address space per job |
| `SANDBOX_DISK_LIMIT_MB` | `320` | each job's share of the `/work` tmpfs |
| `SANDBOX_TOTAL_MEMORY_MB` | `3584` | slots × (memory + disk) + 64 MiB must fit |

Changing slots or limits needs matching values for `SANDBOX_TOTAL_MEMORY_MB`, the `/work` tmpfs
size and `mem_limit` on the `sandbox` service.

## Libraries

numpy, pandas, scipy, sympy, matplotlib, openpyxl, python-dateutil, pint, tabulate, pinned with
hashes in `sandbox/requirements.lock` (regenerate it with the command in `requirements.in`).
Nothing is installed at run time.
