package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Polypheides/go-homelab-cable/client"
	"github.com/Polypheides/go-homelab-cable/domain"
	"github.com/Polypheides/go-homelab-cable/network"
	"github.com/Polypheides/go-homelab-cable/player"
	"github.com/Polypheides/go-homelab-cable/server"
	cli "github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Commands: []*cli.Command{
			{
				Name:  "server",
				Usage: "start a homelab cable server",
				Action: func(cCtx *cli.Context) error {
					n := network.NewNetwork(cCtx.String("network_name"), cCtx.String("network_owner"), cCtx.String("network_callsign"))
					s := server.NewServer(cCtx.String("port"), n)
					
					strategy := player.MediaListSortStrategy(player.SortStratRandom{})
					if cCtx.Bool("episodic") {
						strategy = player.SortStratAlphabetical{}
					}

					paths := cCtx.StringSlice("path")
					for _, p := range paths {
						list, err := player.FromFolder(p, strategy)
						if err != nil {
							return fmt.Errorf("couldn't load media from %s: %w", p, err)
						}
						c := n.AddChannel(list)
						// Initialize all channels to be "ready" in the background
						if n.Live() == "" {
							_ = n.SetChannelLive(c.ID)
						}
					}
					
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
						Name:     "path",
						Usage:    "path to media folder (repeat for multiple channels)",
						Required: true,
					},
					&cli.BoolFlag{
						Name:  "episodic",
						Usage: "play media in alphabetical order (A-Z)",
						Value: false,
					},
					&cli.BoolFlag{
						Name:  "random",
						Usage: "play media in random order (default)",
						Value: true,
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
						Name:  "tune",
						Usage: "switch the host-tuned live channel to the specified channel ID",
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
