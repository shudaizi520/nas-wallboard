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
- **退出**：结束进程。

设置保存在 `%LocalAppData%\NASWallboard\settings.json`。点击 Windows“显示桌面”后，程序会重新附着桌面层，不应永久消失。

## 性能

客户端只承载同源的轻量 HTML/CSS 页面；无视频、WebGL 或持续动画。锁定且空闲时不持续重绘，任务管理器通常显示 0% CPU。WebView2 内存属于浏览器运行时，正常会高于原生托盘外壳；如果 CPU 长时间不回落，请记录版本并附上脱敏诊断，不要发送 Cookie 或完整备份。

## 常见问题

- **SmartScreen 未知发布者**：项目没有商业代码签名。确认 ZIP 来自自己 NAS 的管理页，或 Release 文件的 SHA-256 与 `SHA256SUMS` 一致，再选择“更多信息 → 仍要运行”。
- **双击没有反应**：确认已解压全部文件；查看 `%LocalAppData%\NASWallboard\startup.log`，也可运行 ZIP 中的诊断脚本。
- **托盘图标不见**：展开 Windows 通知区域；同一用户只允许运行一个实例。
- **组件位置不对**：选择“移动位置”后在组件内部拖动，再点“锁定”。显示器变化后重新启动会把窗口移回可见区域。
- **页面显示 NAS 未连接**：先用浏览器打开相同 NAS 地址；客户端会低频重试，也可点“刷新”。
- **缺少 WebView2**：安装 Microsoft Evergreen WebView2 Runtime 后重试。

## 卸载

在托盘取消“开机启动”，选择“退出”，删除解压目录。若不需要保留地址和位置，再删除 `%LocalAppData%\NASWallboard`。无需清理注册表中的其他应用项，也不需要 Rainmeter。
