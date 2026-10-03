using Microsoft.Win32;
using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop;

internal sealed class TrayMenu : IDisposable
{
    private const string RunKey = @"Software\Microsoft\Windows\CurrentVersion\Run";
    private const string RunValue = "NASWallboard";
    private readonly DesktopForm owner;
    private readonly NotifyIcon icon;
    private readonly ToolStripMenuItem lockItem;
    private readonly ToolStripMenuItem autoStartItem;
    private string serverVersion = "尚未读取";

    internal TrayMenu(DesktopForm owner)
    {
        this.owner = owner;
        lockItem = new ToolStripMenuItem("移动位置", null, (_, _) => owner.ToggleLocked());
        autoStartItem = new ToolStripMenuItem("开机启动", null, (_, _) => ToggleAutoStart()) { Checked = owner.Settings.AutoStart };
        var menu = new ContextMenuStrip();
        menu.Items.AddRange([
            lockItem,
            new ToolStripMenuItem("打开内容管理", null, (_, _) => owner.OpenManagement()),
            new ToolStripMenuItem("刷新", null, (_, _) => owner.RefreshDashboard()),
            autoStartItem,
            new ToolStripMenuItem("设置 NAS 地址", null, (_, _) => owner.ChangeServerAddress()),
            new ToolStripMenuItem("关于与版本", null, (_, _) => ShowVersions()),
            new ToolStripMenuItem("退出", null, (_, _) => owner.ExitApplication()),
        ]);

        icon = new NotifyIcon
        {
            Icon = Icon.ExtractAssociatedIcon(Application.ExecutablePath) ?? SystemIcons.Application,
            Text = "NAS 桌面组件",
            ContextMenuStrip = menu,
            Visible = true,
        };
        icon.DoubleClick += (_, _) => owner.ToggleLocked();
        UpdateLocked(owner.Settings.Locked);
        ApplyAutoStart(owner.Settings.AutoStart, false);
    }

    internal void UpdateLocked(bool locked)
    {
        lockItem.Text = locked ? "移动位置" : "锁定";
        icon.Text = locked ? "NAS 桌面组件（已锁定）" : "NAS 桌面组件（可移动）";
    }

    internal void UpdateServerVersion(string version)
    {
        serverVersion = ReleaseVersion.Normalize(version) ?? "未知版本";
    }

    internal void ResetServerVersion() => serverVersion = "尚未读取";

    private void ShowVersions()
    {
        MessageBox.Show($"Windows 客户端：{DesktopClientVersion.Current}\r\nNAS 服务端：{serverVersion}\r\n\r\n网页内容变更可刷新；客户端程序更新需退出后下载并替换。\r\n从托盘打开内容管理，可核对服务器提供的下载版本。",
            "NAS 桌面组件 · 关于与版本", MessageBoxButtons.OK, MessageBoxIcon.Information);
    }

    private void ToggleAutoStart()
    {
        ApplyAutoStart(!autoStartItem.Checked, true);
    }

    private void ApplyAutoStart(bool enabled, bool persist)
    {
        try
        {
            using var key = Registry.CurrentUser.CreateSubKey(RunKey);
            if (enabled)
                key.SetValue(RunValue, StartupCommand.ForExecutable(Application.ExecutablePath));
            else
                key.DeleteValue(RunValue, false);
            autoStartItem.Checked = enabled;
            if (persist) owner.SetAutoStart(enabled);
        }
        catch (UnauthorizedAccessException)
        {
            if (persist) MessageBox.Show("无法修改当前用户的开机启动设置。", "NAS 桌面组件",
                MessageBoxButtons.OK, MessageBoxIcon.Warning);
        }
    }

    public void Dispose()
    {
        icon.Visible = false;
        icon.ContextMenuStrip?.Dispose();
        icon.Dispose();
    }
}
