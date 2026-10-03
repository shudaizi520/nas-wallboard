using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class DesktopRecoveryTests
{
    private static readonly DateTimeOffset Start = DateTimeOffset.UnixEpoch;

    [TestMethod]
    public void SuccessfulNavigationWithoutRenderStillTimesOut()
    {
        var recovery = new DesktopRecovery();
        var attempt = recovery.StartAttempt(Start, manual: true);
        recovery.NavigationCompleted(attempt, true, Start.AddSeconds(2));
        Assert.AreEqual(RecoveryAction.None, recovery.Tick(Start.AddSeconds(46)));
        Assert.AreEqual(RecoveryAction.ShowFallback, recovery.Tick(Start.AddSeconds(47)));
    }

    [TestMethod]
    public void ProvisionalMessagesCannotExtendRenderDeadline()
    {
        var recovery = new DesktopRecovery();
        var attempt = recovery.StartAttempt(Start, manual: true);
        recovery.NavigationCompleted(attempt, true, Start);
        recovery.RenderReported(attempt, false);
        recovery.RenderReported(attempt, false);
        Assert.AreEqual(RecoveryAction.ShowFallback, recovery.Tick(Start.AddSeconds(45)));
    }

    [TestMethod]
    public void SuccessfulRenderCancelsRecoveryAndIgnoresLaterProvisionalSize()
    {
        var recovery = new DesktopRecovery();
        var attempt = recovery.StartAttempt(Start, manual: true);
        recovery.RenderReported(attempt, true);
        recovery.NavigationCompleted(attempt, true, Start);
        recovery.RenderReported(attempt, false);
        Assert.IsTrue(recovery.IsReady);
        Assert.AreEqual(RecoveryAction.None, recovery.Tick(Start.AddHours(1)));
    }

    [TestMethod]
    public void PreviousAttemptCannotCompleteOrFailTheCurrentAttempt()
    {
        var recovery = new DesktopRecovery();
        var old = recovery.StartAttempt(Start, manual: true);
        var current = recovery.StartAttempt(Start.AddSeconds(1), manual: true);
        recovery.NavigationCompleted(old, true, Start);
        recovery.RenderReported(old, true);
        Assert.AreEqual(RecoveryAction.None, recovery.Fail(old, Start, recreateBrowser: true));
        Assert.IsFalse(recovery.IsReady);
        recovery.NavigationCompleted(current, true, Start.AddSeconds(1));
        Assert.AreEqual(RecoveryAction.ShowFallback, recovery.Tick(Start.AddSeconds(46)));
    }

    [TestMethod]
    public void NavigationFailureWaitsOneMinuteAndRepeatedFailureDoesNotPostponeRetry()
    {
        var recovery = new DesktopRecovery();
        var attempt = recovery.StartAttempt(Start, manual: true);
        Assert.AreEqual(RecoveryAction.ShowFallback, recovery.NavigationCompleted(attempt, false, Start));
        Assert.AreEqual(RecoveryAction.None, recovery.Fail(attempt, Start.AddSeconds(20), false));
        Assert.AreEqual(RecoveryAction.None, recovery.Tick(Start.AddSeconds(59)));
        Assert.AreEqual(RecoveryAction.Navigate, recovery.Tick(Start.AddSeconds(60)));
        Assert.AreEqual(RecoveryAction.None, recovery.Tick(Start.AddSeconds(60)));
    }

    [TestMethod]
    public void BrowserCrashEscalatesPendingRendererRecoveryToRecreation()
    {
        var recovery = new DesktopRecovery();
        var attempt = recovery.StartAttempt(Start, manual: true);
        recovery.NavigationCompleted(attempt, true, Start);
        recovery.RenderReported(attempt, true);
        Assert.AreEqual(RecoveryAction.ShowFallback, recovery.Fail(attempt, Start, false));
        recovery.Fail(attempt, Start.AddSeconds(10), true);
        Assert.AreEqual(RecoveryAction.RecreateBrowser, recovery.Tick(Start.AddSeconds(60)));
    }

    [TestMethod]
    public void RepeatedFailuresSlowAutomaticRetriesButRecoverWithoutUserIntervention()
    {
        var recovery = new DesktopRecovery();
        recovery.StartAttempt(Start, manual: true);
        var now = Start;
        for (var retry = 0; retry < 5; retry++)
        {
            recovery.Fail(recovery.Attempt, now, false);
            now = now.AddMinutes(1);
            Assert.AreEqual(RecoveryAction.Navigate, recovery.Tick(now));
        }
        recovery.Fail(recovery.Attempt, now, false);
        Assert.AreEqual(RecoveryAction.None, recovery.Tick(now.AddMinutes(1)));
        Assert.AreEqual(RecoveryAction.None, recovery.Tick(now.AddMinutes(9)));
        Assert.AreEqual(RecoveryAction.Navigate, recovery.Tick(now.AddMinutes(10)));
        now = now.AddMinutes(10);
        recovery.Fail(recovery.Attempt, now, false);
        Assert.AreEqual(RecoveryAction.None, recovery.Tick(now.AddMinutes(9)));
        Assert.AreEqual(RecoveryAction.Navigate, recovery.Tick(now.AddMinutes(10)));
        recovery.StartAttempt(now, manual: true);
        recovery.Fail(recovery.Attempt, now, false);
        Assert.AreEqual(RecoveryAction.Navigate, recovery.Tick(now.AddMinutes(1)));
    }

    [TestMethod]
    public void DisposalSuppressesTimersAndLateEvents()
    {
        var recovery = new DesktopRecovery();
        var attempt = recovery.StartAttempt(Start, manual: true);
        recovery.Dispose();
        recovery.RenderReported(attempt, true);
        Assert.AreEqual(RecoveryAction.None, recovery.NavigationCompleted(attempt, false, Start));
        Assert.AreEqual(RecoveryAction.None, recovery.Fail(attempt, Start, true));
        Assert.AreEqual(RecoveryAction.None, recovery.Tick(Start.AddDays(1)));
        Assert.AreEqual(0L, recovery.StartAttempt(Start, manual: true));
        Assert.IsFalse(recovery.IsReady);
    }

    [TestMethod]
    public void NavigationDeadlineAlsoCoversARequestThatNeverCompletes()
    {
        var recovery = new DesktopRecovery();
        recovery.StartAttempt(Start, manual: true);
        Assert.AreEqual(RecoveryAction.ShowFallback, recovery.Tick(Start.AddSeconds(30)));
    }
}
