namespace NASWallboard.Desktop.Core;

public static class ServerAddress
{
    public static bool TryNormalize(string? value, out Uri uri)
    {
        uri = null!;
        if (!Uri.TryCreate(value?.Trim(), UriKind.Absolute, out var candidate)) return false;
        if (candidate.Scheme != Uri.UriSchemeHttp && candidate.Scheme != Uri.UriSchemeHttps) return false;
        if (string.IsNullOrWhiteSpace(candidate.Host) || candidate.UserInfo.Length != 0) return false;
        if (candidate.Query.Length != 0 || candidate.Fragment.Length != 0 || candidate.AbsolutePath != "/") return false;

        var normalized = new UriBuilder(candidate) { Path = "/", Query = "", Fragment = "" }.Uri;
        uri = normalized;
        return true;
    }
}
