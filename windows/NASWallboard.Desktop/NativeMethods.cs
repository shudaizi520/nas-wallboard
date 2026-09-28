using System.Runtime.InteropServices;
using System.Text;
using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop;

internal static class NativeMethods
{
    private const uint SpawnDesktopWorker = 0x052C;
    private const uint SendMessageTimeoutNormal = 0;
    private const int GWL_STYLE = -16;
    internal const int GWL_EXSTYLE = -20;
    private const long WS_CHILD = 0x40000000L;
    private const long WS_POPUP = 0x80000000L;
    private const int VK_LBUTTON = 0x01;
    internal const nint WS_EX_TOOLWINDOW = 0x00000080;
    internal const nint WS_EX_TRANSPARENT = 0x00000020;
    internal const nint WS_EX_NOACTIVATE = 0x08000000;
    internal const uint SWP_NOSIZE = 0x0001;
    internal const uint SWP_NOMOVE = 0x0002;
    internal const uint SWP_NOZORDER = 0x0004;
    internal const uint SWP_NOACTIVATE = 0x0010;
    internal const uint SWP_FRAMECHANGED = 0x0020;

    private delegate bool EnumWindowsCallback(nint window, nint parameter);

    [StructLayout(LayoutKind.Sequential)]
    private struct NativePoint
    {
        internal int X;
        internal int Y;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct NativeRect
    {
        internal int Left;
        internal int Top;
        internal int Right;
        internal int Bottom;
    }

    [DllImport("user32.dll", EntryPoint = "GetWindowLongPtrW")]
    internal static extern nint GetWindowLongPtr(nint window, int index);

    [DllImport("user32.dll", EntryPoint = "SetWindowLongPtrW")]
    internal static extern nint SetWindowLongPtr(nint window, int index, nint value);

    [DllImport("user32.dll", EntryPoint = "FindWindowW", CharSet = CharSet.Unicode)]
    private static extern nint FindWindow(string? className, string? windowName);

    [DllImport("user32.dll", EntryPoint = "FindWindowExW", CharSet = CharSet.Unicode)]
    private static extern nint FindWindowEx(nint parent, nint childAfter, string? className, string? windowName);

    [DllImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool EnumWindows(EnumWindowsCallback callback, nint parameter);

    [DllImport("user32.dll", EntryPoint = "GetClassNameW", CharSet = CharSet.Unicode)]
    private static extern int GetClassName(nint window, StringBuilder className, int maximumCount);

    [DllImport("user32.dll", EntryPoint = "SendMessageTimeoutW", CharSet = CharSet.Unicode)]
    private static extern nint SendMessageTimeout(
        nint window,
        uint message,
        nint wParam,
        nint lParam,
        uint flags,
        uint timeout,
        out nint result);

    [DllImport("user32.dll")]
    private static extern nint SetParent(nint child, nint newParent);

    [DllImport("user32.dll")]
    private static extern nint GetParent(nint window);

    [DllImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool GetCursorPos(out NativePoint point);

    [DllImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool GetWindowRect(nint window, out NativeRect rect);

    [DllImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool ScreenToClient(nint window, ref NativePoint point);

    [DllImport("user32.dll")]
    private static extern short GetAsyncKeyState(int virtualKey);

    [DllImport("user32.dll", EntryPoint = "SetWindowPos")]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static extern bool SetWindowPos(
        nint window,
        nint insertAfter,
        int x,
        int y,
        int width,
        int height,
        uint flags);

    internal static bool TryAttachToDesktop(nint window)
    {
        var programManager = FindWindow("Progman", null);
        if (programManager == 0) return false;

        SendMessageTimeout(
            programManager,
            SpawnDesktopWorker,
            0,
            0,
            SendMessageTimeoutNormal,
            1_000,
            out _);

        var windows = new List<DesktopWindow>();
        EnumWindows((candidate, _) =>
        {
            var className = new StringBuilder(64);
            GetClassName(candidate, className, className.Capacity);
            windows.Add(new DesktopWindow(
                candidate,
                className.ToString().Equals("WorkerW", StringComparison.Ordinal),
                FindWindowEx(candidate, 0, "SHELLDLL_DefView", null) != 0));
            return true;
        }, 0);

        var desktopHost = DesktopHostSelection.Select(windows, programManager);
        if (desktopHost == 0) return false;

        var style = (long)GetWindowLongPtr(window, GWL_STYLE);
        style = style & ~WS_POPUP | WS_CHILD;
        SetWindowLongPtr(window, GWL_STYLE, (nint)style);
        SetParent(window, desktopHost);
        SetWindowPos(window, 0, 0, 0, 0, 0,
            SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE | SWP_FRAMECHANGED);
        return GetParent(window) == desktopHost;
    }

    internal static bool TryGetCursorPosition(out PixelPoint point)
    {
        if (!GetCursorPos(out var nativePoint))
        {
            point = default;
            return false;
        }

        point = new PixelPoint(nativePoint.X, nativePoint.Y);
        return true;
    }

    internal static bool IsLeftMouseButtonDown() => (GetAsyncKeyState(VK_LBUTTON) & 0x8000) != 0;

    internal static bool TryGetWindowBounds(nint window, out PixelRect bounds)
    {
        if (!GetWindowRect(window, out var rect))
        {
            bounds = default;
            return false;
        }

        bounds = new PixelRect(rect.Left, rect.Top, rect.Right - rect.Left, rect.Bottom - rect.Top);
        return true;
    }

    internal static bool TryMoveWindowToScreenPosition(nint window, PixelPoint location)
    {
        var nativePoint = new NativePoint { X = location.X, Y = location.Y };
        var parent = GetParent(window);
        if (parent != 0 && !ScreenToClient(parent, ref nativePoint)) return false;
        return SetWindowPos(window, 0, nativePoint.X, nativePoint.Y, 0, 0,
            SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE);
    }

    internal static bool TrySetWindowBoundsFromScreen(nint window, ScreenRect bounds)
    {
        var nativePoint = new NativePoint { X = bounds.X, Y = bounds.Y };
        var parent = GetParent(window);
        if (parent != 0 && !ScreenToClient(parent, ref nativePoint)) return false;
        return SetWindowPos(window, 0, nativePoint.X, nativePoint.Y, bounds.Width, bounds.Height,
            SWP_NOZORDER | SWP_NOACTIVATE);
    }
}
