using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class ServerAddressTests
{
    [TestMethod]
    [DataRow("http://10.0.0.99:18082", "http://10.0.0.99:18082/")]
    [DataRow(" https://nas.local ", "https://nas.local/")]
    public void TryNormalizeAcceptsOnlyAnHttpOrigin(string input, string expected)
    {
        Assert.IsTrue(ServerAddress.TryNormalize(input, out var actual));
        Assert.AreEqual(expected, actual.AbsoluteUri);
    }

    [TestMethod]
    [DataRow("")]
    [DataRow("ftp://nas.local")]
    [DataRow("javascript:alert(1)")]
    [DataRow("http://user:pass@nas.local")]
    [DataRow("http://nas.local/path")]
    [DataRow("http://nas.local/?token=secret")]
    [DataRow("http://nas.local/#part")]
    public void TryNormalizeRejectsAnythingOtherThanAnOrigin(string input)
    {
        Assert.IsFalse(ServerAddress.TryNormalize(input, out _));
    }
}
