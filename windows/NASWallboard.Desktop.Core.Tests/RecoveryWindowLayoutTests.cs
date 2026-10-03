using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class RecoveryWindowLayoutTests
{
    private static readonly ScreenRect[] Areas = [new(0, 0, 1920, 1040)];

    [TestMethod]
    public void TemporaryRecoveryResizeEndsThePreviousActiveDragOrigin()
    {
        var layout = new RecoveryWindowLayout();
        var drag = new DesktopDragSession();
        Assert.IsTrue(drag.TryStart(new PixelRect(1596, 900, 300, 80), new PixelPoint(1606, 910), true));
        Assert.IsTrue(drag.TryMoveTo(new PixelPoint(1506, 810), out var location));
        var moved = new ScreenRect(location.X, location.Y, 300, 80);
        var fallback = layout.Begin(moved, 388, 186, Areas, drag);
        Assert.IsFalse(drag.Active);
        Assert.AreEqual(moved, layout.Restore(fallback, 300, 80, Areas));
    }

    [TestMethod]
    public void ReadyBeforeMouseUpRecordsDragAndEndsTheTemporaryDragOrigin()
    {
        var layout = new RecoveryWindowLayout();
        var fallback = layout.Begin(new ScreenRect(1596, 900, 300, 80), 388, 186, Areas);
        var drag = new DesktopDragSession();
        Assert.IsTrue(drag.TryStart(new PixelRect(fallback.X, fallback.Y, fallback.Width, fallback.Height),
            new PixelPoint(1518, 864), leftButtonDown: true));
        Assert.IsTrue(drag.TryMoveTo(new PixelPoint(1418, 764), out var location));
        var moved = fallback with { X = location.X, Y = location.Y };
        Assert.AreEqual(new ScreenRect(1496, 800, 300, 80), layout.Restore(moved, 300, 80, Areas, drag));
        Assert.IsFalse(drag.Active);
        Assert.IsFalse(drag.TryMoveTo(new PixelPoint(1419, 765), out _));
    }

    [TestMethod]
    public void ShortActualDashboardCanShowTheCompleteTemporaryRecoveryPage()
    {
        var layout = new RecoveryWindowLayout();
        var fallback = layout.Begin(new ScreenRect(1596, 900, 300, 80), 388, 186, Areas);
        Assert.AreEqual(new ScreenRect(1508, 854, 388, 186), fallback);
        Assert.IsTrue(layout.Active);
    }

    [TestMethod]
    public void RecoveryClampingDoesNotReplaceTheActualDashboardPlacement()
    {
        var layout = new RecoveryWindowLayout();
        var original = new ScreenRect(1596, 900, 300, 80);
        var fallback = layout.Begin(original, 388, 186, Areas);
        fallback = layout.Begin(fallback, 388, 186, Areas);
        Assert.AreEqual(original, layout.Restore(fallback, 300, 80, Areas));
        Assert.IsFalse(layout.Active);
    }

    [TestMethod]
    public void ExplicitRecoveryDragCarriesForwardToTheRestoredDashboard()
    {
        var layout = new RecoveryWindowLayout();
        var fallback = layout.Begin(new ScreenRect(600, 120, 300, 80), 388, 186, Areas);
        var moved = fallback with { X = 700, Y = 220 };
        layout.TrackDrag(moved);
        Assert.AreEqual(new ScreenRect(700, 220, 300, 80), layout.Restore(moved, 300, 80, Areas));
    }
}
