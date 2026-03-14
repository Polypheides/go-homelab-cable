package main

import (
	"encoding/json"
	"fmt"
	"os"

	"strconv"
	"strings"

	"github.com/Polypheides/go-homelab-cable/client"
	"github.com/Polypheides/go-homelab-cable/domain"
	"github.com/Polypheides/go-homelab-cable/network"
	"github.com/Polypheides/go-homelab-cable/player"
	"github.com/Polypheides/go-homelab-cable/server"
	cli "github.com/urfave/cli/v2"
)

const banner = `
   ___        ___      _     _      
  / _ \___   / __\__ _| |__ | | ___ 
 / /_\/ _ \ / /  / _  | '_ \| |/ _ \
/ /_\\ (_) / /__| (_| | |_) | |  __/
\____/\___/\____/\__,_|_.__/|_|\___| v1.1.0
`

func main() {
	app := &cli.App{
		Name:    "GoCable",
		Version: "v1.1.0",
		Usage:   "A homelab cable network streaming server and client",
		Commands: []*cli.Command{
			{
				Name:  "server",
				Usage: "start a homelab cable server",
				Action: func(cCtx *cli.Context) error {
					n := network.NewNetwork(
						cCtx.String("network_name"),
						cCtx.String("network_owner"),
						cCtx.String("network_callsign"),
						cCtx.String("protocol"),
						cCtx.Bool("stereo"),
					)
					n.WebServerPort = cCtx.String("port") // Ensure Network knows its external web port
					s := server.NewServer(cCtx.String("port"), n)

					// Handle unified paths: path[:season][:mode]
					paths := cCtx.StringSlice("path")
					for _, raw := range paths {
						cfg := parseChannelConfig(raw)

						// Determine strategy
						strategy := player.MediaListSortStrategy(player.SortStratRandom{})
						if cfg.mode == "e" || (cfg.mode == "" && cCtx.Bool("episodic")) {
							strategy = player.SortStratAlphabetical{}
						}

						list, err := player.FromFolderWithSeason(cfg.path, strategy, cfg.season)
						if err != nil {
							return fmt.Errorf("couldn't load media from %s: %w", cfg.path, err)
						}

						c, err := n.AddChannel(list)
						if err != nil {
							return fmt.Errorf("failed to add channel for list from %s: %w", cfg.path, err)
						}
						if n.Live() == "" {
							_ = n.SetChannelLive(c.ID)
						}
					}

					fmt.Print(banner)
					s.Serve()
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
				},
			},
			{
				Name:  "client",
				Usage: "start a homelab cable client to interact with an already-running server",
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

							if c.JSONOut {
								chanBytes, err := json.MarshalIndent(channels, "", "  ")
								if err != nil {
									return err
								}
								fmt.Println(string(chanBytes))
								return nil
							}

							for _, channel := range channels {
								fmt.Println(channel)
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
					fmt.Println(list.All())
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
		},
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

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

func printChannel(c *client.Client, channel domain.Channel) error {
	if c.JSONOut {
		chanBytes, err := json.MarshalIndent(channel, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(chanBytes))
		return nil
	}

	fmt.Println(channel)
	return nil
}

type channelConfig struct {
	path   string
	season int
	mode   string
}

func parseChannelConfig(raw string) channelConfig {
	parts := strings.Split(raw, ":")
	if len(parts) == 0 {
		return channelConfig{}
	}

	cfg := channelConfig{}
	pathEndIdx := 1

	// Handle Windows drive (e.g. "C:\")
	if len(parts[0]) == 1 && len(parts) > 1 {
		cfg.path = parts[0] + ":" + parts[1]
		pathEndIdx = 2
	} else {
		cfg.path = parts[0]
	}

	// Process remaining parts
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
