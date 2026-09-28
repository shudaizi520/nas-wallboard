# TrueNAS SCALE 安装、升级与回滚

支持范围：TrueNAS SCALE 25.10 及更高版本，`linux/amd64`。NAS Wallboard 是局域网只读看板，不应直接暴露到公网。

## 1. 下载并核验发布文件

从项目 GitHub Release 下载：

- `nas-wallboard-truenas.yaml`
- `SHA256SUMS`
- 可选：`nas-wallboard.spdx.json`

在可信电脑上执行 `sha256sum -c SHA256SUMS`，确认 YAML 校验通过。发布 YAML 中的镜像使用 `sha256:` digest 固定，不使用 `latest`。

## 2. 在 TrueNAS 安装

1. 打开 **Apps → Discover Apps → Install via YAML**。
2. 粘贴 `nas-wallboard-truenas.yaml`。
3. 如果 `18082` 已占用，只修改端口映射左侧，例如把 `18082:8080` 改为 `18083:8080`；容器内的 `8080` 不变。
4. 保存并等待 `wallboard` 容器变为 Healthy。

YAML 自动创建独立持久卷。一次性初始化容器只负责把该卷设为 UID/GID 65532；长期运行的 Wallboard 不是 root，根文件系统只读，也没有 Docker Socket。

## 3. 创建 TrueNAS 专用账户和 API Key

不要使用日常管理员或 root 的密钥。

1. TrueNAS 打开 **Credentials → Users → Add**。
2. 创建仅供 Wallboard 使用的本地用户，启用 **TrueNAS Access**，选择 **Readonly Admin**；不要给 Shell、SSH 或 sudo 权限。
3. 在用户详情中选择 **Add API Key**，或从右上角账户菜单进入 **My API Keys**。
4. 为 Key 设置清楚的名称和合适的到期时间。保存后立即复制 Key；TrueNAS 只显示一次。

TrueNAS 25.10 的 API Key 是用户关联、等同该用户 API 权限的凭据。API Key 认证要求 HTTPS/WSS；请优先配置可信证书。Wallboard 的连接地址一般是：

```text
wss://你的-TrueNAS-主机名/api/current
```

如果只在可信局域网使用自签名证书，可在向导中明确选择跳过证书校验；这会降低中间人攻击防护，不适合公网。

## 4. 首次设置

打开：

```text
http://你的-NAS-地址:18082/setup
```

向导会：

1. 确认 TrueNAS 版本；
2. 收集 TrueNAS WSS 地址、专用用户名和 API Key；
3. 实际连接并逐项检查系统、存储池、磁盘、Apps、告警和复制任务读取权限；
4. 创建至少 12 字符的 Wallboard 本地管理密码；
5. 发现可用存储池、磁盘、网卡和 Apps；
6. 预览并原子保存配置。

浏览器不会把 API Key 写入 URL、Local Storage 或页面 HTML。设置失败时不会用半套配置覆盖最后一次有效状态。

## 5. 日常管理

打开 `/manage` 并使用 Wallboard 管理密码登录：

- **概览**：TrueNAS/集成健康、版本、运行时间和更新提示。
- **桌面内容**：添加组件、调整顺序、阈值、数据源与面板宽度，预览即桌面实际效果。
- **集成中心**：添加、测试、禁用或删除 QWeather、Plex、Jellyfin、qBittorrent、Uptime Kuma、Home Assistant 和 Scrutiny。
- **设置与恢复**：脱敏诊断、完整加密备份、修改密码、恢复出厂。

集成配置中的密钥字段保存后只显示“已配置”，不会从 API 读回。更改非密钥字段无需重新输入旧密钥。

## 6. Windows 桌面

在管理页点击“安装桌面小组件”。服务器会实时生成 ZIP，并写入当前访问 NAS 的地址：

1. 完整解压 ZIP；
2. 双击 `NASWallboard.Desktop.exe`；
3. Windows SmartScreen 出现“未知发布者”时，先确认 ZIP 来自自己的 NAS 且校验来源可信，再选择运行；
4. 位置、锁定、开机启动和 NAS 地址都在通知区域的 NAS 图标中设置。

详见 [Windows 桌面小组件](windows-desktop-widget.md)。

## 7. 备份与恢复准备

升级或重置前，在 **设置与恢复**：

1. 下载脱敏诊断包，记录当前版本与连接状态；
2. 下载完整加密备份；
3. 把备份密码存到密码管理器，和 `.age` 文件分开保存；
4. 保留上一版本 YAML、镜像 digest 和备份，直到新版本稳定运行。

恢复出厂只删除 Wallboard 自己在持久卷中的配置、认证与密钥，不会调用 TrueNAS、Plex 或其他集成的删除接口。

## 8. 升级与回滚

升级：

1. 下载新 Release 的 YAML 与校验和；
2. 先完成完整加密备份；
3. 在 TrueNAS 编辑 App YAML，保留同一个 `nas-wallboard-data` 卷，换成新 YAML 中 digest 固定的镜像；
4. 检查 `/healthz`、`/readyz`、管理页概览、桌面显示和关键集成；
5. 至少稳定运行 12 小时后再清理旧镜像记录。

回滚时重新粘贴上一版本 YAML，仍挂载原持久卷。不要在回滚同时恢复出厂或删除卷；先确认管理页和桌面恢复。

## 9. 排障

- `/healthz`：进程存活。
- `/readyz`：TrueNAS 核心数据已成功采集。
- 首次设置连接失败：确认使用 `wss://`、API Key 所属用户名正确、Key 未过期/撤销、Readonly Admin 权限存在。
- 管理页提示某集成异常：在集成中心重新测试；不要把原始 Token 发到 Issue。
- 需要求助：下载脱敏诊断 ZIP。完整加密备份只用于自己保管，不能作为公开附件。

## 10. 卸载

先在 Windows 托盘退出小组件并取消开机启动，再从 TrueNAS Apps 删除 Wallboard App。默认保留 `nas-wallboard-data` 卷以便回滚；只有在确认不再需要配置、密钥和备份后，才在 TrueNAS 的存储界面单独删除该卷。不要运行宽泛的递归删除命令。
