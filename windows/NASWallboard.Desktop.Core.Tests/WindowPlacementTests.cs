using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class WindowPlacementTests
{
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
