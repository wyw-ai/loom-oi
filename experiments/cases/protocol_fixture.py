"""Trusted loopback fixtures, NOT an OIS/OIC implementation or MCP compatibility test."""
import concurrent.futures
import http.client
import json
import random
import resource
import socket
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

for kind, limit in [(resource.RLIMIT_CPU, 10), (resource.RLIMIT_AS, 512*1024*1024),
                    (resource.RLIMIT_NOFILE, 128), (resource.RLIMIT_FSIZE, 8*1024*1024)]:
    resource.setrlimit(kind, (limit, limit))

class Server(ThreadingHTTPServer):
    daemon_threads = True
    def __init__(self):
        self.counter = 0
        self.lock = threading.Lock()
        super().__init__(("127.0.0.1", 0), Handler)

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def do_GET(self):
        if self.path == "/slow":
            time.sleep(0.15)
        status = 503 if self.path == "/not-ready" else 200
        with self.server.lock:
            payload = json.dumps({"counter": self.server.counter}).encode()
        try:
            self.send_response(status)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        except (BrokenPipeError, ConnectionResetError):
            pass
    def do_POST(self):
        if self.path == "/reset":
            with self.server.lock:
                self.server.counter = 0
            self.send_response(204)
            self.end_headers()
            return
        if self.path != "/mutate-drop":
            self.send_error(404)
            return
        with self.server.lock:
            self.server.counter += 1
        # Fault injected strictly after effect, before any response bytes.
        self.connection.shutdown(socket.SHUT_RDWR)
        self.connection.close()
        self.close_connection = True

def request(server, path, method="GET", timeout=0.05):
    start = time.monotonic_ns()
    conn = http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=timeout)
    try:
        conn.request(method, path)
        response = conn.getresponse()
        data = response.read()
        return {"status": response.status, "body": json.loads(data) if data else None,
                "error": None, "elapsed_ms": (time.monotonic_ns()-start)/1e6}
    except (OSError, http.client.HTTPException) as exc:
        return {"status": None, "body": None, "error": type(exc).__name__,
                "elapsed_ms": (time.monotonic_ns()-start)/1e6}
    finally:
        conn.close()

def emit(**event):
    print(json.dumps(event, sort_keys=True), flush=True)

def probe_case(server, repeats, rng):
    for repeat in range(repeats):
        paths = ["/ready", "/not-ready", "/slow"]
        rng.shuffle(paths)
        for path in paths:
            with socket.create_connection(("127.0.0.1", server.server_port), timeout=1):
                reachable = True
            sample = request(server, path)
            emit(kind="probe", repeat=repeat, target=path, tcp_reachable=reachable, **sample)
        # Same endpoints/deadlines; only scheduling strategy changes.
        modes = ["serial", "independent"]
        rng.shuffle(modes)
        for mode in modes:
            start = time.monotonic_ns()
            if mode == "serial":
                slow = request(server, "/slow")
                fast = request(server, "/ready")
                fast_completion_ms = (time.monotonic_ns()-start)/1e6
            else:
                with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
                    a = pool.submit(request, server, "/slow")
                    b = pool.submit(request, server, "/ready")
                    fast = b.result()
                    fast_completion_ms = (time.monotonic_ns()-start)/1e6
                    slow = a.result()
            emit(kind="schedule", repeat=repeat, mode=mode,
                 fast_status=fast["status"], slow_error=slow["error"],
                 fast_completion_ms=fast_completion_ms)
        # Expiry is a separate MODEL trace, not a physical provider observation.
        emit(kind="freshness_model", repeat=repeat, revision=2, sample_revision=1,
             accepted=False, reason="revision_mismatch",
             evidence_class="model", heartbeat_refreshes_readiness=False)

def retry_case(server, repeats, rng):
    for repeat in range(repeats):
        modes = ["no_replay", "blind_replay"]
        rng.shuffle(modes)
        for mode in modes:
            reset = request(server, "/reset", method="POST", timeout=1)
            assert reset["status"] == 204
            errors = []
            for _ in range(1 if mode == "no_replay" else 2):
                result = request(server, "/mutate-drop", method="POST", timeout=1)
                errors.append(result["error"])
            observed = request(server, "/counter", timeout=1)
            emit(kind="mutation", repeat=repeat, mode=mode, attempts=len(errors),
                 client_errors=errors, counter_status=observed["status"],
                 server_counter=(observed["body"] or {}).get("counter"))

if __name__ == "__main__":
    manifest = json.load(open(sys.argv[1]))
    server = Server()
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        rng = random.Random(manifest["seed"])
        function = {"probe": probe_case, "retry": retry_case}[manifest["case"]]
        function(server, manifest["repeats"], rng)
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=1)
