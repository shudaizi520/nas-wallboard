using System.Text;

namespace NASWallboard.Desktop;

internal static class StartupLog
{
    private static readonly object Sync = new();
    private static int dialogShown;

    internal static string PathName { get; } = Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
        "NASWallboard",
        "startup.log");

    internal static void Write(string message)
    {
        try
        {
            lock (Sync)
            {
                Directory.CreateDirectory(Path.GetDirectoryName(PathName)!);
                File.AppendAllText(PathName,
                    $"{DateTimeOffset.Now:O} [{Environment.ProcessId}] {message}{Environment.NewLine}",
                    new UTF8Encoding(false));
            }
        }
        catch
        {
            // Diagnostics must never become another startup failure.
        }
    }

    internal static void Report(Exception exception)
    {
        Write($"FATAL {exception}");
        if (Interlocked.Exchange(ref dialogShown, 1) != 0) return;
        try
        {
            MessageBox.Show(
                $"NAS 桌面组件启动失败。\r\n\r\n{exception.GetType().Name}: {exception.Message}\r\n\r\n日志：{PathName}",
                "NAS 桌面组件",
                MessageBoxButtons.OK,
                MessageBoxIcon.Error);
        }
        catch
        {
            // The companion diagnostic launcher can still collect the event log.
        }
    }
}
