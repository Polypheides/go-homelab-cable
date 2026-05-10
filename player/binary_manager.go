package player

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	ffmpegWinURL   = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
	ffmpegLinuxURL = "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz"
)

// EnsureDependencies checks for FFmpeg, FFprobe, and FFplay and downloads them if missing.
func EnsureDependencies() error {
	requiresDownload := false

	// Check for essential broadcasting tools (FFmpeg and FFprobe)
	if !hasExecutable("ffmpeg") || !hasExecutable("ffprobe") {
		requiresDownload = true
	}

	if !requiresDownload {
		return nil
	}

	Log.Println("[Binary Manager] Required media engines (FFmpeg/FFprobe) are missing!")
	Log.Printf("[Binary Manager] Would you like to download them automatically? (y/n): ")
	var response string
	fmt.Scanln(&response)

	if strings.ToLower(response) != "y" {
		Log.Printf("\n[Binary Manager] FFmpeg is required for transcoding and streaming.\n")
		Log.Printf("To install manually, download the static binaries from:\n")
		Log.Printf("Windows: %s\n", ffmpegWinURL)
		Log.Printf("Linux: %s\n", ffmpegLinuxURL)
		Log.Printf("\nExtract them and place the binaries in the 'bin/' folder next to this program.\n")

		Log.Println("\nPress Enter to exit...")
		var wait string
		fmt.Scanln(&wait)

		return fmt.Errorf("FFmpeg dependency not met")
	}

	return downloadAndInstallSuite()
}

// hasExecutable checks if an executable is available in the PATH or local bin/ folder.
func hasExecutable(name string) bool {
	_, err := exec.LookPath(name)
	if err == nil {
		return true
	}

	// If not in PATH, check local bin/
	localPath := filepath.Join("bin", name)
	if runtime.GOOS == "windows" {
		localPath += ".exe"
	}

	if _, err := os.Stat(localPath); err == nil {
		// Found locally, add bin/ to PATH for the current process if not already there
		absBin, _ := filepath.Abs("bin")
		path := os.Getenv("PATH")
		if !strings.Contains(path, absBin) {
			os.Setenv("PATH", absBin+string(os.PathListSeparator)+path)
		}
		return true
	}

	return false
}

func downloadAndInstallSuite() error {
	url := ""
	switch runtime.GOOS {
	case "windows":
		url = ffmpegWinURL
	case "linux":
		url = ffmpegLinuxURL
	default:
		return fmt.Errorf("automatic download not supported for %s. Please install FFmpeg manually", runtime.GOOS)
	}

	Log.Printf("[Binary Manager] Downloading media suite from %s...\n", url)

	if err := os.MkdirAll("bin", 0755); err != nil {
		return err
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: server returned %d", resp.StatusCode)
	}

	tmpFile := filepath.Join(os.TempDir(), "gocable_suite_archive")
	if runtime.GOOS == "windows" {
		tmpFile += ".zip"
	} else {
		tmpFile += ".tar.xz"
	}
	defer os.Remove(tmpFile)

	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		return err
	}

	Log.Println("[Binary Manager] Extracting binaries (ffmpeg, ffprobe, ffplay)...")
	if runtime.GOOS == "windows" {
		return extractZip(tmpFile, "bin")
	}
	return extractTarXZ(tmpFile, "bin")
}

func extractZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		name := strings.ToLower(f.Name)
		if strings.HasSuffix(name, "ffmpeg.exe") || strings.HasSuffix(name, "ffprobe.exe") || strings.HasSuffix(name, "ffplay.exe") {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			baseName := filepath.Base(f.Name)
			path := filepath.Join(dest, baseName)

			dstFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer dstFile.Close()

			if _, err := io.Copy(dstFile, rc); err != nil {
				return err
			}
			Log.Printf("[Binary Manager] Extracted %s\n", baseName)
		}
	}
	return nil
}

// extractTarXZ uses the system 'tar' command as a robust fallback for Linux.
func extractTarXZ(src, dest string) error {
	// We use -xJf to handle XZ and --strip-components to avoid nested folders
	// We search for the specific binaries inside the tarball and extract them
	cmd := exec.Command("tar", "-xJf", src, "-C", dest, "--wildcards", "*/ffmpeg", "*/ffprobe", "*/ffplay", "--strip-components=1")
	// If the above fails (some tar versions don't like --wildcards), try a simpler extraction
	if err := cmd.Run(); err != nil {
		Log.Println("[Binary Manager] Standard extraction failed, trying fallback...")
		cmd = exec.Command("tar", "-xJf", src, "-C", dest)
		return cmd.Run()
	}
	return nil
}

// CheckStatus returns whether ffmpeg and ffprobe are available.
func CheckStatus() (ffmpeg bool, ffprobe bool) {
	return hasExecutable("ffmpeg"), hasExecutable("ffprobe")
}
