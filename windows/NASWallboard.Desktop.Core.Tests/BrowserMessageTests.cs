using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class BrowserMessageTests
{
    [TestMethod]
    public void TryParseAcceptsKnownCommands()
    {
        Assert.IsTrue(BrowserMessage.TryParse("{\"type\":\"resize\",\"width\":300,\"height\":185}", out var resize));
        Assert.AreEqual(new ResizeCommand(300, 185), resize);
    }

    [TestMethod]
    [DataRow("not-json")]
    [DataRow("{\"type\":\"unknown\"}")]
    [DataRow("{\"type\":\"resize\",\"width\":-1,\"height\":100}")]
    [DataRow("{\"type\":\"resize\",\"width\":801,\"height\":100}")]
    [DataRow("{\"type\":\"resize\",\"width\":300,\"height\":1001}")]
    public void TryParseRejectsMalformedUnknownOrOutOfRangeMessages(string input)
    {
        Assert.IsFalse(BrowserMessage.TryParse(input, out _));
    }
}
