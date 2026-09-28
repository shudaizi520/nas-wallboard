namespace NASWallboard.Desktop.Core;

public readonly record struct PixelPoint(int X, int Y);
public readonly record struct PixelRect(int X, int Y, int Width, int Height)
{
    public bool Contains(PixelPoint point) =>
        point.X >= X && point.X < X + Width && point.Y >= Y && point.Y < Y + Height;
}

public sealed class DesktopDragSession
{
    private PixelPoint windowOrigin;
    private PixelPoint pointerOrigin;

    public bool Active { get; private set; }

    public bool TryStart(PixelRect windowBounds, PixelPoint pointerLocation, bool leftButtonDown)
    {
        if (!leftButtonDown || !windowBounds.Contains(pointerLocation)) return false;

        windowOrigin = new PixelPoint(windowBounds.X, windowBounds.Y);
        pointerOrigin = pointerLocation;
        Active = true;
        return true;
    }

    public bool TryMoveTo(PixelPoint pointerLocation, out PixelPoint windowLocation)
    {
        if (!Active)
        {
            windowLocation = default;
            return false;
        }

        windowLocation = new PixelPoint(
            windowOrigin.X + pointerLocation.X - pointerOrigin.X,
            windowOrigin.Y + pointerLocation.Y - pointerOrigin.Y);
        return true;
    }

    public bool Stop()
    {
        if (!Active) return false;
        Active = false;
        return true;
    }
}
