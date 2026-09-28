package desktop

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"testing/fstest"
)

func TestDesktopPackageIsDeterministicAndContainsConfiguredClient(t *testing.T) {
	assets := fstest.MapFS{
		"downloads/NASWallboard.Desktop.exe": {Data: append([]byte("MZ"), bytes.Repeat([]byte{0x42}, 32)...)},
	}
	first, err := BuildPackage(assets, "http://10.0.0.99:18082/")
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPackage(assets, "http://10.0.0.99:18082/")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("desktop ZIP is not deterministic")
	}

	files := unzipDesktopPackage(t, first)
	if len(files) != 4 || !bytes.HasPrefix(files["NASWallboard.Desktop.exe"], []byte("MZ")) {
		t.Fatalf("desktop ZIP files = %#v", files)
	}
	var settings struct {
		ServerURL string `json:"serverUrl"`
		Locked    bool   `json:"locked"`
		AutoStart bool   `json:"autoStart"`
	}
	if err := json.Unmarshal(files["settings.json"], &settings); err != nil {
		t.Fatal(err)
	}
	if settings.ServerURL != "http://10.0.0.99:18082/" || !settings.Locked || !settings.AutoStart {
		t.Fatalf("settings = %#v", settings)
	}
	if !bytes.Contains(files["使用说明.txt"], []byte("双击 NASWallboard.Desktop.exe")) {
		t.Fatalf("instructions = %q", files["使用说明.txt"])
	}
	if !bytes.Contains(files["诊断启动.cmd"], []byte("Get-WinEvent")) ||
		!bytes.Contains(files["诊断启动.cmd"], []byte("startup.log")) {
		t.Fatalf("diagnostic launcher = %q", files["诊断启动.cmd"])
	}
	if bytes.Contains(files["诊断启动.cmd"], []byte("^|")) {
		t.Fatal("PowerShell pipeline is incorrectly escaped inside a quoted command")
	}
}

func TestWriteDesktopPackageMatchesBufferedPackage(t *testing.T) {
	assets := fstest.MapFS{
		"downloads/NASWallboard.Desktop.exe": {Data: append([]byte("MZ"), bytes.Repeat([]byte{0x31}, 4096)...)},
	}
	want, err := BuildPackage(assets, "http://nas.local:8080/")
	if err != nil {
		t.Fatal(err)
	}
	var streamed bytes.Buffer
	if err := WritePackage(&streamed, assets, "http://nas.local:8080/"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(streamed.Bytes(), want) {
		t.Fatal("streamed desktop package differs from buffered package")
	}
}

func TestDesktopPackageRejectsMissingOrInvalidExecutable(t *testing.T) {
	for name, assets := range map[string]fstest.MapFS{
		"missing": {},
		"not-pe":  {"downloads/NASWallboard.Desktop.exe": {Data: []byte("broken")}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildPackage(assets, "http://nas.local:8080/"); err == nil {
				t.Fatal("BuildPackage accepted an unavailable production executable")
			}
		})
	}
}

func TestDesktopPackageRejectsUnsafeServerURLs(t *testing.T) {
	assets := fstest.MapFS{"downloads/NASWallboard.Desktop.exe": {Data: []byte("MZvalid")}}
	for _, serverURL := range []string{
		"javascript:alert(1)",
		"http://user:pass@nas.local/",
		"http://nas.local/path",
		"http://nas.local/?token=secret",
		"http://nas.local/#fragment",
		"http://nas.local\r\nX-Forged: yes/",
	} {
		if _, err := BuildPackage(assets, serverURL); err == nil {
			t.Errorf("BuildPackage accepted %q", serverURL)
		}
	}
}

func unzipDesktopPackage(t *testing.T, payload []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name], err = io.ReadAll(entry)
		_ = entry.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}
