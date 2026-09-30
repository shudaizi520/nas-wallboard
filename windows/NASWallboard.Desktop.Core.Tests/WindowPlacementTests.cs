using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class WindowPlacementTests
{
    [TestMethod]
    public void InitialActualSizeRestoresSavedPositionWithoutBootstrapClamping()
    {
        var areas = new[] { new ScreenRect(0, 0, 1920, 1040) };
        Assert.AreEqual(new ScreenRect(1536, 24, 360, 340),
            WindowPlacement.Initial(new PixelPoint(1536, 24), 360, 340, areas));
    }

    [TestMethod]
    public void ProvisionalBrowserSizesDoNotReplaceSavedPosition()
    {
        var areas = new[] { new ScreenRect(0, 0, 1920, 1040) };
        var saved = new PixelPoint(1536, 24);
        foreach (var json in new[] {
            "{\"type\":\"resize\",\"width\":560,\"height\":260,\"ready\":false}",
            "{\"type\":\"resize\",\"width\":360,\"height\":340,\"ready\":true}" })
        {
            Assert.IsTrue(BrowserMessage.TryParse(json, out var message));
            var size = (ResizeCommand)message!;
            var bounds = WindowPlacement.Initial(saved, size.Width, size.Height, areas);
            if (WindowPlacement.ShouldPersist(size.Ready, new PixelPoint(bounds.X, bounds.Y), saved))
                saved = new PixelPoint(bounds.X, bounds.Y);
            if (size.Ready) Assert.AreEqual(new ScreenRect(1536, 24, 360, 340), bounds);
        }
        Assert.AreEqual(new PixelPoint(1536, 24), saved);
    }

    [TestMethod]
    public void InitialPlacementUsesDefaultInsetAndHandlesRemovedMonitor()
    {
        var areas = new[] { new ScreenRect(0, 0, 1920, 1040) };
        Assert.AreEqual(new ScreenRect(1536, 24, 360, 340),
            WindowPlacement.Initial(new PixelPoint(-1, -1), 360, 340, areas));
        Assert.AreEqual(new ScreenRect(1560, 24, 360, 340),
            WindowPlacement.Initial(new PixelPoint(3000, 24), 360, 340, areas));
        Assert.IsFalse(WindowPlacement.ShouldPersist(false, new PixelPoint(1500, 24), new PixelPoint(1536, 24)));
    }

    [TestMethod]
    public void DefaultPlacementUsesTheTopRightCornerWithACompactInset()
    {
        var area = new ScreenRect(0, 0, 1920, 1040);

        Assert.AreEqual(
            new ScreenRect(1520, 24, 376, 340),
            WindowPlacement.TopRight(area, 376, 340));
    }

    [TestMethod]
    public void ResizeKeepsTheRightEdgeStableForATopRightWidget()
    {
        var areas = new[] { new ScreenRect(0, 0, 1920, 1040) };
        var current = new ScreenRect(1520, 24, 376, 340);
        var loading = WindowPlacement.Resize(current, 560, 340, areas);
        var loaded = WindowPlacement.Resize(loading, 376, 340, areas);

        Assert.AreEqual(new ScreenRect(1336, 24, 560, 340), loading);
        Assert.AreEqual(current, loaded);
    }

    [TestMethod]
    public void ResizeKeepsTheLeftEdgeStableAwayFromTheRightCorner()
    {
        var areas = new[] { new ScreenRect(0, 0, 1920, 1040) };
        var current = new ScreenRect(600, 120, 420, 260);

        Assert.AreEqual(
            new ScreenRect(600, 120, 376, 340),
            WindowPlacement.Resize(current, 376, 340, areas));
    }

    [TestMethod]
    public void PersistenceStartsOnlyAfterInitialPlacementCompletes()
    {
        var saved = new PixelPoint(1480, 72);
        var transientStartupPosition = new PixelPoint(0, 0);
        var movedPosition = new PixelPoint(1320, 96);

        Assert.IsFalse(WindowPlacement.ShouldPersist(false, transientStartupPosition, saved));
        Assert.IsFalse(WindowPlacement.ShouldPersist(true, saved, saved));
        Assert.IsTrue(WindowPlacement.ShouldPersist(true, movedPosition, saved));
    }

    [TestMethod]
    public void ClampMovesAnOffScreenWindowToTheNearestWorkArea()
    {
        var areas = new[] { new ScreenRect(0, 0, 1920, 1040), new ScreenRect(1920, 0, 1920, 1040) };
        Assert.AreEqual(new ScreenRect(3540, 0, 300, 200), WindowPlacement.Clamp(new ScreenRect(8000, -400, 300, 200), areas));
    }

    [TestMethod]
    public void ClampFitsAnOversizedWindowInsideTheWorkArea()
    {
        var areas = new[] { new ScreenRect(100, 50, 800, 600) };
        Assert.AreEqual(new ScreenRect(100, 50, 800, 600), WindowPlacement.Clamp(new ScreenRect(-20, -30, 1200, 900), areas));
    }
}
