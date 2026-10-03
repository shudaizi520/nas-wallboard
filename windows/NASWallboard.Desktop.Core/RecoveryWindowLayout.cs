namespace NASWallboard.Desktop.Core;

public sealed class RecoveryWindowLayout
{
    private ScreenRect? dashboardBounds;
    private ScreenRect recoveryBounds;
    public bool Active => dashboardBounds.HasValue;

    public ScreenRect Begin(ScreenRect current, int width, int height, IReadOnlyList<ScreenRect> workAreas, DesktopDragSession? drag = null)
    {
        if (drag is { Active: true })
        {
            TrackDrag(current);
            drag.Stop();
        }
        dashboardBounds ??= current;
        recoveryBounds = WindowPlacement.Resize(current, width, height, workAreas);
        return recoveryBounds;
    }

    public ScreenRect Restore(ScreenRect current, int width, int height, IReadOnlyList<ScreenRect> workAreas, DesktopDragSession? drag = null)
    {
        if (Active && drag is { Active: true })
        {
            TrackDrag(current);
            drag.Stop();
        }
        var anchor = dashboardBounds ?? current;
        dashboardBounds = null;
        return WindowPlacement.Resize(anchor, width, height, workAreas);
    }

    public void TrackDrag(ScreenRect current)
    {
        if (dashboardBounds is not { } original) return;
        dashboardBounds = original with
        {
            X = original.X + current.X - recoveryBounds.X,
            Y = original.Y + current.Y - recoveryBounds.Y,
        };
        recoveryBounds = current;
    }
}
