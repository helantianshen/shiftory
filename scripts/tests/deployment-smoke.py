#!/usr/bin/env python3
"""通过临时 Docker 替身验证初始化、既有数据保护和失败中止，不访问真实服务"""

import json
import os
from pathlib import Path
import subprocess
import tempfile


root = Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory() as directory:
    temporary = Path(directory)
    docker = temporary / "docker"
    docker.write_text("""#!/usr/bin/env python3
import json, os, sys
args = sys.argv[1:]
with open(os.environ['MOCK_LOG'], 'a') as log:
    log.write(json.dumps(args) + '\\n')
mode = os.environ['MOCK_MODE']
if args[:2] == ['container', 'inspect']:
    sys.exit(0 if mode == 'existing' else 1)
if args[:2] == ['volume', 'inspect']:
    sys.exit(0 if mode == 'volume' else 1)
if args[0] == 'pull' and mode == 'pull-failure':
    sys.exit(1)
if args[0] == 'inspect':
    print('healthy')
if args[:2] == ['exec', '-i']:
    assert os.environ['SHIFTORY_POSTGRES_PASSWORD'] == "app' $password"
    sql = sys.stdin.read()
    assert "PASSWORD :'app_password'" in sql
    assert '\\\\getenv app_password SHIFTORY_POSTGRES_PASSWORD' in sql
if args[0] == 'exec' and 'redis-cli' in args:
    print('PONG')
""")
    docker.chmod(0o755)

    # 密码经环境传入容器，不能展开到命令参数或状态输出
    for mode in ("new", "existing", "volume", "pull-failure"):
        log = temporary / f"{mode}.jsonl"
        environment = dict(os.environ, PATH=f"{temporary}:{os.environ['PATH']}",
                           MOCK_MODE=mode, MOCK_LOG=str(log), POSTGRES_PORT="15432", REDIS_PORT="16379")
        result = subprocess.run(["bash", str(root / "scripts/start-services.sh")],
                                input="admin-secret\napp' $password\n", text=True,
                                env=environment, capture_output=True)
        calls = [json.loads(line) for line in log.read_text().splitlines()]
        assert "admin-secret" not in result.stdout + result.stderr + log.read_text()
        assert "app' $password" not in result.stdout + result.stderr + log.read_text()
        mutations = [call for call in calls if call[0] in ("pull", "run", "exec", "start")]
        if mode == "existing":
            assert result.returncode == 0, result.stderr
            assert not mutations
        elif mode == "volume":
            assert result.returncode != 0
            assert not mutations
        elif mode == "pull-failure":
            assert result.returncode != 0
            assert [call[0] for call in mutations] == ["pull"]
        else:
            assert result.returncode == 0, result.stderr
            runs = [call for call in calls if call[0] == "run"]
            assert len(runs) == 2
            assert "127.0.0.1:15432:5432" in runs[0]
            assert "postgres-data:/var/lib/postgresql" in runs[0]
            assert "127.0.0.1:16379:6379" in runs[1]
            assert "--appendonly" in runs[1] and "noeviction" in runs[1]
        print(f"PASS: {mode}")

    # 安装权限不足时必须停止，不能写入系统配置
    if os.geteuid() != 0:
        result = subprocess.run(["bash", str(root / "scripts/install-service.sh")],
                                text=True, capture_output=True)
        assert result.returncode != 0 and "sudo bash" in result.stderr
        print("PASS: install permission guard")

    binary = temporary / "bin/shiftory-api"
    binary.parent.mkdir()
    binary.write_text("#!/bin/sh\nexit 0\n")
    binary.chmod(0o755)
    unit = temporary / "shiftory.service"
    unit.write_text((root / "deploy/shiftory.service").read_text().replace(
        "/YOUR/DEPLOY/DIRECTORY", str(temporary)))
    assert "YOUR_" not in unit.read_text()
    result = subprocess.run(["systemd-analyze", "verify", str(unit)],
                            text=True, capture_output=True)
    assert result.returncode == 0, result.stderr
    print("PASS: service path substitution")
