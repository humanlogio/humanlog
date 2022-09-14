package main

import (
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/adrg/xdg"
	"github.com/aybabtme/humanlog"
	"github.com/aybabtme/rgbterm"
	"github.com/mattn/go-colorable"
	"github.com/urfave/cli"
)

var Version = "devel"

func fatalf(c *cli.Context, format string, args ...interface{}) {
	log.Printf(format, args...)
	cli.ShowAppHelp(c)
	os.Exit(1)
}

func main() {
	app := newApp()
	_ = xdg.DataHome

	prefix := rgbterm.FgString(app.Name+"> ", 99, 99, 99)

	log.SetOutput(colorable.NewColorableStderr())
	log.SetFlags(0)
	log.SetPrefix(prefix)
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func newApp() *cli.App {

	skip := cli.StringSlice{}
	keep := cli.StringSlice{}

	skipFlag := cli.StringSliceFlag{
		Name:  "skip",
		Usage: "keys to skip when parsing a log entry",
		Value: &skip,
	}

	keepFlag := cli.StringSliceFlag{
		Name:  "keep",
		Usage: "keys to keep when parsing a log entry",
		Value: &keep,
	}

	sortLongest := cli.BoolTFlag{
		Name:  "sort-longest",
		Usage: "sort by longest key after having sorted lexicographically",
	}

	skipUnchanged := cli.BoolTFlag{
		Name:  "skip-unchanged",
		Usage: "skip keys that have the same value than the previous entry",
	}

	truncates := cli.BoolFlag{
		Name:  "truncate",
		Usage: "truncates values that are longer than --truncate-length",
	}

	truncateLength := cli.IntFlag{
		Name:  "truncate-length",
		Usage: "truncate values that are longer than this length",
		Value: humanlog.DefaultOptions.TruncateLength,
	}

	colorFlag := cli.StringFlag{
		Name:  "color",
		Usage: "specify color mode: auto, on/force, off",
		Value: "auto",
	}

	lightBg := cli.BoolFlag{
		Name:  "light-bg",
		Usage: "use black as the base foreground color (for terminals with light backgrounds)",
	}

	timeFormat := cli.StringFlag{
		Name:  "time-format",
		Usage: "output time format, see https://golang.org/pkg/time/ for details",
		Value: humanlog.DefaultOptions.TimeFormat,
	}

	ignoreInterrupts := cli.BoolFlag{
		Name:  "ignore-interrupts, i",
		Usage: "ignore interrupts",
	}

	messageFields := cli.StringSlice{}
	messageFieldsFlag := cli.StringSliceFlag{
		Name:   "message-fields, m",
		Usage:  "Custom JSON fields to search for the log message. (i.e. mssge, data.body.message)",
		EnvVar: "HUMANLOG_MESSAGE_FIELDS",
		Value:  &messageFields,
	}

	timeFields := cli.StringSlice{}
	timeFieldsFlag := cli.StringSliceFlag{
		Name:   "time-fields, t",
		Usage:  "Custom JSON fields to search for the log time. (i.e. logtime, data.body.datetime)",
		EnvVar: "HUMANLOG_TIME_FIELDS",
		Value:  &timeFields,
	}

	levelFields := cli.StringSlice{}
	levelFieldsFlag := cli.StringSliceFlag{
		Name:   "level-fields, l",
		Usage:  "Custom JSON fields to search for the log level. (i.e. somelevel, data.level)",
		EnvVar: "HUMANLOG_LEVEL_FIELDS",
		Value:  &levelFields,
	}

	app := cli.NewApp()
	app.Author = "Antoine Grondin"
	app.Email = "antoinegrondin@gmail.com"
	app.Name = "humanlog"
	app.Version = Version
	app.Usage = "reads structured logs from stdin, makes them pretty on stdout!"

	app.Flags = []cli.Flag{skipFlag, keepFlag, sortLongest, skipUnchanged, truncates, truncateLength, colorFlag, lightBg, timeFormat, ignoreInterrupts, messageFieldsFlag, timeFieldsFlag, levelFieldsFlag}

	app.Action = func(c *cli.Context) error {

		opts := humanlog.DefaultOptions
		opts.SortLongest = c.BoolT(sortLongest.Name)
		opts.SkipUnchanged = c.BoolT(skipUnchanged.Name)
		opts.Truncates = c.BoolT(truncates.Name)
		opts.TruncateLength = c.Int(truncateLength.Name)
		opts.LightBg = c.BoolT(lightBg.Name)
		opts.TimeFormat = c.String(timeFormat.Name)
		var err error
		if opts.ColorFlag, err = humanlog.GrokColorMode(c.String(colorFlag.Name)); err != nil {
			fatalf(c, "bad --%s value: %s", colorFlag.Name, err.Error())
		}

		switch {
		case c.IsSet(skipFlag.Name) && c.IsSet(keepFlag.Name):
			fatalf(c, "can only use one of %q and %q", skipFlag.Name, keepFlag.Name)
		case c.IsSet(skipFlag.Name):
			opts.SetSkip(skip)
		case c.IsSet(keepFlag.Name):
			opts.SetKeep(keep)
		}

		if c.IsSet(strings.Split(messageFieldsFlag.Name, ",")[0]) {
			opts.MessageFields = messageFields
		}

		if c.IsSet(strings.Split(timeFieldsFlag.Name, ",")[0]) {
			opts.TimeFields = timeFields
		}

		if c.IsSet(strings.Split(levelFieldsFlag.Name, ",")[0]) {
			opts.LevelFields = levelFields
		}

		if c.IsSet(strings.Split(ignoreInterrupts.Name, ",")[0]) {
			signal.Ignore(os.Interrupt)
		}

		opts.ColorFlag.Apply()

		log.Print("reading stdin...")
		if err := humanlog.Scanner(os.Stdin, colorable.NewColorableStdout(), opts); err != nil {
			log.Fatalf("scanning caught an error: %v", err)
		}
		return nil
	}
	return app
}
