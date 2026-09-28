namespace NASWallboard.Desktop.Core;

public readonly record struct DesktopWindow(nint Handle, bool IsWorker, bool HasShellView);

public static class DesktopHostSelection
{
    public static nint Select(IReadOnlyList<DesktopWindow> windows, nint programManager)
    {
        for (var index = 0; index < windows.Count; index++)
        {
            if (!windows[index].HasShellView) continue;
            for (var candidate = index + 1; candidate < windows.Count; candidate++)
            {
                if (windows[candidate].IsWorker) return windows[candidate].Handle;
            }
            break;
        }
        return programManager;
    }
}
