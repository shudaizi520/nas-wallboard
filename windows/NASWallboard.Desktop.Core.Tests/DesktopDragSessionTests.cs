using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class DesktopDragSessionTests
{
    [TestMethod]
    public void MoveToOffsetsWindowByPointerDelta()
    {
        var drag = new DesktopDragSession();
        Assert.IsTrue(drag.TryStart(
            new PixelRect(820, 56, 375, 258),
            new PixelPoint(1000, 200),
            leftButtonDown: true));

        Assert.IsTrue(drag.TryMoveTo(new PixelPoint(1125, 235), out var location));
        Assert.AreEqual(new PixelPoint(945, 91), location);
    }

    [TestMethod]
    public void TryStartRejectsReleasedButtonAndPointerOutsideWindow()
    {
        var drag = new DesktopDragSession();

        Assert.IsFalse(drag.TryStart(
            new PixelRect(820, 56, 375, 258),
            new PixelPoint(1000, 200),
            leftButtonDown: false));
        Assert.IsFalse(drag.TryStart(
            new PixelRect(820, 56, 375, 258),
            new PixelPoint(700, 200),
            leftButtonDown: true));
        Assert.IsFalse(drag.Active);
    }

    [TestMethod]
    public void StopPreventsFurtherMovement()
    {
        var drag = new DesktopDragSession();
        drag.TryStart(new PixelRect(820, 56, 375, 258), new PixelPoint(1000, 200), leftButtonDown: true);

        Assert.IsTrue(drag.Stop());

        Assert.IsFalse(drag.TryMoveTo(new PixelPoint(1125, 235), out _));
        Assert.IsFalse(drag.Stop());
    }
}
