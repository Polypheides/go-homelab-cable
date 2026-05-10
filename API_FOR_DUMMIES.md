# GoCable API for Dummies

This guide explains how to control your GoCable server using the API. Think of the API as a "remote control" that works through your terminal instead of a physical remote.

The examples below use the `curl.exe` command.

---

## 1. Check if the "Braintrust" is Ready
Before you start, check if the server has the required tools (FFmpeg and FFprobe).

**The Command:**
```bash
curl.exe http://localhost:3004/api/status/binaries
```
**What success looks like:**
`{"ffmpeg":true,"ffprobe":true}`

---

## 2. List Your Channels
See what channels are currently available on your network.

**The Command:**
```bash
curl.exe http://localhost:3004/api/networks/PNET:%23/channels
```
*Note: The `%23` is just a special code for the `#` symbol in the network name.*

---

## 3. Tune to a Channel
This is like pressing a number on your remote to change the channel.

**The Command (to tune to Channel 2):**
```bash
curl.exe -X PUT http://localhost:3004/api/networks/PNET:%23/channels/2/set_live
```

---

## 4. Skip and Rewind
Skip to the next movie or go back to the previous one on the "Live" tuner.

**Skip Next:**
```bash
curl.exe -X PUT http://localhost:3004/api/networks/PNET:%23/live/next
```

**Rewind Previous:**
```bash
curl.exe -X PUT http://localhost:3004/api/networks/PNET:%23/live/previous
```

---

## 5. Add a New Channel Anytime
You don't need to restart the server to add a new folder of videos. You can do it while the server is running.

**The Command:**
```bash
curl.exe -X POST -H "Content-Type: application/json" -d "{\"path\": \"C:/MyVideos\", \"season\": 1, \"mode\": \"e\"}" http://localhost:3004/api/networks/PNET:%23/channels
```
- **path**: Where the videos are.
- **season**: Optional (use 0 for all).
- **mode**: Use "e" for episodic (orderly) or "r" for random.

---

## Troubleshooting
- **404 Error**: Usually means you typed the network name (PNET:#) wrong.
- **400 Error**: Usually means your JSON (the text inside the `{}` for the Add Channel command) is formatted incorrectly.
