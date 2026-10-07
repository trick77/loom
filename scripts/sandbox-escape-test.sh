#!/usr/bin/env bash
# Builds the sandbox image, starts it with the production settings from
# compose.yaml and runs the hostile-code checks against it.
#
#   scripts/sandbox-escape-test.sh            # under gVisor (runsc), as in production
#   SANDBOX_RUNTIME=runc scripts/sandbox-escape-test.sh   # local dev, no gVisor
#   SANDBOX_IMAGE=loom-sandbox:ci scripts/sandbox-escape-test.sh  # skip the build
#   SANDBOX_COVER_DIR=coverage/sandbox scripts/sandbox-escape-test.sh
#       # build the -cover binary and collect its counters there (CI)
set -euo pipefail

cd "$(dirname "$0")/.."
runtime="${SANDBOX_RUNTIME:-runsc}"
image="${SANDBOX_IMAGE:-}"
name="loom-sandbox-escape-test"
token="escape-test-token-0123456789"
port="${SANDBOX_TEST_PORT:-18071}"
work="$(mktemp -d)"
trap 'docker stop "$name" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

cover_dir="${SANDBOX_COVER_DIR:-}"
cover_args=()
build_args=()
if [ -n "$cover_dir" ]; then
  mkdir -p "$cover_dir"
  # The job's child flushes its counters after dropping to the slot uid.
  chmod 0777 "$cover_dir"
  cover_args=(-v "$(cd "$cover_dir" && pwd):/cover" -e GOCOVERDIR=/cover)
  build_args=(--build-arg SANDBOX_COVER=1)
fi

if [ -z "$image" ]; then
  image="loom-sandbox:escape-test"
  docker build -q ${build_args[@]+"${build_args[@]}"} -f sandbox/Containerfile -t "$image" . >/dev/null
fi

# A realistic upload: about 20 MiB of xlsx, written by the image's own openpyxl.
docker run --rm --entrypoint python -v "$work:/o" "$image" -c "
import openpyxl, random
wb = openpyxl.Workbook(write_only=True); ws = wb.create_sheet()
ws.append(['id','customer','region','amount','date','note'])
for i in range(600_000):
    ws.append([i, f'customer {i % 5000}', ['ZH','BE','GE','TI'][i % 4], round(random.random()*1e4, 2), f'2025-{i%12+1:02d}-01', 'x'*8])
wb.save('/o/big.xlsx')"

dev_env=()
if [ "$runtime" != "runsc" ]; then
  dev_env=(-e SANDBOX_INSECURE_DEV=1)
fi

docker run -d --rm --name "$name" --runtime "$runtime" \
  --read-only --tmpfs /work:size=16m,mode=0711 \
  --memory 4g --cpus 2 \
  --cap-drop ALL \
  --cap-add SYS_ADMIN --cap-add SETUID --cap-add SETGID --cap-add KILL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --security-opt no-new-privileges:true \
  -e SANDBOX_TOKEN="$token" ${dev_env[@]+"${dev_env[@]}"} ${cover_args[@]+"${cover_args[@]}"} \
  -p "127.0.0.1:$port:8070" "$image" >/dev/null

for _ in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then break; fi
  sleep 1
done
if ! curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null; then
  docker logs "$name"
  exit 1
fi

docker exec ${cover_args[@]+-e GOCOVERDIR=/cover} "$name" /loom-sandbox healthcheck

python3 scripts/sandbox_escape_test.py "http://127.0.0.1:$port" "$token" "$name" "$work/big.xlsx" || {
  docker logs "$name" | tail -20
  exit 1
}
