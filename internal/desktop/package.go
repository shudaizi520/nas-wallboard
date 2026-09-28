package desktop

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"strings"
	"time"
)

const executablePath = "downloads/NASWallboard.Desktop.exe"

var zipTimestamp = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

type clientSettings struct {
	ServerURL string `json:"serverUrl"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Locked    bool   `json:"locked"`
	AutoStart bool   `json:"autoStart"`
}

func BuildPackage(assets fs.FS, serverURL string) ([]byte, error) {
	var output bytes.Buffer
	if err := WritePackage(&output, assets, serverURL); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func ValidatePackage(assets fs.FS, serverURL string) error {
	if _, err := normalizeOrigin(serverURL); err != nil {
		return err
	}
	return validateExecutable(assets)
}

func WritePackage(destination io.Writer, assets fs.FS, serverURL string) error {
	origin, err := normalizeOrigin(serverURL)
	if err != nil {
		return err
	}
	if err := validateExecutable(assets); err != nil {
		return err
	}
	settings, err := json.MarshalIndent(clientSettings{
		ServerURL: origin,
		X:         -1,
		Y:         -1,
		Locked:    true,
		AutoStart: true,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode desktop settings: %w", err)
	}
	settings = append(settings, '\n')
	instructions := []byte("NAS 桌面小组件\r\n\r\n" +
		"1. 解压整个压缩包。\r\n" +
		"2. 双击 NASWallboard.Desktop.exe。\r\n" +
		"3. 需要移动或退出时，右键任务栏通知区域里的 NAS 图标。\r\n" +
		"4. 桌面显示内容请在 NAS 的 /manage 页面调整。\r\n")
	diagnosticLauncher := []byte("@echo off\r\n" +
		"chcp 65001 >nul\r\n" +
		"setlocal\r\n" +
		"set \"DIAG_FILE=%~dp0启动诊断.txt\"\r\n" +
		"del \"%DIAG_FILE%\" 2>nul\r\n" +
		"echo NAS Wallboard startup diagnostic > \"%DIAG_FILE%\"\r\n" +
		"echo Time: %date% %time% >> \"%DIAG_FILE%\"\r\n" +
		"\"%~dp0NASWallboard.Desktop.exe\"\r\n" +
		"echo ExitCode: %errorlevel% >> \"%DIAG_FILE%\"\r\n" +
		"echo. >> \"%DIAG_FILE%\"\r\n" +
		"echo Application startup.log: >> \"%DIAG_FILE%\"\r\n" +
		"type \"%LOCALAPPDATA%\\NASWallboard\\startup.log\" >> \"%DIAG_FILE%\" 2>nul\r\n" +
		"echo. >> \"%DIAG_FILE%\"\r\n" +
		"echo Recent Windows application errors: >> \"%DIAG_FILE%\"\r\n" +
		"powershell -NoProfile -Command \"Get-WinEvent -FilterHashtable @{LogName='Application'; StartTime=(Get-Date).AddMinutes(-5)} -ErrorAction SilentlyContinue | Where-Object {$_.ProviderName -in @('.NET Runtime','Application Error','Windows Error Reporting')} | Select-Object -First 8 TimeCreated,ProviderName,Id,LevelDisplayName,Message | Format-List\" >> \"%DIAG_FILE%\" 2>&1\r\n" +
		"start \"\" notepad.exe \"%DIAG_FILE%\"\r\n")

	writer := zip.NewWriter(destination)
	executableHeader := &zip.FileHeader{Name: "NASWallboard.Desktop.exe", Method: zip.Deflate}
	executableHeader.SetModTime(zipTimestamp)
	executableHeader.SetMode(0o644)
	executableEntry, err := writer.CreateHeader(executableHeader)
	if err != nil {
		_ = writer.Close()
		return fmt.Errorf("create executable ZIP entry: %w", err)
	}
	executable, err := assets.Open(executablePath)
	if err != nil {
		_ = writer.Close()
		return fmt.Errorf("open Windows executable: %w", err)
	}
	_, copyErr := io.Copy(executableEntry, executable)
	closeErr := executable.Close()
	if copyErr != nil {
		_ = writer.Close()
		return fmt.Errorf("write executable ZIP entry: %w", copyErr)
	}
	if closeErr != nil {
		_ = writer.Close()
		return fmt.Errorf("close Windows executable: %w", closeErr)
	}

	for _, entry := range []struct {
		name string
		data []byte
	}{
		{"settings.json", settings},
		{"使用说明.txt", instructions},
		{"诊断启动.cmd", diagnosticLauncher},
	} {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetModTime(zipTimestamp)
		header.SetMode(0o644)
		file, createErr := writer.CreateHeader(header)
		if createErr != nil {
			_ = writer.Close()
			return fmt.Errorf("create ZIP entry %s: %w", entry.name, createErr)
		}
		if _, writeErr := file.Write(entry.data); writeErr != nil {
			_ = writer.Close()
			return fmt.Errorf("write ZIP entry %s: %w", entry.name, writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish desktop ZIP: %w", err)
	}
	return nil
}

func validateExecutable(assets fs.FS) error {
	executable, err := assets.Open(executablePath)
	if err != nil {
		return fmt.Errorf("open Windows executable: %w", err)
	}
	defer executable.Close()
	var signature [2]byte
	if _, err := io.ReadFull(executable, signature[:]); err != nil {
		return fmt.Errorf("read Windows executable signature: %w", err)
	}
	if signature != [2]byte{'M', 'Z'} {
		return errors.New("Windows executable has an invalid PE signature")
	}
	return nil
}

func normalizeOrigin(value string) (string, error) {
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t") {
		return "", errors.New("invalid server URL")
	}
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("server URL must use HTTP or HTTPS")
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "/" {
		return "", errors.New("server URL must be an origin without credentials, path, query, or fragment")
	}
	return parsed.String(), nil
}
