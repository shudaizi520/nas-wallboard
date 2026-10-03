using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class ReleaseVersionTests
{
    [TestMethod]
    [DataRow("v1.0.10", "v1.0.10")]
    [DataRow("1.0.10+abc123", "v1.0.10")]
    [DataRow("v1.0.10-beta.2+abc123", "v1.0.10-beta.2")]
    public void ReleaseIdentityUsesTagWithoutCommitMetadata(string input, string expected)
    {
        Assert.AreEqual(expected, ReleaseVersion.Normalize(input));
    }

    [TestMethod]
    [DataRow(null)]
    [DataRow("")]
    [DataRow("unknown")]
    [DataRow("dev+abc123")]
    [DataRow("1.0")]
    [DataRow("v01.0.10")]
    [DataRow("v1.0.10<script>")]
    [DataRow(" v1.0.10 ")]
    public void MissingOrUnsafeReleaseIdentityDoesNotPretendToBeInstalledVersion(string? input)
    {
        Assert.IsNull(ReleaseVersion.Normalize(input));
    }
}
