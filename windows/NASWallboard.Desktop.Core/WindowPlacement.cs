namespace NASWallboard.Desktop.Core;

public readonly record struct ScreenRect(int X, int Y, int Width, int Height)
{
    public int Right => X + Width;
    public int Bottom => Y + Height;
}

public static class WindowPlacement
{
    public const int DefaultMargin = 24;
    private const int RightAnchorThreshold = 96;

    public static bool ShouldPersist(bool initialPlacementComplete, PixelPoint current, PixelPoint saved) =>
        initialPlacementComplete && current != saved;

    public static ScreenRect Initial(PixelPoint saved, int width, int height, IReadOnlyList<ScreenRect> workAreas)
    {
        if (saved.X != -1 || saved.Y != -1)
            return Clamp(new ScreenRect(saved.X, saved.Y, width, height), workAreas);
        return workAreas.Count == 0
            ? new ScreenRect(0, 0, width, height)
            : TopRight(workAreas[0], width, height);
    }

    public static ScreenRect TopRight(ScreenRect workArea, int width, int height)
    {
        var fittedWidth = Math.Min(Math.Max(width, 1), workArea.Width);
        var fittedHeight = Math.Min(Math.Max(height, 1), workArea.Height);
        var marginX = Math.Min(DefaultMargin, Math.Max(0, workArea.Width - fittedWidth));
        var marginY = Math.Min(DefaultMargin, Math.Max(0, workArea.Height - fittedHeight));
        return new ScreenRect(
            workArea.Right - fittedWidth - marginX,
            workArea.Y + marginY,
            fittedWidth,
            fittedHeight);
    }

    public static ScreenRect Resize(ScreenRect window, int width, int height, IReadOnlyList<ScreenRect> workAreas)
    {
        if (workAreas.Count == 0) return new ScreenRect(window.X, window.Y, width, height);
        var area = NearestWorkArea(window, workAreas);
        var rightGap = area.Right - window.Right;
        var x = rightGap is >= 0 and <= RightAnchorThreshold
            ? area.Right - width - rightGap
            : window.X;
        return Clamp(new ScreenRect(x, window.Y, width, height), workAreas);
    }

    public static ScreenRect Clamp(ScreenRect window, IReadOnlyList<ScreenRect> workAreas)
    {
        if (workAreas.Count == 0) return window;
        var area = NearestWorkArea(window, workAreas);
        var width = Math.Min(Math.Max(window.Width, 1), area.Width);
        var height = Math.Min(Math.Max(window.Height, 1), area.Height);
        var x = Math.Clamp(window.X, area.X, area.Right - width);
        var y = Math.Clamp(window.Y, area.Y, area.Bottom - height);
        return new ScreenRect(x, y, width, height);
    }

    private static ScreenRect NearestWorkArea(ScreenRect window, IReadOnlyList<ScreenRect> workAreas)
    {
        var centerX = window.X + window.Width / 2d;
        var centerY = window.Y + window.Height / 2d;
        return workAreas.MinBy(candidate => DistanceSquared(centerX, centerY, candidate));
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
