package main

import (
	"gopkg.in/urfave/cli.v1"
	"log"
	"os"
)

func main() {
	app := cli.NewApp()
	app.Name = "vhd"
	app.Usage = "Commands to manage VHDs"

	// global level flags
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "verbose",
			Usage: "Show more output",
		},
	}

	app.Commands = []cli.Command{
		vhdInspectCmdHandler(),
		vhdUploadCmdHandler(),
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatalln(err)
	}
}
