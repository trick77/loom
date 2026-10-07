# Python sandbox (run_python)

`run_python` lets the model run Python for work it cannot do reliably by hand: exact arithmetic,
counting, date math, text transforms, analysis of an attached CSV or Excel file, charts. The user
never sees the sandbox, only the trace row and the result. Files the code writes to `/work/out/`
become artifacts in the chat.

The tool is optional. loom offers it only while the `sandbox` sidecar answers its health probe
(checked every minute); otherwise the tool and its prompt guidance are absent and chat works as
before.

## Enabling it

1. Install gVisor on the Docker host and register it as the `runsc` runtime
   (<https://gvisor.dev/docs/user_guide/install/>):

   ```sh
   ARCH=$(uname -m)
   URL=https://storage.googleapis.com/gvisor/releases/release/latest/${ARCH}
   wget ${URL}/gvisor.tar.zstd ${URL}/gvisor.tar.zstd.sha512
   sha512sum -c gvisor.tar.zstd.sha512
   sudo tar --zstd -xf gvisor.tar.zstd -C /usr/local/bin
   sudo /usr/local/bin/runsc install
   sudo systemctl reload docker
   docker run --rm --runtime=runsc alpine cat /proc/version   # prints a "-gvisor" kernel
   ```

2. Add to `.env`:

   ```sh
   COMPOSE_PROFILES=sandbox
   BACKEND_SANDBOX_TOKEN=<openssl rand -hex 32>
   ```

3. `docker compose up -d`. loom logs `sandbox available, run_python offered` within a minute.

The sidecar refuses to start outside gVisor. `SANDBOX_INSECURE_DEV=1` lifts that for local
development only (`docker compose -f compose.dev.yaml --profile sandbox up`, which runs it on runc).

## What isolates a job

Each layer holds on its own:

- **gVisor**: the job's system calls go to gVisor's user-space kernel, never to the host kernel.
- **No network**: the `sandbox` compose network is `internal` (no egress) and holds only loom and
  the sidecar. Each job also runs in an empty network namespace, so it cannot reach loom or the
  runner either. `/run` requires `BACKEND_SANDBOX_TOKEN`, which the job never sees.
- **Per-job namespaces**: fresh PID, mount, IPC and UTS namespaces; a fresh `/proc` shows only the
  job's own processes. Other jobs' directories are hidden.
- **Unprivileged**: the job runs as a per-slot uid with no groups, `no_new_privs`, a read-only root
  file system, read-only inputs.
- **Limits**: 60 s wall clock and CPU, 1.25 GiB address space, 64 processes, 64 MiB per file, a
  320 MiB tmpfs per job, two concurrent jobs, 4 GiB for the container.
- **Stateless**: every call starts from nothing; the job's tmpfs is unmounted afterwards. No user
  id, path or volume reaches the sidecar: loom sends the bytes of in-scope uploads and stores the
  outputs itself.
- **Outputs**: only regular files with an allowed type (png, csv, xlsx, json, txt, md; no svg)
  come back, at most 10 and 25 MiB; loom checks the type and PNG dimensions again.

`make sandbox-escape-test` runs hostile snippets against the image under gVisor (network, `/proc`,
privilege, fork bomb, memory, disk, output tricks, concurrent jobs, cancel). CI runs it on every
pull request.

## Configuration

| Variable | Default | |
|---|---|---|
| `BACKEND_SANDBOX_TOKEN` | empty | shared secret; setting it (with the profile) turns the tool on |
| `BACKEND_SANDBOX_TIMEOUT` | `60s` | longest a job may run, at most 60 s |
| `SANDBOX_SLOTS` | `2` | concurrent jobs in the sidecar |
| `SANDBOX_MEM_LIMIT_MB` | `1280` | address space per job |
| `SANDBOX_DISK_LIMIT_MB` | `320` | tmpfs per job (inputs, outputs, home) |

Raising slots or limits needs a matching `mem_limit` on the `sandbox` service.

## Libraries

numpy, pandas, scipy, sympy, matplotlib, openpyxl, python-dateutil, pint, tabulate, pinned with
hashes in `sandbox/requirements.lock` (regenerate it with the command in `requirements.in`).
Nothing is installed at run time.
