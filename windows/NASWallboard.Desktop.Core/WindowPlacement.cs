namespace NASWallboard.Desktop.Core;

public readonly record struct ScreenRect(int X, int Y, int Width, int Height)
{
    public int Right => X + Width;
    public int Bottom => Y + Height;
}

public static class WindowPlacement
{
    public static ScreenRect Clamp(ScreenRect window, IReadOnlyList<ScreenRect> workAreas)
    {
        if (workAreas.Count == 0) return window;
        var centerX = window.X + window.Width / 2d;
        var centerY = window.Y + window.Height / 2d;
        var area = workAreas.MinBy(candidate => DistanceSquared(centerX, centerY, candidate));
        var width = Math.Min(Math.Max(window.Width, 1), area.Width);
        var height = Math.Min(Math.Max(window.Height, 1), area.Height);
        var x = Math.Clamp(window.X, area.X, area.Right - width);
        var y = Math.Clamp(window.Y, area.Y, area.Bottom - height);
        return new ScreenRect(x, y, width, height);
    }

    private static double DistanceSquared(double x, double y, ScreenRect area)
    {
        var nearestX = Math.Clamp(x, area.X, area.Right);
        var nearestY = Math.Clamp(y, area.Y, area.Bottom);
        var dx = x - nearestX;
        var dy = y - nearestY;
        return dx * dx + dy * dy;
    }
}
