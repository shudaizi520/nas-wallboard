# NAS Wallboard

面向 **TrueNAS SCALE 25.10+** 的轻量桌面状态小组件。NAS 上只运行一个非 root Go 进程；浏览器和 Windows 桌面端共用同一套界面，不需要 Rainmeter，也不需要用户编辑配置文件。

## 能显示什么

- CPU 使用率与温度、可用内存、网速、机械硬盘温度与 SMART 告警
- 存储池容量、TrueNAS 告警、应用异常、复制任务异常
- 天气和预警
- Plex / Jellyfin 多终端播放（每个会话单独一行）
- qBittorrent、Uptime Kuma、Home Assistant、Scrutiny
- 可在网页中添加、禁用、排序和设置组件，桌面实时复用结果

空闲的媒体来源每分钟查询一次；检测到播放后才提高到 15 秒。停用的集成不会在后台发请求。Windows 客户端空闲时不持续重绘，典型任务管理器占用应稳定在 0% CPU 附近。

## 安装

1. 在 GitHub Releases 下载 `nas-wallboard-truenas.yaml` 和 `SHA256SUMS`，先核对校验值。
2. TrueNAS 打开 **Apps → Discover Apps → Install via YAML**，粘贴 YAML 并保存。
3. 打开 `http://你的-NAS-地址:18082/setup`。
4. 按向导创建并验证 TrueNAS 专用只读账户/API Key，再设置 Wallboard 管理密码。
5. 在 `/manage` 添加集成、选择组件；点击“安装桌面小组件”下载已写入当前 NAS 地址的 Windows ZIP。

详细步骤、升级和回滚见 [TrueNAS 安装说明](docs/truenas-install.md)，桌面端见 [Windows 小组件说明](docs/windows-desktop-widget.md)。

## 安全设计

- 只支持 TrueNAS SCALE 25.10+ 的 WebSocket JSON-RPC API，不调用 TrueNAS 删除或修改接口。
- API Key、Token 和密码只保存于 `/data/secrets` 或 Argon2id 密码记录；管理 API 永不返回原值。
- `/manage` 需要本地密码会话、同源校验与 CSRF；完整备份和恢复出厂还要求一次性重新认证。
- “集成中心”仅使用编译进程序的内置集成，不下载或执行第三方插件。
- 容器为 UID/GID 65532、只读根文件系统、无 Docker Socket、全部 capabilities 丢弃，并限制为 128 MiB、0.5 CPU、64 PID。
- 应用定位为可信局域网工具，不要直接映射到公网；需要跨网访问时请使用自己维护的 TLS VPN 或反向代理。

## 备份与排障

管理页“设置与恢复”可下载两种文件：

- 脱敏诊断 ZIP：不含密钥，适合提交 Issue。
- 完整加密备份：包含配置与密钥，必须重新验证管理员密码，并用至少 12 字符的独立密码加密。

不要把完整备份、API Key、Cookie、硬盘序列号或未经检查的日志上传到公开 Issue。

## 从源码验证

```sh
go test -race ./...
go vet ./...
npm ci
npm test
npm run test:e2e
dotnet test windows/NASWallboard.Desktop.sln -c Release
```

本地镜像先运行 `make image`；它会构建 Windows 客户端，并把同一份可执行文件嵌入服务镜像。公开版本由标签触发的流水线生成 digest 固定的镜像、TrueNAS YAML、Windows ZIP、SHA-256 校验和与 SPDX SBOM。

## 项目范围

项目只支持 TrueNAS SCALE，不计划适配 Synology、Unraid、TrueNAS CORE 或通用 Linux 主机。问题报告和贡献规则见 [CONTRIBUTING.md](CONTRIBUTING.md)，安全问题见 [SECURITY.md](SECURITY.md)。

MIT License
