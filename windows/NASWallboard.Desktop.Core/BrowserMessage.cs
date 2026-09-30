using System.Text.Json;

namespace NASWallboard.Desktop.Core;

public abstract record BrowserCommand;
public sealed record ResizeCommand(int Width, int Height, bool Ready = true) : BrowserCommand;

public static class BrowserMessage
{
    public static bool TryParse(string? json, out BrowserCommand? command)
    {
        command = null;
        if (string.IsNullOrWhiteSpace(json)) return false;
        try
        {
            using var document = JsonDocument.Parse(json);
            var root = document.RootElement;
            if (root.ValueKind != JsonValueKind.Object || !root.TryGetProperty("type", out var type) || type.ValueKind != JsonValueKind.String) return false;
            switch (type.GetString())
            {
                case "resize":
                    if (!root.TryGetProperty("width", out var widthElement) || !widthElement.TryGetInt32(out var width)) return false;
                    if (!root.TryGetProperty("height", out var heightElement) || !heightElement.TryGetInt32(out var height)) return false;
                    if (width is < 200 or > 800 || height is < 80 or > 1000) return false;
                    var ready = true; // Older servers did not distinguish provisional sizes.
                    if (root.TryGetProperty("ready", out var readyElement))
                    {
                        if (readyElement.ValueKind is not JsonValueKind.True and not JsonValueKind.False) return false;
                        ready = readyElement.GetBoolean();
                    }
                    command = new ResizeCommand(width, height, ready);
                    return true;
                default:
                    return false;
            }
        }
        catch (JsonException)
        {
            return false;
        }
    }
}
