#!/usr/bin/env python3
"""Hostile-code checks against a running loom-sandbox container.

Usage: sandbox_escape_test.py URL TOKEN CONTAINER XLSX

Every check posts a job and asserts it failed safely (or, for the smoke
checks, worked). The runner must stay healthy throughout. Standard library
only; scripts/sandbox-escape-test.sh starts the container and calls this.
"""

import base64
import json
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request

URL, TOKEN, CONTAINER, XLSX = sys.argv[1:5]
failures = []


def run(code, files=None, timeout_ms=10000, client_timeout=90):
    body = {"code": code, "timeout_ms": timeout_ms}
    if files:
        body["files"] = [{"name": n, "data": base64.b64encode(d).decode()} for n, d in files.items()]
    req = urllib.request.Request(
        URL + "/run",
        data=json.dumps(body).encode(),
        headers={"X-Sandbox-Token": TOKEN, "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=client_timeout) as resp:
        return json.load(resp)


def check(name, cond, detail=""):
    print(("ok   " if cond else "FAIL ") + name + ("" if cond else f"  -- {detail}"))
    if not cond:
        failures.append(name)


def healthy():
    try:
        r = run("print('alive')", timeout_ms=5000)
        return r["stdout"].strip() == "alive"
    except Exception as e:  # noqa: BLE001
        print("health error:", e)
        return False


# --- smoke ---------------------------------------------------------------
r = run("import os\nprint(os.getuid(), os.getgid(), os.getpid(), os.getgroups())")
check("runs as slot uid, PID 1, no groups", r["stdout"].split()[:3] == ["10000", "10000", "1"]
      or r["stdout"].split()[:3] == ["10001", "10001", "1"], r)

r = run("import matplotlib.pyplot as plt, pandas as pd, numpy as np, scipy, sympy, openpyxl, pint\n"
        "plt.plot([1,2,3]); plt.savefig('/work/out/a.png'); print('ok')")
pngs = [f for f in r.get("files") or [] if f["name"] == "a.png"]
check("data stack imports, chart lands as artifact",
      r["stdout"].strip() == "ok" and pngs and base64.b64decode(pngs[0]["data"])[:8] == b"\x89PNG\r\n\x1a\n", r)

r = run("from zoneinfo import ZoneInfo\nimport datetime as d\n"
        "print(d.datetime(2027,3,30,14,30,tzinfo=ZoneInfo('Europe/Zurich')).astimezone(ZoneInfo('Asia/Tokyo')).strftime('%H:%M'))")
check("time zone database present", r["stdout"].strip() == "21:30", r)

r = run("print(open('/work/in/data.csv').read().strip())", files={"data.csv": b"a,b\n1,2"})
check("input file readable at /work/in", r["stdout"].strip() == "a,b\n1,2", r)

with open(XLSX, "rb") as fh:
    xlsx = fh.read()
r = run("import pandas as pd\ndf = pd.read_excel('/work/in/big.xlsx')\nprint(len(df))",
        files={"big.xlsx": xlsx}, timeout_ms=60000)
check(f"read_excel on a {len(xlsx) >> 20} MiB xlsx fits the memory limit",
      r["exit_code"] == 0 and r["stdout"].strip().isdigit(), {k: r[k] for k in ("stderr", "exit_code", "timed_out")})

# --- network -------------------------------------------------------------
for target in [("1.1.1.1", 443), ("127.0.0.1", 8070), ("172.17.0.1", 8070), ("loom", 8080)]:
    r = run(f"import socket\ntry:\n    socket.create_connection({target!r}, timeout=3)\n    print('CONNECTED')\n"
            f"except Exception as e:\n    print('blocked', type(e).__name__)")
    check(f"no connection to {target[0]}:{target[1]}", r["stdout"].startswith("blocked"), r)

r = run("import socket\nprint(socket.if_nameindex())")
check("only loopback exists", "eth" not in r["stdout"], r)

# --- host and other jobs -------------------------------------------------
r = run("import os\nprint(sorted(p for p in os.listdir('/proc') if p.isdigit()))\n"
        "print(open('/proc/1/environ','rb').read())")
check("only own processes visible", r["stdout"].splitlines()[0] in ("['1']",), r)
check("runner token not visible", TOKEN not in r["stdout"], r)

r = run("import os\nprint(os.listdir('/work/jobs'))")
check("job directories hidden", r["stdout"].strip() == "[]", r)

for name, code in {
    "read /etc/shadow": "open('/etc/shadow').read()",
    "write to /usr": "open('/usr/x','w').write('x')",
    "write to /work/in": "open('/work/in/x','w').write('x')",
    "become root": "import os; os.setuid(0)",
    "mount": "import ctypes; l=ctypes.CDLL(None); assert l.mount(b'none', b'/mnt', b'tmpfs', 0, None) == 0",
}.items():
    r = run(code)
    check(f"denied: {name}", r["exit_code"] != 0, r)

# --- resource exhaustion -------------------------------------------------
r = run("import os\nwhile True:\n    os.fork()", timeout_ms=10000)
check("fork bomb contained", r["exit_code"] != 0 or r["timed_out"], r)
check("healthy after fork bomb", healthy())

r = run("b = bytearray(4 << 30)")
check("4 GiB allocation fails", "MemoryError" in r["stderr"], r)

r = run("while True: pass", timeout_ms=3000)
check("busy loop killed at timeout", r["timed_out"], r)

r = run("import time; time.sleep(999)", timeout_ms=3000)
check("sleep killed at timeout", r["timed_out"], r)

r = run("import sys\nfor _ in range(100): sys.stdout.write('x' * 1_000_000)")
check("100 MB of stdout truncated", r["truncated"] and len(r["stdout"]) < 30_000, len(r["stdout"]))

r = run("open('/work/home/f','wb').write(b'x' * (400 << 20))")
check("job disk quota enforced", r["exit_code"] != 0, r)

# --- output files --------------------------------------------------------
r = run("import os\n"
        "os.symlink('/etc/passwd', '/work/out/passwd.txt')\n"
        "os.mkfifo('/work/out/pipe.txt')\n"
        "open('/work/out/a.txt','w').write('a'); os.link('/work/out/a.txt', '/work/out/b.txt')\n"
        "open('/work/out/x.svg','w').write('<svg/>')\n"
        "open('/work/out/good.csv','w').write('1')")
names = [f["name"] for f in r.get("files") or []]
check("only the safe output returned", names == ["good.csv"], r)

# --- concurrency and cancel ----------------------------------------------
secret = "s3cr3t-" + str(time.time())
results = {}


def job_a():
    results["a"] = run(f"open('/work/home/secret.txt','w').write({secret!r})\nimport time; time.sleep(4)")


t = threading.Thread(target=job_a)
t.start()
time.sleep(1.5)
r = run(f"import os\nfound=[]\nfor root, ds, fs in os.walk('/work'):\n    for f in fs:\n"
        f"        try:\n            if {secret!r} in open(os.path.join(root,f), errors='ignore').read(): found.append(f)\n"
        f"        except Exception: pass\nprint(found)")
t.join()
check("concurrent job cannot see another job's files", r["stdout"].strip() == "[]", r)

try:
    run("import time; time.sleep(30)", timeout_ms=60000, client_timeout=2)
except Exception:  # noqa: BLE001 - the client timeout is the point
    pass
time.sleep(2)
top = subprocess.run(["docker", "top", CONTAINER], capture_output=True, text=True).stdout
check("cancelled job is killed", "main.py" not in top, top)

check("runner healthy at the end", healthy())

print()
if failures:
    print(f"{len(failures)} check(s) failed: {', '.join(failures)}")
    sys.exit(1)
print("all checks passed")
