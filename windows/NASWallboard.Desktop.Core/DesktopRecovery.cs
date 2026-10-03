namespace NASWallboard.Desktop.Core;

public enum RecoveryAction { None, ShowFallback, Navigate, RecreateBrowser }

public sealed class DesktopRecovery : IDisposable
{
    private enum State { Idle, Navigating, Rendering, Ready, Retry, Disposed }
    private State state;
    private DateTimeOffset deadline;
    private bool renderReady;
    private bool recreate;
    private int retries;
    public long Attempt { get; private set; }
    public bool IsReady => state == State.Ready;
    public bool HasPendingWork => state is State.Navigating or State.Rendering or State.Retry;
    public bool NeedsBrowserRecreation => recreate;
    public TimeSpan RetryDelay => TimeSpan.FromMinutes(retries >= 5 ? 10 : 1);
    public bool IsCurrentAttempt(long attempt) => attempt == Attempt && state is State.Navigating or State.Rendering or State.Ready;

    public long StartAttempt(DateTimeOffset now, bool manual)
    {
        if (state == State.Disposed) return 0;
        if (manual) retries = 0;
        renderReady = false;
        state = State.Navigating;
        deadline = now.AddSeconds(30);
        return ++Attempt;
    }

    public RecoveryAction NavigationCompleted(long attempt, bool success, DateTimeOffset now)
    {
        if (attempt != Attempt || state != State.Navigating) return RecoveryAction.None;
        if (!success) return Fail(attempt, now, false);
        state = State.Rendering;
        deadline = now.AddSeconds(45);
        if (renderReady) MarkReady();
        return RecoveryAction.None;
    }

    public void RenderReported(long attempt, bool ready)
    {
        if (!IsCurrentAttempt(attempt) || !ready) return;
        renderReady = true;
        if (state == State.Rendering) MarkReady();
    }

    private void MarkReady()
    {
        state = State.Ready;
        retries = 0;
        recreate = false;
    }

    public RecoveryAction Fail(long attempt, DateTimeOffset now, bool recreateBrowser)
    {
        if (attempt != Attempt || state is State.Disposed or State.Idle) return RecoveryAction.None;
        recreate |= recreateBrowser;
        if (state == State.Retry) return RecoveryAction.None;
        state = State.Retry;
        deadline = now.Add(RetryDelay);
        return RecoveryAction.ShowFallback;
    }

    public RecoveryAction Tick(DateTimeOffset now)
    {
        if (!HasPendingWork || now < deadline) return RecoveryAction.None;
        if (state is State.Navigating or State.Rendering) return Fail(Attempt, now, false);
        retries = Math.Min(retries + 1, 5);
        StartAttempt(now, manual: false);
        var action = recreate ? RecoveryAction.RecreateBrowser : RecoveryAction.Navigate;
        recreate = false;
        return action;
    }

    public void Dispose() => state = State.Disposed;
}
