namespace NASWallboard.Desktop.Core;

public sealed record AppSettings(string ServerUrl, int X, int Y, bool Locked, bool AutoStart)
{
    public static AppSettings Default { get; } = new("http://127.0.0.1:18082/", -1, -1, true, true);
}
