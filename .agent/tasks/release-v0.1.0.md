# v0.1.0 发布

授权：2026-10-03 用户要求提交本次修改并发布 Release

范围：Linux 二进制部署脚本、部署文档、systemd 模板、生产样例端口 18763、网页标题与 favicon；保留未跟踪的 bin 目录，不发布本地凭据

版本：仓库无已有标签或 Release，首个版本为 v0.1.0，提供 Linux amd64 完整部署包与 SHA-256 校验文件

状态：已完成，Release 已公开发布

提交：1534369（部署与网页标识）、eae1447（脚本执行权限）；main 和 v0.1.0 标签已推送，远程标签指向 eae14474caee500a2740561ac28b37687ad27aa0

发布：https://github.com/helantianshen/shiftory/releases/tag/v0.1.0

产物：shiftory-v0.1.0-linux-amd64.tar.gz、checksums.txt；下载包 SHA-256 与本地一致，3ffaa3660d9366610277b82289c33ba0d3d1320cf35c9793bc9183b6ee4bf25f

本地任务记录未纳入代码提交，原有 bin 目录保持未跟踪

验证：前端 28 项测试、后端配置测试、Shell 语法检查、6 项部署脚本安全与模板检查、git diff --check 均通过

验证边界：不在本机安装服务、创建应用数据库或修改共享容器；基础服务使用替身验证，systemd 模板执行静态校验
