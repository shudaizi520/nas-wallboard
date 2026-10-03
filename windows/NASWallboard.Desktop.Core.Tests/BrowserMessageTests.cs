using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class BrowserMessageTests
{
    [TestMethod]
    [DataRow("{\"type\":\"resize\",\"width\":\"300\",\"height\":100}")]
    [DataRow("{\"type\":\"resize\",\"width\":300,\"height\":null}")]
    public void InvalidNumericKindsAreRejectedWithoutThrowing(string input)
    {
        Assert.IsFalse(BrowserMessage.TryParse(input, out _));
    }

    [TestMethod]
    [DataRow("retry")]
    [DataRow("render-failed")]
    public void RecoveryMessagesAreAccepted(string type)
    {
        Assert.IsTrue(BrowserMessage.TryParse($"{{\"type\":\"{type}\"}}", out var command));
        if (type == "retry") Assert.IsInstanceOfType<RetryCommand>(command);
        else Assert.IsInstanceOfType<RenderFailedCommand>(command);
    }

    [TestMethod]
    public void VersionMessagesRequireAReleaseVersion()
    {
        Assert.IsTrue(BrowserMessage.TryParse("{\"type\":\"version\",\"version\":\"1.0.10\"}", out var command));
        Assert.AreEqual(new VersionCommand("v1.0.10"), command);
        Assert.IsFalse(BrowserMessage.TryParse("{\"type\":\"version\",\"version\":\"<script>\"}", out _));
        Assert.IsFalse(BrowserMessage.TryParse("{\"type\":\"version\",\"version\":123}", out _));
    }

    [TestMethod]
    [DataRow("http://nas.local/?desktop=1&desktop_attempt=2", true)]
    [DataRow("http://nas.local/?desktop=1&desktop_attempt=1", false)]
    [DataRow("http://evil.local/?desktop=1&desktop_attempt=2", false)]
    [DataRow("http://nas.local/manage?desktop=1&desktop_attempt=2", false)]
    [DataRow("http://nas.local:9000/?desktop=1&desktop_attempt=2", false)]
    [DataRow("about:blank", false)]
    [DataRow("not-a-uri", false)]
    public void OnlyTheCurrentDashboardDocumentCanSendCommands(string source, bool expected)
    {
        var current = new Uri("http://nas.local/?desktop=1&desktop_attempt=2");
        Assert.AreEqual(expected, BrowserMessage.IsCurrentDashboardSource(source, current));
    }
    [TestMethod]
    public void ProvisionalBrowserSizesCannotBeMistakenForConfiguredLayout()
    {
        Assert.IsTrue(BrowserMessage.TryParse("{\"type\":\"resize\",\"width\":560,\"height\":80,\"ready\":false}", out var provisional));
        Assert.AreEqual(new ResizeCommand(560,80,false),provisional);
        Assert.IsTrue(BrowserMessage.TryParse("{\"type\":\"resize\",\"width\":360,\"height\":340,\"ready\":true}",out var configured));
        Assert.AreEqual(new ResizeCommand(360,340,true),configured);
    }

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
    [DataRow("{\"type\":\"resize\",\"width\":300,\"height\":100,\"ready\":\"yes\"}")]
    public void TryParseRejectsMalformedUnknownOrOutOfRangeMessages(string input)
    {
        Assert.IsFalse(BrowserMessage.TryParse(input, out _));
    }
}
