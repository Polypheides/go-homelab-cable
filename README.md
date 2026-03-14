# GoCable (Polypheides Edition) 📺

A high-performance Go application for playing media in your homelab the way a television station would. Set up multiple "virtual channels" that continuously broadcast media to your network.

---

## 📺 How it Works
Think of GoCable like a physical TV rack. You point it at folders (channels), and it broadcasts them 24/7. Use the "Live Window" or "Master Stream" to watch what's currently tuned.

```mermaid
graph TD
    A[Folder: Movies] -->|Channel 0| B[Broadcaster]
    C[Folder: Shows] -->|Channel 1| D[Broadcaster]
    B -->|Bcast: 5000| E[TV Network]
    D -->|Bcast: 5001| E
    E -->|Tuned Channel| F[Master Port 4999]
    F -->|Watch| G[VLC Player]
```

---

## 🎛️ Key Features

- **Multi-Channel Architecture**: Each folder you add becomes an independent channel with its own dedicated broadcast relay.
- **UDP, TCP & HTTP Streaming**: Three protocols supported. UDP/TCP stream to VLC directly; HTTP streaming is always-on via the built-in web server.
- **Master Stream (Port 4999 / `/master`)**: A central hub that always broadcasts the currently tuned channel, available over UDP, TCP, and HTTP.
- **TCP Fan-out Relayer**: Custom Go-side server allows **multiple simultaneous clients** to connect to the same TCP stream.
- **HTTP Per-Channel Streams**: Every channel is accessible over HTTP at `http://<host>:<port>/<channel_num>/` — great for browser-based players and network compatibility.
- **Premium Web Dashboard**: A modern, HTMX-powered interface featuring "metal" tactile controls and real-time "LCD" track updates with smooth DOM morphing.
- **Station "Broadcast Bug"**: Automatically burns your network callsign (e.g., **KHLC**) into the bottom-right corner as a semi-transparent, shadowed overlay for a professional broadcast look.
- **Smart GPU Transcoding**: Transparently probes your hardware for NVIDIA (**NVENC**), Intel (**QSV**), AMD (**AMF**), or generic (**VAAPI/MF**) encoders. CPU usage stays low while keeping the "bug" active.
- **Professional Audio Normalization**: Integrated `loudnorm` filter ensures every file matches the **EBU R128** television standard (-24.0 LUFS) with immediate true-peak limiting.
- **Recursive Discovery**: Automatically find media in nested subfolders (e.g., `Season 1/`, `S02/`).

---

## 🎞️ Media Recommendations
For the best experience (and smoothest transitions):
- **Format**: `.mkv` or `.mp4`
- **Codec**: **H.265 / HEVC** is highly recommended.
- **Why?**: GoCable is specifically tuned with synchronization flags optimized for HEVC streams to ensure professional, "glitch-free" switching between files.

---

## 🔧 Dependencies

Follow these steps exactly to get everything ready:

### 1. Install FFmpeg (The Engine)
- **Windows**: 
  1. Download the "essentials" zip from [gyan.dev](https://www.gyan.dev/ffmpeg/builds/).
  2. Extract it to `C:\ffmpeg`.
  3. Search for "Edit the system environment variables" in Windows.
  4. Click **Environment Variables** > **Path** > **Edit** > **New**.
  5. Paste `C:\ffmpeg\bin` and click OK.
- **Linux**: Run `sudo apt update && sudo apt install ffmpeg`.

### 2. Install VLC (The Screen)
- Download and install normally from [videolan.org](https://www.videolan.org/vlc/).
- **Linux Users**: If you want the server to open a window, also run `sudo apt install vlc libvlc-dev`.

### 3. Install Go (The Compiler)
- Download from [go.dev](https://go.dev/dl/).

<details>
<summary><b>Step-by-Step Go Installation Guide</b></summary>

#### 🐧 Linux
1. **Cleanup**: Remove any previous installation:
   `sudo rm -rf /usr/local/go`
2. **Extract**: Unpack the archive into `/usr/local` (replace with your version filename):
   `sudo tar -C /usr/local -xzf go1.XX.X.linux-amd64.tar.gz`
3. **Set Path**: Add the following line to your `$HOME/.profile`:
   `export PATH=$PATH:/usr/local/go/bin`
4. **Apply**: Run `source $HOME/.profile`.
5. **Verify**: Type `go version` to confirm.

#### 🪟 Windows
1. **Run Installer**: Open the downloaded `.msi` file and follow the prompts.
2. **Refresh Environment**: Close and reopen any open PowerShell/CMD windows.
3. **Verify**: Type `go version` in a new terminal window to confirm.
</details>

---

## 📦 Build Instructions

### 1. Build the Application
**Windows:**
```powershell
go build -o cable.exe ./cmds/cli
```
**Linux:**
```bash
go build -o cable ./cmds/cli
```

### 2. Live GUI Mode (Requires VLC SDK)
**Windows:**
```powershell
go build -tags vlc -o cable.exe ./cmds/cli
```
**Linux:**
```bash
go build -tags vlc -o cable ./cmds/cli
```

---

## 🚀 Quick Start

### 1. Start the Station
Run the server and point it to one or more folders. A "Channel" is automatically created for every `--path` you provide.

**The Path Syntax:** `path[:season][:mode]`
- **Season**: (Optional) Provide a number to only play that season.
- **Mode**: (Optional) Use `e` for Episodic (A-Z) or `r` for Random.

```powershell
# Binge Season 2 in order, then some Random Movies
./cable.exe server --path "C:\Shows\ShowName:2:e" --path "C:\Movies:r"

# Use TCP instead of UDP for the broadcast protocol
./cable.exe server --path "C:\Movies" --protocol tcp

# Launch with a custom Callsign bug
./cable.exe server --path "C:\Shows" --network_callsign "POL-TV"

# Disable the Broadcast Bug overlay entirely
./cable.exe server --path "C:\Shows" --no_bug
```

> [!NOTE]
> The `--protocol` flag controls the **UDP/TCP broadcast** on ports 5000+. HTTP streaming via the web server (`/master`, `/<channel_num>/`) is **always available** regardless of this flag.

> [!TIP]
> If you just use `--path "C:\Shows"`, it will default to **Random**. Add `--episodic` at the end to make all "blind" paths play in order.

### 2. View the Dashboard
Navigate to `http://localhost:3004` to see your station's status and control channels with the **Next (<)**, **Previous (>)**, and **TUNE** buttons.

### 3. Tune in via VLC
Open VLC and connect to the Master Stream or a specific channel:

| Protocol | Master Stream | Channel 0 | Channel 1 |
|---|---|---|---|
| **UDP** | `udp://@127.0.0.1:4999` | `udp://@127.0.0.1:5000` | `udp://@127.0.0.1:5001` |
| **TCP** | `tcp://127.0.0.1:4999` | `tcp://127.0.0.1:5000` | `tcp://127.0.0.1:5001` |
| **HTTP** | `http://localhost:3004/master` | `http://localhost:3004/0/` | `http://localhost:3004/1/` |

> [!TIP]
> HTTP streams work in any browser or media player that supports `video/mp2t`. Use them when UDP/TCP is blocked by a firewall or when streaming over the internet.

---

## 📡 CLI Client Commands

The CLI allows you to control your station from the terminal:

- **List All Channels**: `client channels` (or `client list`)
- **Tune to Channel 0**: `client tune 0`
- **Skip to Next Show**: `client next`
- **Go to Previous Show**: `client previous`

---

## 📐 Technical Architecture

- **Broadcaster**: Manages an independent FFmpeg process for each channel, streaming to unique ports (starting at 5000).
- **MasterBroadcaster**: Relays the stream of the active channel to port 4999.
- **Smart GPU Core**: Detects the best possible HEVC encoder at runtime (prioritizing HW over SW). All bugged streams are re-encoded into high-quality H.265 using vendor-optimized presets.
- **Network Layer**: Thread-safe management of channel states and "Live" tuning logic.
- **HTMX Server**: Provides a "morphed" real-time UI that reflects station changes across all connected browsers instantly.
- **HTTP Streaming**: The Echo web server exposes `GET /master` and `GET /:channel_num/` as `video/mp2t` HTTP streams.

### Protocols at a Glance

| Protocol | Flag | Ports | Always On? |
|---|---|---|---|
| UDP | `--protocol udp` (default) | 4999 (master), 5000+ | ✅ |
| TCP | `--protocol tcp` | 4999 (master), 5000+ | ✅ |
| HTTP | *(none needed)* | Web server port (default `3004`) | ✅ Always |

---

## ⚠️ Troubleshooting (FAQ)

**Q: I see a black screen in VLC!**
- Ensure the server logs show a media file is "Playing". 
- Check if your path contains folders with actual video files (`.mp4`, `.mkv`, etc.).

**Q: "cable.exe" is not recognized...**
- Make sure you ran the `go build` command first! Check that `cable.exe` exists in your folder.

**Q: VLC won't open on Linux?**
- Ensure you have a monitor connected and `DISPLAY=:0.0` is set if running via SSH.

**Q: Error: "FFmpeg not found"**
- Go back to the **Dependencies** section and make sure you added FFmpeg to your system PATH!

---
Enjoy your professional homelab broadcast experience!
