"""Own a disposable TLS ClickHouse generation for the Go integration test (stdin controls lifetime)."""

import fcntl
import json
from pathlib import Path
import secrets
import subprocess
import sys
import time
import uuid

ROOT = Path(sys.argv[1])
IMAGE = "clickhouse/clickhouse-server:26.8.34.13@sha256:8b28eb09479c36c4533eec7a6268917d04ba3a6fd90e3f4205f70e761b6d7779"
volume = "controlhub-ch-preview-" + uuid.uuid4().hex
owned = []
secret = secrets.token_hex(24)


def run(*args, data=None, check=True):
    p = subprocess.run(args, text=True, input=data, capture_output=True, timeout=120)
    if check and p.returncode:
        raise RuntimeError(f"{args[0]} {args[1]} failed: {p.stderr[-1200:]}")
    return p.stdout.strip() if check else p


def start(serve=False):
    args = [
        "docker",
        "run",
        "-d",
        "--memory",
        "2g",
        "--cpus",
        "2",
        "-v",
        f"{volume}:/var/lib/clickhouse",
    ]
    if serve:
        for src, dest in [
            ("reader.xml", "users.xml"),
            ("tls.xml", "config.d/preview.xml"),
            ("ca.pem", "server.crt"),
            ("key.pem", "server.key"),
        ]:
            args.extend(["-v", f"{ROOT / src}:/etc/clickhouse-server/{dest}:ro"])
        args.extend(
            [
                "--user",
                "101:101",
                "--cap-drop",
                "ALL",
                "--security-opt",
                "no-new-privileges",
                "-p",
                "127.0.0.1::9440",
                "--entrypoint",
                "/usr/bin/clickhouse-server",
                IMAGE,
                "--config-file=/etc/clickhouse-server/config.xml",
            ]
        )
    else:
        args.extend(
            [
                "--network",
                "none",
                "-e",
                "CLICKHOUSE_USER=builder",
                "-e",
                "CLICKHOUSE_PASSWORD=" + secret,
                "-e",
                "CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1",
                IMAGE,
            ]
        )
    cid = run(*args)
    owned.append(cid)
    user = "preview_reader" if serve else "builder"
    client = [
        "docker",
        "exec",
        "-i",
        cid,
        "clickhouse-client",
        "--user",
        user,
        "--password",
        secret,
    ]
    for _ in range(100):
        if run(*client, "--query", "SELECT 1", check=False).returncode == 0:
            return cid, client
        time.sleep(0.2)
    raise RuntimeError("fixture database startup failed")


run("docker", "volume", "create", volume)
try:
    with (ROOT / "lease").open("w") as lease:
        fcntl.flock(lease, fcntl.LOCK_EX)
        # A fresh CA/leaf is generated for each generation; the test only trusts this endpoint.
        (ROOT / "cert.cnf").write_text(
            "[req]\ndistinguished_name=dn\nx509_extensions=ext\nprompt=no\n[dn]\nCN=controlhub-preview-fixture\n[ext]\nbasicConstraints=critical,CA:TRUE\nsubjectAltName=IP:127.0.0.1\nextendedKeyUsage=serverAuth\n"
        )
        run(
            "openssl",
            "req",
            "-x509",
            "-nodes",
            "-newkey",
            "rsa:2048",
            "-days",
            "1",
            "-keyout",
            str(ROOT / "key.pem"),
            "-out",
            str(ROOT / "ca.pem"),
            "-config",
            str(ROOT / "cert.cnf"),
        )
        (ROOT / "key.pem").chmod(0o444)
        (ROOT / "tls.xml").write_text(
            "<clickhouse><tcp_port_secure>9440</tcp_port_secure><openSSL><server><certificateFile>/etc/clickhouse-server/server.crt</certificateFile><privateKeyFile>/etc/clickhouse-server/server.key</privateKeyFile><verificationMode>none</verificationMode></server></openSSL></clickhouse>"
        )
        tables = ["sample", "large_cell", "large_reply", "slow", "late", "server_budget"]
        grants = "".join(
            f"<query>GRANT SELECT(id,title,secret) ON g2.{name}</query>"
            for name in tables
        )
        (ROOT / "reader.xml").write_text(
            f"""<clickhouse><profiles><default/><limited><readonly>1</readonly><allow_ddl>0</allow_ddl><max_execution_time>2</max_execution_time><timeout_before_checking_execution_speed>0</timeout_before_checking_execution_speed><max_rows_to_read>10000</max_rows_to_read><max_result_bytes>1048576</max_result_bytes><max_result_rows>32</max_result_rows><max_memory_usage>67108864</max_memory_usage><max_threads>1</max_threads><max_block_size>1</max_block_size></limited></profiles><quotas><default/></quotas><users><preview_reader><password>{secret}</password><networks><ip>::/0</ip></networks><profile>limited</profile><quota>default</quota><grants>{grants}</grants></preview_reader></users></clickhouse>"""
        )
        builder, client = start()
        run(
            *client,
            "--multiquery",
            data=Path(__file__).with_name("seed.sql").read_text(),
        )
        run("docker", "stop", builder)
        run("docker", "rm", "-v", builder)
        owned.remove(builder)
        cid, _ = start(True)
        addr = run("docker", "port", cid, "9440/tcp")
        print(json.dumps({"addr": addr, "password": secret}), flush=True)
    sys.stdin.read()
finally:
    for cid in owned:
        run("docker", "rm", "-fv", cid, check=False)
    run("docker", "volume", "rm", volume)
