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


def denied(code):
    """Runs code that must raise; prints DENIED if it did."""
    body = "\n".join("    " + line for line in code.splitlines())
    return run(f"try:\n{body}\n    print('ALLOWED')\nexcept Exception as e:\n    print('DENIED', type(e).__name__, e)")


# --- smoke ---------------------------------------------------------------
r = run("import os\nprint(os.getuid(), os.getgid(), os.getgroups())")
check("runs as a slot uid with no groups", r["stdout"].strip() in ("10000 10000 []", "10001 10001 []"), r)

r = run("import matplotlib.pyplot as plt, pandas as pd, numpy as np, scipy, sympy, openpyxl, pint\n"
        "plt.plot([1,2,3]); plt.savefig('out/a.png'); print('ok')")
pngs = [f for f in r.get("files") or [] if f["name"] == "a.png"]
check("data stack imports, chart lands as artifact",
      r["stdout"].strip() == "ok" and pngs and base64.b64decode(pngs[0]["data"])[:8] == b"\x89PNG\r\n\x1a\n", r)

r = run("from zoneinfo import ZoneInfo\nimport datetime as d\n"
        "print(d.datetime(2027,3,30,14,30,tzinfo=ZoneInfo('Europe/Zurich')).astimezone(ZoneInfo('Asia/Tokyo')).strftime('%H:%M'))")
check("time zone database present", r["stdout"].strip() == "21:30", r)

r = run("print(open('in/data.csv').read().strip())", files={"data.csv": b"a,b\n1,2"})
check("input file readable at in/", r["stdout"].strip() == "a,b\n1,2", r)

with open(XLSX, "rb") as fh:
    xlsx = fh.read()
r = run("import pandas as pd\ndf = pd.read_excel('in/big.xlsx')\nprint(len(df))",
        files={"big.xlsx": xlsx}, timeout_ms=60000)
check(f"read_excel on a {len(xlsx) >> 20} MiB xlsx fits the memory limit",
      r["exit_code"] == 0 and r["stdout"].strip().isdigit(), {k: r[k] for k in ("stderr", "exit_code", "timed_out")})

# --- network: no sockets but AF_UNIX -------------------------------------
for target in [("1.1.1.1", 443), ("127.0.0.1", 8070), ("loom", 8080)]:
    r = denied(f"import socket\nsocket.create_connection({target!r}, timeout=3)")
    check(f"no connection to {target[0]}:{target[1]}", r["stdout"].startswith("DENIED"), r)
for family in ("AF_INET", "AF_INET6", "AF_NETLINK", "AF_PACKET"):
    r = denied(f"import socket\nsocket.socket(socket.{family}, socket.SOCK_RAW if '{family}' == 'AF_PACKET' else socket.SOCK_DGRAM)")
    check(f"no {family} socket", r["stdout"].startswith("DENIED"), r)

# --- the runner and other jobs -------------------------------------------
r = denied("print(open('/proc/1/environ','rb').read())")
check("runner environment unreadable", r["stdout"].startswith("DENIED") and TOKEN not in r["stdout"], r)

r = denied("import os\nprint(os.listdir('/work/jobs'))")
check("job directories not listable", r["stdout"].startswith("DENIED"), r)

for name, code in {
    "read /etc/shadow": "open('/etc/shadow').read()",
    "write to /usr": "open('/usr/x','w').write('x')",
    "write a new file into in/": "open('in/x','w').write('x')",
    "become root": "import os; os.setuid(0)",
    "mount": "import ctypes, os; l=ctypes.CDLL(None, use_errno=True)\nif l.mount(b'none', b'/tmp', b'tmpfs', 0, None) != 0: raise OSError(ctypes.get_errno(), 'mount')",
    "anonymous memory file": "import os; os.memfd_create('x')",
    "new user namespace": "import os; os.unshare(os.CLONE_NEWUSER)",
    "ptrace": "import ctypes; l=ctypes.CDLL(None, use_errno=True)\nif l.ptrace(16, 1, 0, 0) != 0: raise OSError(ctypes.get_errno(), 'ptrace')",
}.items():
    r = denied(code)
    check(f"denied: {name}", r["stdout"].startswith("DENIED"), r)

# --- resource exhaustion -------------------------------------------------
r = run("import os\nwhile True:\n    os.fork()", timeout_ms=10000)
check("fork bomb contained", r["exit_code"] != 0 or r["timed_out"], r)
check("healthy after fork bomb", healthy())

r = run("b = bytearray(4 << 30)")
check("4 GiB allocation fails", "MemoryError" in r["stderr"], r)

# One process per job keeps every job's memory under a fixed ceiling.
r = run("import multiprocessing as mp\nwith mp.Pool(4) as p: print(p.map(abs, [-1, -2]))")
check("multiprocessing is refused", r["exit_code"] != 0 and "BlockingIOError" in r["stderr"], r)
r = run("import subprocess\nsubprocess.run(['true'])")
check("subprocess is refused", r["exit_code"] != 0, r)
r = run("import threading\nout = []\nts = [threading.Thread(target=out.append, args=(i,)) for i in range(4)]\n"
        "[t.start() for t in ts]; [t.join() for t in ts]\nprint(sorted(out))")
check("threads still work", r["stdout"].strip() == "[0, 1, 2, 3]", r)

big = {}


def big_job(key):
    big[key] = run("b = bytearray(1100 << 20)\nb[-1] = 1\nimport time; time.sleep(3)\nprint('held')", timeout_ms=30000)


ts = [threading.Thread(target=big_job, args=(k,)) for k in ("a", "b")]
[t.start() for t in ts]
[t.join() for t in ts]
check("two 1.1 GiB jobs run side by side within the budget",
      all(big[k]["stdout"].strip() == "held" for k in ("a", "b")), big)

r = run("while True: pass", timeout_ms=3000)
check("busy loop killed at timeout", r["timed_out"], r)

r = run("import time; time.sleep(999)", timeout_ms=3000)
check("sleep killed at timeout", r["timed_out"], r)

r = run("import sys\nfor _ in range(100): sys.stdout.write('x' * 1_000_000)")
check("100 MB of stdout truncated", r["truncated"] and len(r["stdout"]) < 30_000, len(r["stdout"]))

r = run("open('home/f','wb').write(b'x' * (100 << 20))")
check("a file over 64 MiB is refused", r["exit_code"] != 0, r)

# --- output files --------------------------------------------------------
r = run("import os\n"
        "os.symlink('/etc/passwd', 'out/passwd.txt')\n"
        "os.mkfifo('out/pipe.txt')\n"
        "open('out/a.txt','w').write('a'); os.link('out/a.txt', 'out/b.txt')\n"
        "open('out/x.svg','w').write('<svg/>')\n"
        "open('out/good.csv','w').write('1')")
names = [f["name"] for f in r.get("files") or []]
check("only the safe output returned", names == ["good.csv"], r)

# --- nothing survives a job -----------------------------------------------
run("open('/dev/shm/left-behind','w').write('x')")
r = run("import os\nprint(os.listdir('/dev/shm'))")
check("a job's /dev/shm files are gone for the next job", r["stdout"].strip() == "[]", r)

# --- concurrency and cancel ----------------------------------------------
secret = "s3cr3t-" + str(time.time())
results = {}


def job_a():
    results["a"] = run(f"open('home/secret.txt','w').write({secret!r})\n"
                       f"open('/dev/shm/secret','w').write({secret!r})\nimport time; time.sleep(4)")


t = threading.Thread(target=job_a)
t.start()
time.sleep(1.5)
r = run(f"import os\nfound=[]\nfor top in ('/work', '/dev/shm', '/tmp'):\n  for root, ds, fs in os.walk(top):\n    for f in fs:\n"
        f"        try:\n            if {secret!r} in open(os.path.join(root,f), errors='ignore').read(): found.append(f)\n"
        f"        except Exception: pass\n"
        f"readable = []\nfor p in os.listdir('/proc'):\n  if p.isdigit() and int(p) != os.getpid():\n"
        f"    try:\n      open(f'/proc/{{p}}/environ','rb').read(); readable.append(p)\n    except Exception: pass\n"
        f"print(found, readable)")
t.join()
check("concurrent job cannot read another job's files or processes", r["stdout"].strip() == "[] []", r)

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
