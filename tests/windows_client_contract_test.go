package tests

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestWindowsClientProjectContract(t *testing.T) {
	project := string(projectFile(t, "windows/NASWallboard.Desktop/NASWallboard.Desktop.csproj"))
	for _, required := range []string{
		"<TargetFramework>net8.0-windows</TargetFramework>",
		"<UseWindowsForms>true</UseWindowsForms>",
		"<EnableWindowsTargeting>true</EnableWindowsTargeting>",
		"<RuntimeIdentifier>win-x64</RuntimeIdentifier>",
		"<SelfContained>true</SelfContained>",
		"<PublishSingleFile>true</PublishSingleFile>",
		`<PackageReference Include="Microsoft.Web.WebView2" Version="1.0.4191.47" />`,
	} {
		if !strings.Contains(project, required) {
			t.Errorf("Windows project missing %q", required)
		}
	}

	source := string(projectFile(t, "windows/NASWallboard.Desktop/DesktopForm.cs")) +
		string(projectFile(t, "windows/NASWallboard.Desktop/TrayMenu.cs")) +
		string(projectFile(t, "windows/NASWallboard.Desktop/NativeMethods.cs")) +
		string(projectFile(t, "windows/NASWallboard.Desktop/Program.cs")) +
		string(projectFile(t, "windows/NASWallboard.Desktop/StartupLog.cs"))

	for _, required := range []string{
		"DefaultBackgroundColor = Color.Transparent",
		"NavigationStarting +=",
		"NavigationCompleted +=",
		"WebMessageReceived +=",
		"WS_EX_TOOLWINDOW",
		"WS_EX_NOACTIVATE",
		"WS_EX_TRANSPARENT",
		"锁定",
		"移动位置",
		"打开内容管理",
		"刷新",
		"开机启动",
		"设置 NAS 地址",
		"退出",
		"settings.X != -1 || settings.Y != -1",
		"Application.ThreadException +=",
		"AppDomain.CurrentDomain.UnhandledException +=",
		"StartupLog.Write",
		"catch (Exception exception)",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Windows client source missing %q", required)
		}
	}

	compact := strings.ReplaceAll(source, " ", "")
	if strings.Contains(compact, "TopMost=true") {
		t.Error("desktop widget must never be always on top")
	}
}

func TestWindowsClientIconContainsSmallAndLargeImages(t *testing.T) {
	icon := projectFile(t, "windows/NASWallboard.Desktop/Assets/NASWallboard.ico")
	if len(icon) < 6 || binary.LittleEndian.Uint16(icon[0:2]) != 0 || binary.LittleEndian.Uint16(icon[2:4]) != 1 {
		t.Fatal("Windows icon has an invalid ICO header")
	}
	count := int(binary.LittleEndian.Uint16(icon[4:6]))
	if len(icon) < 6+count*16 {
		t.Fatalf("Windows icon directory is truncated: %d entries, %d bytes", count, len(icon))
	}
	has16, has256 := false, false
	for index := 0; index < count; index++ {
		width := int(icon[6+index*16])
		if width == 0 {
			width = 256
		}
		has16 = has16 || width == 16
		has256 = has256 || width == 256
	}
	if !has16 || !has256 {
		t.Fatalf("Windows icon sizes: has16=%t has256=%t", has16, has256)
	}
}

func TestWindowsClientCreateParamsDoesNotReadDerivedFieldsDuringBaseConstruction(t *testing.T) {
	source := string(projectFile(t, "windows/NASWallboard.Desktop/DesktopForm.cs"))
	start := strings.Index(source, "protected override CreateParams CreateParams")
	if start < 0 {
		t.Fatal("could not locate DesktopForm.CreateParams")
	}
	end := strings.Index(source[start:], "private async void OnLoaded")
	if end < 0 {
		t.Fatal("could not locate DesktopForm.CreateParams")
	}
	createParams := source[start : start+end]
	if strings.Contains(createParams, "settings") {
		t.Fatalf("CreateParams reads derived state before the Form base constructor completes:\n%s", createParams)
	}
}

func TestWindowsDesktopDownloadIsLinkedAndBuiltIntoProductionImage(t *testing.T) {
	manage := string(projectFile(t, "web/manage.html"))
	dockerfile := string(projectFile(t, "Dockerfile"))
	for _, contract := range []struct {
		name     string
		content  string
		required []string
	}{
		{"management page", manage, []string{"/download/nas-wallboard-desktop.zip", "安装桌面小组件"}},
		{"Dockerfile", dockerfile, []string{"COPY web ./web", "go build", "NASWallboard.Desktop.exe"}},
	} {
		for _, value := range contract.required {
			if !strings.Contains(contract.content, value) {
				t.Errorf("%s missing %q", contract.name, value)
			}
		}
	}
}
