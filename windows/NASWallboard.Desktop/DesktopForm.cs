using System.Diagnostics;
using System.Text.Json;
using Microsoft.Web.WebView2.Core;
using Microsoft.Web.WebView2.WinForms;
using Microsoft.Win32;
using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop;

internal sealed class DesktopForm : Form
{
    private readonly SettingsStore settingsStore;
    private WebView2 browser = new() { Dock = DockStyle.Fill };
    private readonly DesktopRecovery recovery = new();
    private readonly RecoveryWindowLayout recoveryLayout = new();
    private readonly System.Windows.Forms.Timer retryTimer = new() { Interval = 1000 };
    private readonly System.Windows.Forms.Timer dragTimer = new() { Interval = 30 };
    private readonly DesktopDragSession dragSession = new();
    private TrayMenu? tray;
    private AppSettings settings;
    private Uri serverOrigin;
    private bool allowExit;
    private bool browserReady;
    private bool showingFallback;
    private bool initialPlacementComplete;
    private Size? lastCssSize;
    private Uri? currentDashboardUri;
    private ulong? navigationId;
    private bool closing;
    private Label? recoveryLabel;

    internal AppSettings Settings => settings;

    internal DesktopForm(SettingsStore settingsStore)
    {
        StartupLog.Write("desktop form constructor entered");
        this.settingsStore = settingsStore;
        settings = settingsStore.Load();
        if (!ServerAddress.TryNormalize(settings.ServerUrl, out serverOrigin!))
            serverOrigin = new Uri(AppSettings.Default.ServerUrl);

        AutoScaleMode = AutoScaleMode.Dpi;
        FormBorderStyle = FormBorderStyle.None;
        ShowInTaskbar = false;
        StartPosition = FormStartPosition.Manual;
        BackColor = Color.Magenta;
        TransparencyKey = Color.Magenta;
        ClientSize = new Size(420, 260);
        Controls.Add(browser);

        retryTimer.Tick += (_, _) => ApplyRecoveryAction(recovery.Tick(DateTimeOffset.UtcNow));
        dragTimer.Tick += (_, _) => ContinueDragging();
        Load += OnLoaded;
        Move += (_, _) =>
        {
            if (!dragSession.Active) PersistPosition();
        };
        DpiChanged += (_, _) => ReapplyCssSize();
        FormClosing += OnClosing;
        SystemEvents.DisplaySettingsChanged += DisplaySettingsChanged;
        StartupLog.Write("desktop form constructor completed");
    }

    protected override bool ShowWithoutActivation => true;

    protected override CreateParams CreateParams
    {
        get
        {
            var parameters = base.CreateParams;
            parameters.ExStyle |= (int)(NativeMethods.WS_EX_TOOLWINDOW | NativeMethods.WS_EX_NOACTIVATE | NativeMethods.WS_EX_TRANSPARENT);
            return parameters;
        }
    }

    private async void OnLoaded(object? sender, EventArgs e)
    {
        StartupLog.Write("form load entered");
        StartupLog.Write(NativeMethods.TryAttachToDesktop(Handle)
            ? "attached to desktop host"
            : "desktop host attachment failed; continuing as a normal window");
        PlaceInitialWindow();
        tray = new TrayMenu(this);
        StartupLog.Write("tray icon created");
        ApplyLockedStyle();
        UpdateDragMonitoring();
        var target = browser;
        var attempt = recovery.StartAttempt(DateTimeOffset.UtcNow, manual: true);
        UpdateRecoveryTimer();
        try
        {
            await InitializeBrowserAsync(target);
            if (!closing && ReferenceEquals(target, browser) && recovery.IsCurrentAttempt(attempt)) NavigateCurrentAttempt();
        }
        catch (WebView2RuntimeNotFoundException)
        {
            if (closing || !ReferenceEquals(target, browser)) return;
            recovery.Dispose();
            retryTimer.Stop();
            StartupLog.Write("WebView2 Runtime was not found");
            ShowRuntimeMissing();
        }
        catch (Exception exception)
        {
            if (closing || !ReferenceEquals(target, browser)) return;
            StartupLog.Report(exception);
            ApplyRecoveryAction(recovery.Fail(attempt, DateTimeOffset.UtcNow, true));
        }
    }

    private async Task InitializeBrowserAsync(WebView2 target)
    {
        var dataDirectory = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
            "NASWallboard", "WebView2");
        var environment = await CoreWebView2Environment.CreateAsync(null, dataDirectory);
        if (closing || !ReferenceEquals(target, browser)) return;
        await target.EnsureCoreWebView2Async(environment);
        if (closing || !ReferenceEquals(target, browser)) return;
        target.DefaultBackgroundColor = Color.Transparent;
        target.CoreWebView2.Settings.AreDefaultContextMenusEnabled = false;
        target.CoreWebView2.Settings.AreDevToolsEnabled = false;
        target.CoreWebView2.Settings.IsStatusBarEnabled = false;
        target.CoreWebView2.Settings.IsZoomControlEnabled = false;
        target.CoreWebView2.NavigationStarting += NavigationStarting;
        target.CoreWebView2.NavigationCompleted += NavigationCompleted;
        target.CoreWebView2.WebMessageReceived += WebMessageReceived;
        target.CoreWebView2.ProcessFailed += ProcessFailed;
        browserReady = true;
        StartupLog.Write("WebView2 controller ready");
    }

    private void NavigationStarting(object? sender, CoreWebView2NavigationStartingEventArgs e)
    {
        if (closing || !ReferenceEquals(sender, browser.CoreWebView2)) { e.Cancel = true; return; }
        if (!Uri.TryCreate(e.Uri, UriKind.Absolute, out var requested))
        {
            e.Cancel = true;
            return;
        }

        if (showingFallback && requested.Scheme.Equals("about", StringComparison.OrdinalIgnoreCase)) return;
        if (!SameOrigin(requested, serverOrigin)) { e.Cancel = true; return; }
        // Only the requested attempt document may satisfy navigation or ready.
        if (!showingFallback && BrowserMessage.IsCurrentDashboardSource(e.Uri, currentDashboardUri))
            navigationId = e.NavigationId;
        else
        {
            e.Cancel = true;
            if (!DesktopNavigation.IsExternalManagementLink(e.Uri, serverOrigin, e.IsUserInitiated)) return;
            var external = new UriBuilder(requested)
            {
                Query = $"client_version={Uri.EscapeDataString(DesktopClientVersion.Current)}",
            }.Uri.AbsoluteUri;
            try { Process.Start(new ProcessStartInfo(external) { UseShellExecute = true }); }
            catch (Exception exception) { StartupLog.Report(exception); }
        }
    }

    private void NavigationCompleted(object? sender, CoreWebView2NavigationCompletedEventArgs e)
    {
        if (closing || showingFallback || !ReferenceEquals(sender, browser.CoreWebView2) || e.NavigationId != navigationId) return;
        ApplyRecoveryAction(recovery.NavigationCompleted(recovery.Attempt, e.IsSuccess, DateTimeOffset.UtcNow));
        if (e.IsSuccess) UpdateMovableMode();
    }

    private void WebMessageReceived(object? sender, CoreWebView2WebMessageReceivedEventArgs e)
    {
        if (closing || !ReferenceEquals(sender, browser.CoreWebView2)) return;
        var fromFallback = showingFallback && e.Source.Equals("about:blank", StringComparison.OrdinalIgnoreCase);
        if (!fromFallback && (showingFallback || !BrowserMessage.IsCurrentDashboardSource(e.Source, currentDashboardUri) ||
            !recovery.IsCurrentAttempt(recovery.Attempt))) return;
        if (!BrowserMessage.TryParse(e.WebMessageAsJson, out var command)) return;
        if (fromFallback)
        {
            if (command is RetryCommand) RefreshDashboard();
            return;
        }
        switch (command)
        {
            case ResizeCommand resize:
                recovery.RenderReported(recovery.Attempt, resize.Ready);
                ResizeAndClamp(resize.Width, resize.Height, actualDashboardSize: resize.Ready && !showingFallback);
                break;
            case RenderFailedCommand:
                ApplyRecoveryAction(recovery.Fail(recovery.Attempt, DateTimeOffset.UtcNow, false));
                break;
            case VersionCommand version:
                tray?.UpdateServerVersion(version.Version);
                break;
        }
        UpdateRecoveryTimer();
    }

    internal void ToggleLocked()
    {
        FinishDragging();
        SetLocked(MoveModeTransition.NextLocked(settings.Locked, MoveModeAction.Toggle));
    }

    private void SetLocked(bool locked)
    {
        if (settings.Locked == locked) return;
        settings = settings with { Locked = locked };
        SaveSettings();
        ApplyLockedStyle();
        tray?.UpdateLocked(settings.Locked);
        UpdateMovableMode();
        UpdateDragMonitoring();
    }

    internal void SetAutoStart(bool enabled)
    {
        settings = settings with { AutoStart = enabled };
        SaveSettings();
    }

    internal void OpenManagement()
    {
        var address = new UriBuilder(new Uri(serverOrigin, "manage"))
        {
            Query = $"client_version={Uri.EscapeDataString(DesktopClientVersion.Current)}",
        }.Uri.AbsoluteUri;
        Process.Start(new ProcessStartInfo(address) { UseShellExecute = true });
    }

    internal void RefreshDashboard()
    {
        NavigateDashboard();
    }

    internal void ChangeServerAddress()
    {
        using var dialog = new ServerAddressDialog(serverOrigin.AbsoluteUri);
        if (dialog.ShowDialog() != DialogResult.OK) return;
        serverOrigin = new Uri(dialog.ServerUrl);
        tray?.ResetServerVersion();
        settings = settings with { ServerUrl = serverOrigin.AbsoluteUri };
        SaveSettings();
        NavigateDashboard();
    }

    internal void ExitApplication()
    {
        allowExit = true;
        Close();
    }

    private void NavigateDashboard()
    {
        if (closing) return;
        var recreate = !browserReady || recovery.NeedsBrowserRecreation;
        if (recovery.StartAttempt(DateTimeOffset.UtcNow, manual: true) == 0) return;
        ApplyRecoveryAction(recreate ? RecoveryAction.RecreateBrowser : RecoveryAction.Navigate);
    }

    private void NavigateCurrentAttempt()
    {
        if (closing || !browserReady) return;
        showingFallback = false;
        navigationId = null;
        currentDashboardUri = new UriBuilder(serverOrigin)
        {
            Query = $"desktop=1&desktop_attempt={recovery.Attempt}&client_version={Uri.EscapeDataString(DesktopClientVersion.Current)}",
        }.Uri;
        if (recoveryLabel is not null) recoveryLabel.Visible = false;
        browser.Visible = true;
        try { browser.CoreWebView2.Navigate(currentDashboardUri.AbsoluteUri); }
        catch (Exception exception)
        {
            StartupLog.Report(exception);
            browserReady = false;
            ApplyRecoveryAction(recovery.Fail(recovery.Attempt, DateTimeOffset.UtcNow, true));
        }
    }

    private void ShowFallback()
    {
        if (closing || showingFallback) return;
        showingFallback = true;
        navigationId = null;
        if (!browserReady) recovery.Fail(recovery.Attempt, DateTimeOffset.UtcNow, true);
        if (browserReady)
        {
            try { browser.NavigateToString(FallbackPage.Create(serverOrigin.AbsoluteUri, (int)recovery.RetryDelay.TotalMinutes)); }
            catch (Exception exception)
            {
                StartupLog.Report(exception);
                browserReady = false;
                recovery.Fail(recovery.Attempt, DateTimeOffset.UtcNow, true);
            }
        }
        if (!browserReady) ShowNativeRecovery();
        ResizeAndClamp(388, 186, actualDashboardSize: false, temporaryRecovery: true);
    }

    private void ShowNativeRecovery()
    {
        browser.Visible = false;
        recoveryLabel ??= new Label
        {
            Dock = DockStyle.Fill, Padding = new Padding(22),
            BackColor = Color.FromArgb(12, 23, 40), ForeColor = Color.FromArgb(220, 231, 245),
            Font = new Font("Microsoft YaHei UI", 10F),
        };
        if (!Controls.Contains(recoveryLabel)) Controls.Add(recoveryLabel);
        recoveryLabel.Text = $"桌面组件暂不可用\r\n{(int)recovery.RetryDelay.TotalMinutes} 分钟后重试，也可从托盘立即刷新。";
        recoveryLabel.Visible = true;
        recoveryLabel.BringToFront();
    }

    private void ProcessFailed(object? sender, CoreWebView2ProcessFailedEventArgs e)
    {
        if (closing || !ReferenceEquals(sender, browser.CoreWebView2)) return;
        StartupLog.Write($"WebView2 process failed: {e.ProcessFailedKind}");
        var recreate = e.ProcessFailedKind == CoreWebView2ProcessFailedKind.BrowserProcessExited;
        if (!recreate && e.ProcessFailedKind is not CoreWebView2ProcessFailedKind.RenderProcessExited
            and not CoreWebView2ProcessFailedKind.RenderProcessUnresponsive) return;
        if (recreate) browserReady = false;
        ApplyRecoveryAction(recovery.Fail(recovery.Attempt, DateTimeOffset.UtcNow, recreate));
        if (showingFallback && (recreate || e.ProcessFailedKind == CoreWebView2ProcessFailedKind.RenderProcessUnresponsive))
            ShowNativeRecovery();
    }

    private void ApplyRecoveryAction(RecoveryAction action)
    {
        if (closing) return;
        switch (action)
        {
            case RecoveryAction.ShowFallback: ShowFallback(); break;
            case RecoveryAction.Navigate:
                if (browserReady) NavigateCurrentAttempt();
                else RecreateBrowserAsync();
                break;
            case RecoveryAction.RecreateBrowser: RecreateBrowserAsync(); break;
        }
        UpdateRecoveryTimer();
    }

    private void UpdateRecoveryTimer()
    {
        if (closing) return;
        if (recovery.HasPendingWork) retryTimer.Start();
        else retryTimer.Stop();
    }

    private async void RecreateBrowserAsync()
    {
        var attempt = recovery.Attempt;
        WebView2? target = null;
        try
        {
            var old = browser;
            DetachBrowserEvents(old);
            Controls.Remove(old);
            old.Dispose();
            browserReady = false;
            browser = new WebView2 { Dock = DockStyle.Fill };
            target = browser;
            Controls.Add(target);
            await InitializeBrowserAsync(target);
            if (!closing && ReferenceEquals(target, browser) && recovery.IsCurrentAttempt(attempt)) NavigateCurrentAttempt();
        }
        catch (Exception exception)
        {
            if (closing || (target is not null && !ReferenceEquals(target, browser))) return;
            StartupLog.Report(exception);
            ApplyRecoveryAction(recovery.Fail(attempt, DateTimeOffset.UtcNow, true));
        }
    }

    private void DetachBrowserEvents(WebView2 target)
    {
        if (target.CoreWebView2 is not { } core) return;
        core.NavigationStarting -= NavigationStarting;
        core.NavigationCompleted -= NavigationCompleted;
        core.WebMessageReceived -= WebMessageReceived;
        core.ProcessFailed -= ProcessFailed;
    }

    private void ShowRuntimeMissing()
    {
        browser.Visible = false;
        BackColor = Color.FromArgb(12, 23, 40);
        TransparencyKey = Color.Empty;
        ClientSize = new Size(390, 130);
        var message = new Label
        {
            Dock = DockStyle.Fill,
            Padding = new Padding(22),
            ForeColor = Color.FromArgb(220, 231, 245),
            Font = new Font("Microsoft YaHei UI", 10F),
            Text = "缺少 Microsoft Edge WebView2 Runtime\r\n安装后重新打开 NAS 桌面组件。",
        };
        Controls.Add(message);
        message.BringToFront();
        ClampToVisibleArea();
    }

    private void ShowStartupFailure(Exception exception)
    {
        browser.Visible = false;
        BackColor = Color.FromArgb(12, 23, 40);
        TransparencyKey = Color.Empty;
        ClientSize = new Size(430, 150);
        var message = new Label
        {
            Dock = DockStyle.Fill,
            Padding = new Padding(22),
            ForeColor = Color.FromArgb(220, 231, 245),
            Font = new Font("Microsoft YaHei UI", 10F),
            Text = $"启动失败：{exception.GetType().Name}\r\n错误已经写入启动日志。",
        };
        Controls.Add(message);
        message.BringToFront();
        ClampToVisibleArea();
    }

    private void ApplyLockedStyle()
    {
        if (!IsHandleCreated) return;
        var style = NativeMethods.GetWindowLongPtr(Handle, NativeMethods.GWL_EXSTYLE);
        style |= NativeMethods.WS_EX_TOOLWINDOW | NativeMethods.WS_EX_NOACTIVATE;
        if (settings.Locked) style |= NativeMethods.WS_EX_TRANSPARENT;
        else style &= ~NativeMethods.WS_EX_TRANSPARENT;
        NativeMethods.SetWindowLongPtr(Handle, NativeMethods.GWL_EXSTYLE, style);
        NativeMethods.SetWindowPos(Handle, 0, 0, 0, 0, 0,
            NativeMethods.SWP_NOMOVE | NativeMethods.SWP_NOSIZE | NativeMethods.SWP_NOZORDER |
            NativeMethods.SWP_NOACTIVATE | NativeMethods.SWP_FRAMECHANGED);
    }

    private void ContinueDragging()
    {
        if (settings.Locked)
        {
            dragTimer.Stop();
            return;
        }

        var leftButtonDown = NativeMethods.IsLeftMouseButtonDown();
        if (!leftButtonDown)
        {
            FinishDragging();
            SetLocked(MoveModeTransition.NextLocked(settings.Locked, MoveModeAction.DragCompleted));
            return;
        }
        if (!NativeMethods.TryGetCursorPosition(out var pointer)) return;
        if (!dragSession.Active)
        {
            if (!NativeMethods.TryGetWindowBounds(Handle, out var bounds) ||
                !dragSession.TryStart(bounds, pointer, leftButtonDown)) return;
        }
        if (dragSession.TryMoveTo(pointer, out var location))
            NativeMethods.TryMoveWindowToScreenPosition(Handle, location);
    }

    private void FinishDragging()
    {
        if (!dragSession.Active) return;
        ClampToVisibleArea();
        recoveryLayout.TrackDrag(CurrentBounds());
        if (!dragSession.Stop()) return;
        PersistPosition();
    }

    private void UpdateDragMonitoring()
    {
        if (settings.Locked) dragTimer.Stop();
        else dragTimer.Start();
    }

    private void UpdateMovableMode()
    {
        if (!browserReady || showingFallback) return;
        try { browser.CoreWebView2.PostWebMessageAsJson(JsonSerializer.Serialize(new
        {
            type = "movable",
            enabled = !settings.Locked,
        })); }
        catch (Exception exception)
        {
            StartupLog.Report(exception);
            ApplyRecoveryAction(recovery.Fail(recovery.Attempt, DateTimeOffset.UtcNow, true));
        }
    }

    private void PlaceInitialWindow()
    {
        if (settings.X != -1 || settings.Y != -1)
        {
            if (!NativeMethods.TryMoveWindowToScreenPosition(Handle, new PixelPoint(settings.X, settings.Y)))
                Location = new Point(settings.X, settings.Y);
        }
        else
        {
            var area = Screen.PrimaryScreen?.WorkingArea ?? new Rectangle(0, 0, 1920, 1080);
            var placement = WindowPlacement.TopRight(
                new ScreenRect(area.X, area.Y, area.Width, area.Height),
                Width,
                Height);
            var location = new PixelPoint(placement.X, placement.Y);
            if (!NativeMethods.TryMoveWindowToScreenPosition(Handle, location))
                Location = new Point(location.X, location.Y);
        }
        ClampToVisibleArea();
    }

    private void ResizeAndClamp(int width, int height, bool actualDashboardSize = true, bool temporaryRecovery = false)
    {
        // A refreshed page may report loading geometry; keep its previous actual
        // bounds until configuration renders, never persist that temporary size.
        if (!actualDashboardSize && !temporaryRecovery && initialPlacementComplete) return;
        if (actualDashboardSize) lastCssSize = new Size(width, height);
        var scale = DeviceDpi / 96d;
        var pixels = CssPixelSize.ToRawPixels(width, height, scale);
        var current = CurrentBounds();
        var resized = temporaryRecovery
            ? recoveryLayout.Begin(current, pixels.Width, pixels.Height, WorkAreas(), dragSession)
            : recoveryLayout.Restore(current, pixels.Width, pixels.Height, WorkAreas(), actualDashboardSize ? dragSession : null);
        if (!temporaryRecovery && !initialPlacementComplete)
            resized = WindowPlacement.Initial(new PixelPoint(settings.X, settings.Y), pixels.Width, pixels.Height, InitialWorkAreas());
        if (!NativeMethods.TrySetWindowBoundsFromScreen(Handle, resized))
            Bounds = new Rectangle(resized.X, resized.Y, resized.Width, resized.Height);
        if (actualDashboardSize) initialPlacementComplete = true;
        PersistPosition();
    }

    private void ReapplyCssSize()
    {
        if (recoveryLayout.Active)
        {
            ResizeAndClamp(388, 186, actualDashboardSize: false, temporaryRecovery: true);
            return;
        }
        if (lastCssSize is { } size) ResizeAndClamp(size.Width, size.Height);
    }

    private void DisplaySettingsChanged(object? sender, EventArgs e) => ClampToVisibleArea();

    private void ClampToVisibleArea()
    {
        var clamped = WindowPlacement.Clamp(CurrentBounds(), WorkAreas());
        if (!NativeMethods.TrySetWindowBoundsFromScreen(Handle, clamped))
            Bounds = new Rectangle(clamped.X, clamped.Y, clamped.Width, clamped.Height);
    }

    private ScreenRect CurrentBounds() =>
        NativeMethods.TryGetWindowBounds(Handle, out var bounds)
            ? new ScreenRect(bounds.X, bounds.Y, bounds.Width, bounds.Height)
            : new ScreenRect(Left, Top, Width, Height);

    private static ScreenRect[] WorkAreas() => Screen.AllScreens
        .Select(screen => new ScreenRect(
            screen.WorkingArea.X,
            screen.WorkingArea.Y,
            screen.WorkingArea.Width,
            screen.WorkingArea.Height))
        .ToArray();

    private static ScreenRect[] InitialWorkAreas() => Screen.AllScreens
        .OrderByDescending(screen => screen.Primary)
        .Select(screen => new ScreenRect(screen.WorkingArea.X, screen.WorkingArea.Y, screen.WorkingArea.Width, screen.WorkingArea.Height))
        .ToArray();

    private void PersistPosition()
    {
        if (WindowState != FormWindowState.Normal || recoveryLayout.Active) return;
        var location = NativeMethods.TryGetWindowBounds(Handle, out var bounds)
            ? new PixelPoint(bounds.X, bounds.Y)
            : new PixelPoint(Left, Top);
        if (!WindowPlacement.ShouldPersist(initialPlacementComplete, location, new PixelPoint(settings.X, settings.Y))) return;
        settings = settings with { X = location.X, Y = location.Y };
        SaveSettings();
    }

    private void SaveSettings()
    {
        try
        {
            settingsStore.Save(settings);
        }
        catch (IOException)
        {
            // Keep running; the next explicit setting change will retry the save.
        }
        catch (UnauthorizedAccessException)
        {
            // Keep the in-memory setting for this session.
        }
    }

    private void OnClosing(object? sender, FormClosingEventArgs e)
    {
        if (!allowExit && e.CloseReason == CloseReason.UserClosing)
        {
            e.Cancel = true;
            return;
        }
        retryTimer.Stop();
        closing = true;
        recovery.Dispose();
        dragTimer.Stop();
        SystemEvents.DisplaySettingsChanged -= DisplaySettingsChanged;
        tray?.Dispose();
        DetachBrowserEvents(browser);
        browser.Dispose();
        dragTimer.Dispose();
        retryTimer.Dispose();
    }

    private static bool SameOrigin(Uri left, Uri right) =>
        left.Scheme.Equals(right.Scheme, StringComparison.OrdinalIgnoreCase) &&
        left.Host.Equals(right.Host, StringComparison.OrdinalIgnoreCase) &&
        left.Port == right.Port;
}
