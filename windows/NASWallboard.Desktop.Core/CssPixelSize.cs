namespace NASWallboard.Desktop.Core;

public readonly record struct PixelSize(int Width, int Height);

public static class CssPixelSize
{
    public static PixelSize ToRawPixels(int width, int height, double rasterizationScale)
    {
        if (!double.IsFinite(rasterizationScale) || rasterizationScale <= 0)
            throw new ArgumentOutOfRangeException(nameof(rasterizationScale));
        return new PixelSize(
            (int)Math.Ceiling(width * rasterizationScale),
            (int)Math.Ceiling(height * rasterizationScale));
    }
}
