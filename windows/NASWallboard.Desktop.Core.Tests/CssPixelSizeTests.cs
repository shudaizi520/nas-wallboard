using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class CssPixelSizeTests
{
    [TestMethod]
    [DataRow(1.0, 300, 206)]
    [DataRow(1.25, 375, 258)]
    [DataRow(1.5, 450, 309)]
    public void ToRawPixelsExpandsForWebViewRasterizationScale(double scale, int expectedWidth, int expectedHeight)
    {
        Assert.AreEqual(
            new PixelSize(expectedWidth, expectedHeight),
            CssPixelSize.ToRawPixels(300, 206, scale));
    }

    [TestMethod]
    public void ToRawPixelsRejectsInvalidScale()
    {
        Assert.ThrowsExactly<ArgumentOutOfRangeException>(() => CssPixelSize.ToRawPixels(300, 206, 0));
        Assert.ThrowsExactly<ArgumentOutOfRangeException>(() => CssPixelSize.ToRawPixels(300, 206, double.NaN));
    }
}
