#!/usr/bin/env python3
"""Exercise micro run/chat, discovery, failure, alert delivery and recovery locally.
Requires MICRO_BIN (or micro on PATH), nats-server, Go, and Python 3.
All model and webhook traffic goes to this script's local fixture server.
"""
import http.server
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import tempfile
import threading
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
MICRO = shutil.which(os.environ.get("MICRO_BIN", "micro"))
NATS = shutil.which("nats-server")
if not MICRO or not NATS:
    raise SystemExit("Install micro and nats-server, or set MICRO_BIN to the CLI binary")

calls = []
notifications = []
lock = threading.Lock()


class Fixture(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path == "/alerts":
            with lock:
                notifications.append((self.headers.get("Idempotency-Key"), body))
            self.send_response(204)
            self.end_headers()
            return
        if self.path != "/v1/chat/completions":
            self.send_error(404)
            return
        messages = body["messages"]
        names = {tool["function"]["name"] for tool in body.get("tools", [])}
        system = " ".join(str(m.get("content", "")) for m in messages if m["role"] == "system")
        if messages[-1]["role"] == "tool":
            message = {"role": "assistant", "content": "Fixture result: " + str(messages[-1]["content"])}
            reason = "stop"
        else:
            if "You monitor the configured" in system:
                name, args = "monitor_Monitor_Status", {}
            elif "You investigate the configured" in system:
                name, args = "devops_Process_Logs", {"service": "support", "lines": 30}
            elif "Investigate support" in str(messages[-1].get("content", "")):
                name, args = "delegate", {"to": "devops", "task": "Investigate support"}
            else:
                name, args = "delegate", {"to": "monitor", "task": "Check platform health"}
            if name not in names:
                self.send_error(400, "expected tool absent: " + name)
                return
            with lock:
                calls.append(name)
            message = {"role": "assistant", "content": None, "tool_calls": [
                {"id": "fixture-call", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}
            ]}
            reason = "tool_calls"
        data = json.dumps({"id": "fixture", "object": "chat.completion", "model": "fixture",
                           "choices": [{"index": 0, "message": message, "finish_reason": reason}]}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def wait_for(label, check, timeout=90):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        try:
            value = check()
            if value:
                print("PASS:", label, flush=True)
                return value
        except (subprocess.SubprocessError, ValueError, OSError) as error:
            last = error
        time.sleep(0.5)
    raise RuntimeError(f"Timed out: {label}; last error: {last}")


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def stop(proc):
    if proc.poll() is None:
        proc.send_signal(signal.SIGTERM)
        try:
            proc.wait(timeout=15)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
            raise RuntimeError("process did not stop cleanly")


with tempfile.TemporaryDirectory(prefix="agents-smoke-") as scratch:
    work = Path(scratch)
    fixture = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    threading.Thread(target=fixture.serve_forever, daemon=True).start()
    env = {k: v for k, v in os.environ.items() if not k.startswith(("MICRO_", "GITHUB_", "OPENAI_", "ANTHROPIC_", "SUPPORT_", "DEVOPS_", "MONITOR_", "RELEASE_", "ALERT_", "STUDIO_"))}
    # Isolate persistent state and never inherit provider credentials.
    env.update(HOME=str(work), GOMODCACHE=os.environ.get("GOMODCACHE", str(Path.home() / "go/pkg/mod")),
               GOCACHE=os.environ.get("GOCACHE", str(Path.home() / ".cache/go-build")),
               MICRO_AI_PROVIDER="openai", MICRO_AI_API_KEY="fixture", MICRO_AI_MODEL="fixture",
               MICRO_AI_BASE_URL=f"http://127.0.0.1:{fixture.server_port}",
               ALERT_WEBHOOK=f"http://127.0.0.1:{fixture.server_port}/alerts",
               MICRO_REGISTRY="nats", MICRO_BROKER="memory", MICRO_CLIENT_RETRIES="0")
    studio_port = free_port()
    env["STUDIO_ADDRESS"] = f"127.0.0.1:{studio_port}"
    port = free_port()
    env["MICRO_REGISTRY_ADDRESS"] = f"nats://127.0.0.1:{port}"
    processes = []
    logs = []
    def launch(command, name, cwd=ROOT):
        output = (work / (name + ".log")).open("w")
        logs.append(output)
        proc = subprocess.Popen(command, cwd=cwd, env=env, stdout=output, stderr=output)
        processes.append(proc)
        return proc
    def rpc(service, endpoint):
        result = subprocess.run([MICRO, "call", service, endpoint, "{}"], cwd=ROOT, env=env,
                                capture_output=True, text=True, timeout=8, check=True)
        return json.loads(result.stdout)
    try:
        launch([NATS, "-a", "127.0.0.1", "-p", str(port)], "nats")
        def listening():
            with socket.create_connection(("127.0.0.1", port), timeout=1):
                return True
        wait_for("shared registry listening", listening, 10)
        plan = subprocess.run([MICRO, "deploy", "--dry-run", "user@example.invalid"], cwd=ROOT, env=env,
                              capture_output=True, text=True, timeout=10, check=True)
        assert all(name in plan.stdout for name in ("support", "devops", "release", "monitor", "alert", "studio")), plan.stdout
        runner = launch([MICRO, "run", "--no-watch"], "run")
        def healthy():
            result = rpc("monitor", "Monitor.Status")
            return len(result.get("services", [])) == 6 and all(s["status"] == "healthy" for s in result["services"])
        wait_for("all six services respond through shared discovery", healthy, 240)
        studio_url = f"http://127.0.0.1:{studio_port}"
        print("Studio URL:", studio_url, flush=True)
        def web(path):
            with urllib.request.urlopen(studio_url + path, timeout=10) as response:
                return json.load(response)
        inventory = wait_for("Studio discovers all services", lambda: web("/api/services"))
        assert {s["name"] for s in inventory} == {"support", "devops", "release", "monitor", "alert", "studio"}, inventory
        assert len([s for s in inventory if s["agent"]]) == 5, inventory
        with urllib.request.urlopen(studio_url, timeout=10) as response:
            assert "Micro Studio" in response.read().decode()
        request = urllib.request.Request(studio_url + "/api/chat", data=json.dumps({
            "agent": "monitor", "message": "Check platform health",
            "session": "7cba214f-e270-4d79-893d-fafedb0bef19"
        }).encode(), headers={"Content-Type": "application/json", "Origin": studio_url})
        with urllib.request.urlopen(request, timeout=30) as response:
            result = json.load(response)
        assert "healthy" in result["reply"], result
        assert any(t["name"] == "monitor_Monitor_Status" for t in result["tool_calls"]), result
        print("PASS: Studio web chat returns the agent reply and tool activity", flush=True)
        answer = subprocess.run([MICRO, "chat", "--yes", "--prompt", "Check platform health"], cwd=ROOT, env=env,
                                capture_output=True, text=True, timeout=30)
        assert answer.returncode == 0, (answer.stdout, answer.stderr)
        with lock:
            assert "delegate" in calls and "monitor_Monitor_Status" in calls, (calls, answer.stdout, answer.stderr)
        assert "healthy" in answer.stdout, answer.stdout
        print("PASS: micro chat delegated to Monitor and returned real RPC observations", flush=True)
        def pid(name):
            files = list((work / "micro/run").glob(name + "-*.pid"))
            assert len(files) == 1, files
            return int(files[0].read_text().splitlines()[0])
        os.kill(pid("support"), signal.SIGTERM)
        def notice(status):
            with lock:
                return next((body for _, body in notifications if body["service"] == "support" and body["status"] == status), None)
        fired = wait_for("Support failure delivered to webhook", lambda: notice("firing"))
        investigation = subprocess.run([MICRO, "chat", "--yes", "--prompt", "Investigate support"], cwd=ROOT, env=env,
                                       capture_output=True, text=True, timeout=30)
        assert investigation.returncode == 0, (investigation.stdout, investigation.stderr)
        with lock:
            assert "devops_Process_Logs" in calls, calls
        assert "Deregistering" in investigation.stdout, investigation.stdout
        print("PASS: the same chat entry point delegated investigation to DevOps and read Support logs", flush=True)
        binary = next((work / "micro/bin").glob("support-*"))
        launch([str(binary)], "support-restarted", ROOT / "support")
        assert any(n["id"] == fired["id"] for n in web("/api/alerts")["notifications"])
        recovered = wait_for("Support recovery delivered to webhook", lambda: notice("recovered"))
        assert fired["incident"] == recovered["incident"]
        def saved():
            return any(n["id"] == recovered["id"] and n.get("delivered_at") for n in rpc("alert", "Alert.List")["notifications"])
        wait_for("delivery receipt persisted", saved)
        os.kill(pid("alert"), signal.SIGTERM)
        alert_binary = next((work / "micro/bin").glob("alert-*"))
        launch([str(alert_binary)], "alert-restarted", ROOT / "alert")
        wait_for("new Alert process registered", lambda: "Registering node" in (work / "alert-restarted.log").read_text())
        wait_for("Alert restored delivery history after restart", saved)
        with lock:
            support = [body for _, body in notifications if body["service"] == "support"]
            assert len(support) == 2, support
            assert all(key == body["id"] for key, body in notifications)
        stop(runner)
        print("PASS: micro run shutdown", flush=True)
    except Exception:
        for file in work.glob("*.log"):
            print(f"--- {file.name} ---\n{file.read_text()[-8000:]}", flush=True)
        raise
    finally:
        for proc in reversed(processes):
            stop(proc)
        # Catch any launched child if the runner exited before installing its signal handler.
        for file in (work / "micro/run").glob("*.pid"):
            try:
                os.kill(int(file.read_text().splitlines()[0]), signal.SIGTERM)
            except (ProcessLookupError, FileNotFoundError):
                pass
        fixture.shutdown()
        fixture.server_close()
        for output in logs:
            output.close()
