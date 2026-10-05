# Linux 部署步骤

按以下顺序操作即可。构建机需要 Go、Node.js 24.12+（含 npm）；服务器需要安装好 Docker，使用支持 systemd 的 Linux。默认服务器为 x86_64。

## 1. 构建部署包

在项目根目录执行：

```bash
./scripts/build-release.sh /tmp/shiftory-package
```

成功后得到 `/tmp/shiftory-package`，里面已经包含后端、前端、配置和部署脚本。如果提示目录已存在，换一个新的输出目录。ARM64 服务器在命令前加 `GOARCH=arm64`。

## 2. 上传到服务器

用 SFTP 等工具把部署包里的全部内容上传到服务器的 `/opt/shiftory`。最终目录应为：

```text
/opt/shiftory/
├── bin/
├── web/
├── config/production.yaml
├── scripts/
├── deploy/
└── DEPLOYMENT.md
```

注意：应为 `/opt/shiftory/bin`，不要多套一层 `shiftory-package`。

## 3. 启动数据库和 Redis

在服务器执行：

```bash
cd /opt/shiftory
sudo bash scripts/start-services.sh
```

按提示输入两次密码：第一项是 PostgreSQL 管理密码，第二项是 **Shiftory 数据库密码**，记住第二项，下一步需要填写。输入时屏幕不会显示字符。

脚本自动拉取并启动 PostgreSQL、Redis，创建应用数据库，每一步都会显示状态。若提示同名容器已存在，则保留已有服务；数据库密码使用原有的 Shiftory 密码。如果现有实例还没有 Shiftory 数据库，按文末的“已有数据库”操作。

## 4. 填写配置

打开服务器上的配置文件：

```bash
sudoedit /opt/shiftory/config/production.yaml
```

找到以下配置，填写数据库密码和访问地址，端口可按需修改：

```yaml
postgres:
  password: "YOUR_SHIFTORY_DATABASE_PASSWORD"
server:
  port: 18763
  public_origin: http://YOUR_SERVER_IP:18763
```

- `password`：填写上一步的 **Shiftory 数据库密码**，不是管理密码。
- `port`：应用监听端口，默认 `18763`。如果已被占用，直接在配置文件里换一个空闲端口。
- `public_origin`：填写浏览器实际访问地址。直接使用服务器 IP 时，填 `http://你的服务器IP:18763`，端口与 `port` 一致；已配置 HTTPS 反向代理时，填 `https://你的域名`，代理转发到 `port` 指定的端口。不要加末尾 `/`。

上面只是需要修改的字段位置，不要用这几行替换整个配置文件。AI 默认关闭。改端口后执行 `sudo systemctl restart shiftory`，不需要修改 service 文件。

## 5. 安装并启动应用

```bash
cd /opt/shiftory
sudo bash scripts/install-service.sh
sudo systemctl status shiftory --no-pager
```

脚本自动准备目录权限、创建 service 文件、填写部署路径并启动应用，同时设置开机自启，不需要手动编辑 service。状态显示 `active (running)` 后，等待应用初始化完成，在浏览器打开第 4 步填写的地址，通过注册页面创建账号。如需健康检查，在该访问地址后加 `/health`，响应应包含 `"status":"ok"`。

直接通过 IP 访问时，确保服务器防火墙或云安全组允许配置的应用端口，默认是 18763。

生产日志默认写入部署目录的 `var/logs/shiftory.log`，控制台文本仍可通过 journalctl 查看。文件大小和备份数在 `config/production.yaml` 的 `log.file.max_size_mb` 与 `max_backups` 配置，默认每文件 20 MiB、两个备份，共 60 MiB；迁移日志 `shiftory.migrate.log` 独立计算限额。文件轮转不会限制 systemd journal 的存储占用。默认日志目录位于安装脚本已授权的 `var` 下；自定义路径需由服务用户可写，并符合 systemd 的目录限制。

## 常用命令

```bash
# 查看应用状态
sudo systemctl status shiftory --no-pager
# 查看最近的错误和日志
sudo journalctl -u shiftory -n 50 --no-pager
# 修改配置后重启
sudo systemctl restart shiftory
# 停止应用
sudo systemctl stop shiftory
```

## 更新应用

重新构建并上传新部署包，先备份数据库和部署目录，再停止应用：

```bash
sudo systemctl stop shiftory
```

只替换 `/opt/shiftory/bin/` 和 `/opt/shiftory/web/`，然后启动：

```bash
sudo systemctl start shiftory
sudo systemctl status shiftory --no-pager
```

保留原来的 `config/`、`uploads/`、`var/` 和 Docker 数据卷。API 启动会自动同步数据库结构。

## 已有数据库，但尚未创建 Shiftory 数据库

仅在需要时执行：

```bash
sudo docker exec -it postgres psql -U postgres -d postgres
```

进入后依次执行，设置的密码填到第 4 步：

```sql
CREATE ROLE shiftory LOGIN;
\password shiftory
CREATE DATABASE shiftory OWNER shiftory TEMPLATE template0 ENCODING 'UTF8';
\q
```

如果提示账号或数据库已经存在，不要重复创建，使用原有配置。基础服务脚本不会修改已有容器或清空数据；缺少容器但存在 PostgreSQL 数据卷时会停止，需先确认数据恢复方式。
