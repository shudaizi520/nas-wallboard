using System.Text.RegularExpressions;

namespace NASWallboard.Desktop.Core;

public static class ReleaseVersion
{
    private static readonly Regex Pattern = new(
        @"\Av?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?\z",
        RegexOptions.CultureInvariant | RegexOptions.NonBacktracking);

    public static string? Normalize(string? value)
    {
        if (value is null || value.Length > 96 || !Pattern.IsMatch(value)) return null;
        var tag = value.Split('+', 2)[0];
        return tag.StartsWith('v') ? tag : "v" + tag;
    }
}
