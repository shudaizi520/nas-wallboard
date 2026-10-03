# Windows 桌面小组件

## 推荐安装方式

1. 在局域网登录 NAS Wallboard `/manage`。
2. 点击“安装桌面小组件”。这个 ZIP 已写入你当前访问的 NAS 地址。
3. 完整解压后双击 `NASWallboard.Desktop.exe`。

程序是自包含单文件，不要求安装 .NET；WebView2 Runtime 通常已随 Windows 11 提供。它不会更换壁纸，也不会在前台常驻一个普通窗口。

从 GitHub Release 下载的通用 ZIP 不包含你的地址。首次运行后右键通知区域 NAS 图标，选择“设置 NAS 地址”，填入 Wallboard 首页（例如 `http://nas-host:18082/`）。

## 移动、锁定和开机启动

右键通知区域的 NAS 图标：

- **移动位置**：解除锁定；随后直接在组件内按住鼠标拖动。
- **锁定**：保存当前位置并恢复鼠标穿透。
- **打开内容管理**：打开服务器 `/manage`，组件内容和排列都在那里修改。
- **刷新**：立即重新载入。
- **开机启动**：勾选时随当前 Windows 用户登录静默启动；取消即可关闭自启。
- **设置 NAS 地址**：修改服务器或端口。
- **关于与版本**：查看本机客户端版本及当前 NAS 服务版本；服务尚未成功加载时会明确显示尚未读取。
- **退出**：结束进程。

设置保存在 `%LocalAppData%\NASWallboard\settings.json`。点击 Windows“显示桌面”后，程序会重新附着桌面层，不应永久消失。

## 更新与故障恢复

NAS 服务和 Windows 客户端是两个独立版本。管理页还会显示服务器提供下载的客户端版本；普通浏览器打开管理页时，本机版本显示未知。从托盘“打开内容管理”进入时才会携带本机版本用于比较，该信息仅作显示，不用于身份验证。

更新客户端时先在托盘退出，下载并完整解压新 ZIP，替换原目录内的程序后重新启动。地址、位置等设置仍在 `%LocalAppData%\NASWallboard`，无需删除。开机启动记录使用程序路径，建议保留原安装路径；若更换目录，请在新位置重新勾选开机启动。仅更新 NAS 服务或点“刷新”不会替换已运行的桌面外壳。

初始化和导航等待期限为 30 秒；导航成功后仍须收到首次有效渲染，等待期限为 45 秒。启动失败、长时间未完成渲染或 WebView2 崩溃时显示恢复提示，前五次自动重试间隔为 1 分钟，之后每 10 分钟继续重试。托盘“刷新”和恢复页按钮可立即重试。恢复等待只在本机计时，不增加 NAS 数据采集频率；恢复页的临时窗口尺寸不覆盖已保存位置和锁定设置。

## 性能

客户端只承载同源的轻量 HTML/CSS 页面；无视频、WebGL 或持续动画。锁定且空闲时不持续重绘，任务管理器通常显示 0% CPU。WebView2 内存属于浏览器运行时，正常会高于原生托盘外壳；如果 CPU 长时间不回落，请记录版本并附上脱敏诊断，不要发送 Cookie 或完整备份。

## 常见问题

- **SmartScreen 未知发布者**：项目没有商业代码签名。确认 ZIP 来自自己 NAS 的管理页，或 Release 文件的 SHA-256 与 `SHA256SUMS` 一致，再选择“更多信息 → 仍要运行”。
- **双击没有反应**：确认已解压全部文件；查看 `%LocalAppData%\NASWallboard\startup.log`，也可运行 ZIP 中的诊断脚本。
- **托盘图标不见**：展开 Windows 通知区域；同一用户只允许运行一个实例。
- **组件位置不对**：选择“移动位置”后在组件内部拖动，再点“锁定”。显示器变化后重新启动会把窗口移回可见区域。
- **页面显示 NAS 未连接**：先用浏览器打开相同 NAS 地址；客户端会低频重试，也可点“刷新”。
- **缺少 WebView2**：安装 Microsoft Evergreen WebView2 Runtime 后退出并重新启动客户端。

## 从源码构建客户端

本地开发构建默认显示未知版本，避免冒充正式发布。正式发布流水线先校验标签，再通过 `WallboardReleaseVersion` 写入客户端版本，并生成 `desktop-version.txt` 随同客户端嵌入服务器。手动发布示例：

```sh
dotnet publish windows/NASWallboard.Desktop/NASWallboard.Desktop.csproj -c Release -r win-x64 --self-contained true -p:EnableWindowsTargeting=true -p:PublishSingleFile=true -p:WallboardReleaseVersion=v1.0.10 -o web/downloads
```

生成的客户端和版本文件应一起提供；缺少版本文件时管理页显示下载版本未知，不用 NAS 服务版本代替它。源码示例中的版本号需替换为实际发布标签。

## 卸载

在托盘取消“开机启动”，选择“退出”，删除解压目录。若不需要保留地址和位置，再删除 `%LocalAppData%\NASWallboard`。无需清理注册表中的其他应用项，也不需要 Rainmeter。
