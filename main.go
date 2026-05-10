package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"strconv"
	"strings"

	"github.com/Polypheides/go-homelab-cable/client"
	"github.com/Polypheides/go-homelab-cable/domain"
	"github.com/Polypheides/go-homelab-cable/logger"
	"github.com/Polypheides/go-homelab-cable/network"
	"github.com/Polypheides/go-homelab-cable/player"
	"github.com/Polypheides/go-homelab-cable/server"
	cli "github.com/urfave/cli/v2"
)

// --- Globals & Constants ---

const banner = `
   ___        ___      _     _      
  / _ \___   / __\__ _| |__ | | ___ 
 / /_\/ _ \ / /  / _  | '_ \| |/ _ \
/ /_\\ (_) / /__| (_| | |_) | |  __/
\____/\___/\____/\__,_|_.__/|_|\___| v1.1.0
`

var (
	isInteractive   bool
	interactiveMu   sync.Mutex
	activeNetwork   *network.Network
	activeServer    *server.Server
	isPromptVisible bool
)

// --- Lifecycle ---

// main initializes and executes the GoCable CLI application.
func main() {
	logger.SetLogger(shellPrint)
	app := newApp()

	if len(os.Args) < 2 {
		// If no args, spawn a CMD window and run in interactive mode
		// We use /c so that the window closes when GoCable exits
		cmd := exec.Command("cmd", "/c", "start", "cmd", "/c", os.Args[0], "interactive")
		if err := cmd.Start(); err != nil {
			// Fallback if launcher fails
			runInteractiveMode(app)
		}
		return
	}

	if err := app.Run(os.Args); err != nil {
		logger.For("system").Printf("[GoCable] Fatal error: %v\n", err)
		os.Exit(1)
	}
}

// runInteractiveMode provides a persistent shell for users who launch the app without arguments.
func runInteractiveMode(app *cli.App) {
	isInteractive = true
	sys := logger.For("system")
	sys.Print(banner)
	sys.Println("\n[Interactive Mode] Welcome to GoCable!")
	sys.Println("Type 'server', 'client', or 'help' to begin. Type 'shutdown' to stop server.")
	sys.Println("Type 'exit' to quit and close window.")

	// Let Ctrl+C trigger the scanner failure below for a graceful exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		interactiveMu.Lock()
		fmt.Print("\nGoCable> ")
		isPromptVisible = true
		interactiveMu.Unlock()

		if !scanner.Scan() {
			// Ctrl+C or EOF: Recreate scanner and continue silently to stay persistent
			scanner = bufio.NewScanner(os.Stdin)
			continue
		}

		line := strings.TrimSpace(scanner.Text())

		interactiveMu.Lock()
		isPromptVisible = false
		interactiveMu.Unlock()

		if line == "" {
			continue
		}

		if line == "shutdown" {
			sys := logger.For("system")
			if activeNetwork != nil || activeServer != nil {
				sys.Println("[Server] Shutting down active context...")
				if activeNetwork != nil {
					activeNetwork.Stop()
					activeNetwork = nil
				}
				if activeServer != nil {
					activeServer.Stop()
					activeServer = nil
				}
				sys.Println("[Server] Shutdown complete.")
			} else {
				sys.Println("[Server] No active server to shutdown.")
			}
			continue
		}

		if line == "exit" || line == "quit" {
			break
		}

		// Split input into arguments and run the command
		fields := splitArgs(line)
		fullArgs := append([]string{os.Args[0]}, fields...)

		if err := app.Run(fullArgs); err != nil {
			logger.For("system").Printf("\n[Error] %v\n", err)
		}
	}

	// Always perform graceful cleanup when exiting the shell loop
	logger.For("system").Println("\n[GoCable] Shutting down gracefully...")
	if activeNetwork != nil {
		activeNetwork.Stop()
	}
	if activeServer != nil {
		activeServer.Stop()
	}
	logger.For("system").Println("Goodbye!")
}

// --- CLI Application Factory ---

// newApp creates and configures the CLI application.
func newApp() *cli.App {
	return &cli.App{
		Name:    "GoCable",
		Version: "v1.1.0",
		Usage:   "A homelab cable network streaming server and client",
		CustomAppHelpTemplate: `GoCable v1.1.0 - Homelab Cable Suite

COMMANDS:
  server    Start a broadcaster
    Options: --path, --port, --episodic, --stereo, --no_bug
    
  client    Connect to a server
    Options: --host, --port, --json
    Subcommands: channels, tune

USER SHORTCUTS:
  tune, next, prev, exit, shutdown

QUICK START:
  1. GoCable server --path "C:\Videos"
  2. GoCable client channels

For detailed help on any command, use:
  GoCable <command> --help
`,
		ExitErrHandler: func(c *cli.Context, err error) {
			// Prevent the app from exiting the process so the REPL stays alive
		},
		Commands: []*cli.Command{
			{
				Name:  "server",
				Usage: "start a homelab cable server",
				Action: func(cCtx *cli.Context) error {
					if cCtx.String("log") != "" {
						logger.Enable(cCtx.String("log"))
					}
					if err := player.EnsureDependencies(); err != nil {
						return err
					}

					n := network.NewNetwork(
						cCtx.String("network_name"),
						cCtx.String("network_owner"),
						cCtx.String("network_callsign"),
						cCtx.String("protocol"),
						cCtx.Bool("stereo"),
					)
					activeNetwork = n
					n.NoBug = cCtx.Bool("no_bug")
					n.WebServerPort = cCtx.String("port")
					s := server.NewServer(cCtx.String("port"), n)
					activeServer = s

					paths := cCtx.StringSlice("path")
					if len(paths) == 0 {
						logger.For("system").Println("[Server] --help for usage")
						return nil
					}

					var firstID string
					for _, raw := range paths {
						cfg := parseChannelConfig(raw)

						c, err := n.AddChannelFromPath(cfg.path, cfg.season, cfg.mode)
						if err != nil {
							return err
						}
						if firstID == "" {
							firstID = c.ID
						}
					}

					if firstID != "" && n.Live() == "" {
						_ = n.SetChannelLive(firstID)
					}

					logger.For("system").Printf("[Network] Initializing %s with %d channels...\n", n.Name, len(n.Channels()))
					logger.For("player").Printf("[Broadcaster] Streaming ports 5000-%d active.\n", 5000+len(n.Channels())-1)
					logger.For("master").Println("[Master] Relay active (Port 4999).")

					if isInteractive {
						ready := make(chan struct{})
						go s.Serve(ready)
						<-ready
						// Give the Echo startup logs a moment to print
						time.Sleep(100 * time.Millisecond)
						sys := logger.For("system")
						sys.Println("\n[Server] Started in background. Returning to shell...")
						sys.Println("[Server] You can now use 'client' commands in this same window.")
						return nil
					}

					// Set up graceful shutdown
					stop := make(chan os.Signal, 1)
					signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

					go func() {
						s.Serve(nil)
					}()

					<-stop
					sys := logger.For("system")
					sys.Println("\n[Server] Shutting down gracefully...")
					n.Stop()
					sys.Println("[Server] All channels stopped. Goodbye!")
					return nil
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "port",
						Value: "3004",
						Usage: "port to run on",
					},
					&cli.StringFlag{
						Name:  "protocol",
						Value: "udp",
						Usage: "streaming protocol to use (udp, tcp)",
					},
					&cli.StringFlag{
						Name:  "network_name",
						Value: "Homelab Cable",
						Usage: "the name of your homelab cable network",
					},
					&cli.StringFlag{
						Name:  "network_owner",
						Value: "clabretro",
						Usage: "the owner of your homelab cable network",
					},
					&cli.StringFlag{
						Name:  "network_callsign",
						Value: "KHLC",
						Usage: "the call sign of your homelab cable network",
					},
					&cli.StringSliceFlag{
						Name:  "path",
						Usage: "path[:season][:mode] (e.g. \"C:\\Shows:1:e\")",
					},
					&cli.BoolFlag{
						Name:  "episodic",
						Usage: "play media in alphabetical order (global default)",
						Value: false,
					},
					&cli.BoolFlag{
						Name:  "random",
						Usage: "play media in random order (global default)",
						Value: true,
					},
					&cli.BoolFlag{
						Name:  "stereo",
						Usage: "force 2-channel stereo AC3 for all broadcasts (better for old TVs/Pi)",
						Value: false,
					},
					&cli.BoolFlag{
						Name:  "no_bug",
						Usage: "disable the station callsign overlay bug",
						Value: false,
					},
					&cli.StringFlag{
						Name:  "log",
						Usage: "enable specific log categories (e.g. server:client:player)",
					},
				},
			},
			{
				Name:  "client",
				Usage: "start a homelab cable client to interact with an already-running server",
				Action: func(cCtx *cli.Context) error {
					if cCtx.NArg() == 0 {
						logger.For("system").Println("[Client] --help for usage")
						return nil
					}
					return nil
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "port",
						Value: "3004",
						Usage: "server port to connect to",
					},
					&cli.StringFlag{
						Name:  "host",
						Value: "http://localhost",
						Usage: "host the server is running on",
					},
					&cli.BoolFlag{
						Name:  "json",
						Value: false,
						Usage: "output command results in JSON",
					},
				},
				Subcommands: []*cli.Command{
					{
						Name:    "channels",
						Aliases: []string{"list"},
						Usage:   "list all active channels on the network",
						Action: func(cCtx *cli.Context) error {
							c, err := connect(cCtx)
							if err != nil {
								return err
							}
							channels, err := c.Channels()
							if err != nil {
								return err
							}

							sys := logger.For("system")
							if c.JSONOut {
								chanBytes, err := json.MarshalIndent(channels, "", "  ")
								if err != nil {
									return err
								}
								sys.Println(string(chanBytes))
								return nil
							}

							for _, channel := range channels {
								sys.Println(channel)
							}
							return nil
						},
					},
					{
						Name:      "tune",
						Usage:     "switch the host-tuned live channel to the specified channel ID",
						ArgsUsage: "<channel_id>",
						Action: func(cCtx *cli.Context) error {
							id := cCtx.Args().First()
							if id == "" {
								return fmt.Errorf("must specify a channel ID")
							}
							c, err := connect(cCtx)
							if err != nil {
								return err
							}
							channel, err := c.Tune(id)
							if err != nil {
								return err
							}

							return printChannel(c, channel)
						},
					},
				},
			},
			{
				Name:  "path_test",
				Usage: "list the media files a given --path would play",
				Action: func(cCtx *cli.Context) error {
					path := cCtx.String("path")
					strategy := player.MediaListSortStrategy(player.SortStratRandom{})
					if cCtx.Bool("episodic") {
						strategy = player.SortStratAlphabetical{}
					}
					list, err := player.FromFolder(path, strategy)
					if err != nil {
						return err
					}
					logger.For("system").Println(list.All())
					return nil
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "path",
						Value:    "",
						Usage:    "path to media folder",
						Required: true,
					},
					&cli.BoolFlag{
						Name:  "episodic",
						Usage: "sort alphabetically",
					},
				},
			},
			{
				Name:   "interactive",
				Hidden: true,
				Action: func(cCtx *cli.Context) error {
					runInteractiveMode(cCtx.App)
					return nil
				},
			},
			{
				Name:      "tune",
				Usage:     "shorthand for 'client tune'",
				ArgsUsage: "<channel_id>",
				Action: func(cCtx *cli.Context) error {
					id := cCtx.Args().First()
					if id == "" {
						return fmt.Errorf("must specify a channel ID")
					}
					c, err := connect(cCtx)
					if err != nil {
						return err
					}
					channel, err := c.Tune(id)
					if err != nil {
						return err
					}
					return printChannel(c, channel)
				},
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "port", Value: "3004"},
					&cli.StringFlag{Name: "host", Value: "http://localhost"},
				},
			},
			{
				Name:  "next",
				Usage: "shorthand for 'client live next'",
				Action: func(cCtx *cli.Context) error {
					c, err := connect(cCtx)
					if err != nil {
						return err
					}
					channel, err := c.LiveNext()
					if err != nil {
						return err
					}
					return printChannel(c, channel)
				},
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "port", Value: "3004"},
					&cli.StringFlag{Name: "host", Value: "http://localhost"},
				},
			},
			{
				Name:    "prev",
				Aliases: []string{"previous"},
				Usage:   "shorthand for 'client live previous'",
				Action: func(cCtx *cli.Context) error {
					c, err := connect(cCtx)
					if err != nil {
						return err
					}
					channel, err := c.LivePrev()
					if err != nil {
						return err
					}
					return printChannel(c, channel)
				},
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "port", Value: "3004"},
					&cli.StringFlag{Name: "host", Value: "http://localhost"},
				},
			},
		},
	}
}

// --- Support & Helpers ---

// shellPrint prints a formatted message, clearing and restoring the REPL prompt if necessary.
func shellPrint(format string, a ...interface{}) (int, error) {
	interactiveMu.Lock()
	defer interactiveMu.Unlock()

	if isInteractive && isPromptVisible {
		fmt.Print("\r\033[K")
	}
	n, err := fmt.Printf(format, a...)
	if isInteractive && isPromptVisible {
		fmt.Print("GoCable> ")
	}
	return n, err
}

// splitArgs slices a string into a list of arguments, respecting double-quoted substrings.
func splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false
	escaped := false

	for _, r := range s {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		switch {
		case r == '\\':
			escaped = true
		case r == '"':
			inQuotes = !inQuotes
		case r == ' ' && !inQuotes:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// connect initializes a client connection based on CLI flags.
func connect(ctx *cli.Context) (*client.Client, error) {
	port := ctx.String("port")
	host := ctx.String("host")
	jsonOut := ctx.Bool("json")

	c, err := client.Connect(host, port)

	if err != nil {
		return nil, fmt.Errorf("couldn't connect to homelab-cable server at %s: %w", host+":"+port, err)
	}
	c.JSONOut = jsonOut
	return c, nil
}

// printChannel outputs channel metadata in plain text or JSON format.
func printChannel(c *client.Client, channel domain.Channel) error {
	sys := logger.For("system")
	if c.JSONOut {
		chanBytes, err := json.MarshalIndent(channel, "", "  ")
		if err != nil {
			return err
		}
		sys.Println(string(chanBytes))
		return nil
	}

	sys.Println(channel)
	return nil
}

type channelConfig struct {
	path   string
	season int
	mode   string
}

// parseChannelConfig parses the path[:season][:mode] string format.
func parseChannelConfig(raw string) channelConfig {
	parts := strings.Split(raw, ":")
	if len(parts) == 0 {
		return channelConfig{}
	}

	cfg := channelConfig{}
	pathEndIdx := 1

	if len(parts[0]) == 1 && len(parts) > 1 {
		cfg.path = parts[0] + ":" + parts[1]
		pathEndIdx = 2
	} else {
		cfg.path = parts[0]
	}

	for i := pathEndIdx; i < len(parts); i++ {
		p := strings.ToLower(parts[i])
		if p == "e" || p == "r" {
			cfg.mode = p
		} else if s, err := strconv.Atoi(p); err == nil {
			cfg.season = s
		}
	}

	return cfg
}
