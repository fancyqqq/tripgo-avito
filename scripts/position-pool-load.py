import argparse
import datetime
import json
import os
from pathlib import Path
import re
import shlex
import signal
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--prometheus-url", default="http://127.0.0.1:23909")
    parser.add_argument("--report-dir", default="reports/positions")
    parser.add_argument("--instance-label", default="exported_instance")
    args = parser.parse_args()
    if not re.fullmatch(r"[a-zA-Z_][a-zA-Z0-9_]*", args.instance_label):
        parser.error("invalid Prometheus instance label")
    root = Path(__file__).resolve().parent.parent
    env = os.environ.copy()
    for line in (root / ".env").read_text().splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key, separator, value = line.partition("=")
        if separator:
            fields = shlex.split(value)
            env.setdefault(key.strip(), fields[0] if fields else "")
    database = urllib.parse.urlsplit(env["DATABASE_URL"])
    pg_env = env.copy()
    pg_env.update(
        PGHOST=database.hostname or "", PGPORT=str(database.port or 5432),
        PGDATABASE=database.path.lstrip("/"), PGUSER=urllib.parse.unquote(database.username or ""),
        PGPASSWORD=urllib.parse.unquote(database.password or ""),
        PGSSLMODE=urllib.parse.parse_qs(database.query).get("sslmode", ["disable"])[0],
    )
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def request(url, payload=None):
        body = json.dumps(payload).encode() if payload is not None else None
        req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
        with client.open(req, timeout=5) as response:
            raw = response.read()
            if not raw:
                return None
            try:
                return json.loads(raw)
            except json.JSONDecodeError:
                return raw.decode()

    def sql(statement):
        subprocess.run(
            ["psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-c", statement],
            env=pg_env, check=True, capture_output=True, timeout=10,
        )

    run_id = "positions-load-" + uuid.uuid4().hex
    schema = "positions_load_" + uuid.uuid4().hex
    selector = '{' + args.instance_label + '="' + run_id + '"}'
    queue_name = "tripgo_position_requests_queue_size"
    active_name = "tripgo_position_requests_workers_active"
    metrics_selector = '{__name__=~"tripgo_(position_requests.*|stale_checks.*|positions_saved.*)",' + args.instance_label + '="' + run_id + '"}'
    samples, ready_latencies, memory = [], [], []
    process = None
    behaviour_changed = False
    push_admin = env.get("PUSH_ADMIN_URL", env["PUSH_HTTP_URL"].rstrip("/") + "/admin/behaviour")
    if not push_admin.rstrip("/").endswith("/admin/behaviour"):
        push_admin = push_admin.rstrip("/") + "/admin/behaviour"
    started_at = time.time()
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    base = f"http://127.0.0.1:{port}"
    query = urllib.parse.parse_qs(database.query)
    query["search_path"] = [schema]
    env["DATABASE_URL"] = urllib.parse.urlunsplit(database._replace(query=urllib.parse.urlencode(query, doseq=True)))
    attributes = env.get("OTEL_RESOURCE_ATTRIBUTES", "")
    env["OTEL_RESOURCE_ATTRIBUTES"] = ",".join(
        [value for value in attributes.split(",") if value and not value.startswith("service.instance.id=")]
        + ["service.instance.id=" + run_id]
    )
    env.update(
        HTTP_ADDR=f"127.0.0.1:{port}", POSITION_WORKERS="4", POSITION_QUEUE_SIZE="10",
        POSITION_CHECK_INTERVAL="1s", POSITION_STALE_AFTER="10s", PUSH_REQUEST_TIMEOUT="1s",
        SHUTDOWN_TIMEOUT="5s",
    )

    def prometheus(path, parameters):
        payload = request(args.prometheus_url.rstrip("/") + path + "?" + urllib.parse.urlencode(parameters))
        if payload["status"] != "success":
            raise RuntimeError("Prometheus query failed")
        return payload["data"]["result"]

    def wait_metrics(phase, predicate):
        deadline = time.monotonic() + 65
        while True:
            if process.poll() is not None:
                raise RuntimeError("service exited during load check")
            began = time.monotonic()
            request(base + "/ready")
            ready_latencies.append(time.monotonic() - began)
            status = Path(f"/proc/{process.pid}/status").read_text()
            memory.append(int(next(line.split()[1] for line in status.splitlines() if line.startswith("VmRSS:"))))
            rows = prometheus("/api/v1/query", {"query": metrics_selector})
            values = {row["metric"]["__name__"] + ":" + row["metric"].get("result", ""): float(row["value"][1]) for row in rows if not row["metric"]["__name__"].endswith("_bucket")}
            samples.append({"elapsed_seconds": time.time() - started_at, "phase": phase, "values": values})
            if predicate(values):
                print(phase + ": metrics observed", flush=True)
                return values
            if time.monotonic() >= deadline:
                raise RuntimeError("timed out waiting for " + phase + " metrics: " + json.dumps(values))
            threading.Event().wait(1)

    def value(values, name, result=""):
        return values.get(name + ":" + result, -1)

    try:
        sql(f'CREATE SCHEMA "{schema}"')
        sql(f'CREATE TABLE "{schema}".trips (LIKE public.trips INCLUDING ALL)')
        sql(f'CREATE TABLE "{schema}".trip_positions (LIKE public.trip_positions INCLUDING ALL)')
        with tempfile.TemporaryFile(mode="w+") as logs:
            process = subprocess.Popen([str(root / "bin/trip-service")], cwd=root, env=env, stdout=logs, stderr=subprocess.STDOUT)
            try:
                deadline = time.monotonic() + 10
                while True:
                    try:
                        request(base + "/ready")
                        break
                    except (urllib.error.URLError, ConnectionError):
                        if process.poll() is not None or time.monotonic() >= deadline:
                            raise RuntimeError("service did not become ready")
                        threading.Event().wait(0.1)
                wait_metrics("idle", lambda values: value(values, queue_name) == 0 and value(values, active_name) == 0)
                behaviour_changed = True
                request(push_admin, {"latency_ms": 5000})
                trip_ids = [str(uuid.uuid4()) for _ in range(200)]
                rows = ",".join(
                    "('%s','%s','%s',0,0,0,0,0,'active',now()-interval '1 minute')" % (trip_id, uuid.uuid4(), uuid.uuid4())
                    for trip_id in trip_ids
                )
                sql(f'INSERT INTO "{schema}".trips(id,user_id,driver_id,start_latitude,start_longitude,end_latitude,end_longitude,price,status,started_at) VALUES {rows}')
                loaded = wait_metrics("slow_push", lambda values: 0 < value(values, queue_name) <= 10 and value(values, active_name) == 4 and value(values, "tripgo_stale_checks_total", "dropped") > 0 and value(values, "tripgo_position_requests_total", "error") > 0)
                request(base + f"/api/v1/trips/{trip_ids[0]}/positions", {
                    "coordinates": {"latitude": 55.75, "longitude": 37.61},
                    "recorded_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                })
                sql(f'UPDATE "{schema}".trips SET status=\'completed\',finished_at=now()')
                request(push_admin, {"latency_ms": 0})
                behaviour_changed = False
                drained = wait_metrics("drained", lambda values: value(values, queue_name) == 0 and value(values, active_name) == 0 and value(values, "tripgo_position_requests_total", "success") > 0 and value(values, "tripgo_positions_saved_total") == 1)
                process.send_signal(signal.SIGTERM)
                exit_code = process.wait(timeout=7)
                if exit_code != 0:
                    raise RuntimeError(f"service exited with code {exit_code}")
                report = {
                    "run_id": run_id, "started_at_unix": started_at, "finished_at_unix": time.time(),
                    "parameters": {"trips": 200, "workers": 4, "queue_size": 10, "check_interval_seconds": 1, "push_timeout_seconds": 1, "slow_push_latency_ms": 5000, "metric_export_interval_seconds": 15},
                    "loaded": loaded, "drained": drained, "samples": samples,
                    "ready_max_seconds": max(ready_latencies), "ready_checks": len(ready_latencies),
                    "rss_min_kib": min(memory), "rss_max_kib": max(memory), "exit_code": exit_code,
                    "prometheus_queries": {"queue": queue_name + selector, "workers": active_name + selector, "all": metrics_selector},
                    "prometheus_range": prometheus("/api/v1/query_range", {"query": metrics_selector, "start": started_at, "end": time.time(), "step": 1}),
                }
                report_dir = Path(args.report_dir)
                if not report_dir.is_absolute():
                    report_dir = root / report_dir
                report_dir.mkdir(parents=True, exist_ok=True)
                (report_dir / "worker-pool-load.json").write_text(json.dumps(report, indent=2) + "\n")
                print(json.dumps({key: report[key] for key in ("run_id", "ready_max_seconds", "rss_min_kib", "rss_max_kib", "exit_code")}))
            except BaseException:
                if process.poll() is not None:
                    logs.seek(0)
                    print(logs.read()[-5000:])
                raise
    finally:
        if process is not None and process.poll() is None:
            process.kill()
            process.wait()
        try:
            if behaviour_changed:
                request(push_admin, {"latency_ms": 0})
        finally:
            sql(f'DROP SCHEMA IF EXISTS "{schema}" CASCADE')


if __name__ == "__main__":
    main()
