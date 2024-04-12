package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	gonanoid "github.com/matoous/go-nanoid"
	"golang.org/x/exp/rand"

	"github.com/humanlogio/humanlog"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/urfave/cli"

	"gonum.org/v1/gonum/stat/distuv"
)

// note: all the randomness clamping with % leads to not
// really random and it's all not correct and it's not meant
// to be, this is just written real fast to get it going. feel
// free to make this better and fancier!

const (
	gennyCmdName = "genny"
)

func gennyCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
) cli.Command {

	seedFlag := cli.Uint64Flag{
		Name:  "seed",
		Value: uint64(time.Now().UnixNano()),
	}
	startAtFlag := cli.StringFlag{
		Name:  "start_at",
		Value: time.Now().Format(time.RFC3339),
	}
	averagePerInternalFlag := cli.Float64Flag{
		Name:  "logs_per_s",
		Value: 50,
	}

	return cli.Command{
		Name:   gennyCmdName,
		Usage:  "Generate fake logs",
		Hidden: true,
		Flags: []cli.Flag{
			seedFlag,
			startAtFlag,
			averagePerInternalFlag,
		},

		Action: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			seed := cctx.Uint64(seedFlag.Name)
			start, err := time.Parse(time.RFC3339, cctx.String(startAtFlag.Name))
			if err != nil {
				return fmt.Errorf("invalid start: %v", err)
			}
			averagePerInternal := cctx.Float64(averagePerInternalFlag.Name)
			return genny(ctx, seed, start, time.Second, averagePerInternal, os.Stdout)
		},
	}
}

func genny(
	ctx context.Context,
	seed uint64,
	start time.Time,
	interval time.Duration,
	averagePerInterval float64,
	out io.Writer,
) error {
	src := rand.NewSource(seed)
	arrivalRateDist := distuv.Poisson{
		Src:    src,
		Lambda: averagePerInterval,
	}

	now := start
	for {
		if err := emitMessage(out, now, src); err != nil {
			return err
		}
		nextArrival := arrivalRateDist.Rand()
		nextMsgIn := time.Duration(float64(interval) / nextArrival)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(nextMsgIn):
			now = now.Add(nextMsgIn)
		}
	}
}

func emitMessage(out io.Writer, now time.Time, src rand.Source) error {
	t := ts(now, src)
	l := lvl(src)
	m := msg(src)
	k := kvs(src)
	_, err := fmt.Fprintln(out, t+l+m+k)
	return err
}

var opts = humanlog.DefaultOptions()

func ts(now time.Time, src rand.Source) string {
	if src.Uint64()%20 == 0 { // 1/20 times, no timestamp
		return ""
	}
	key := randel(src, opts.TimeFields)
	format := randel(src, humanlog.TimeFormats)
	return key + "=" + now.Format(format)
}

func kvs(src rand.Source) string {
	keyCount := src.Uint64() % 20
	if keyCount > 20 {
		panic(keyCount)
	}
	if keyCount == 0 {
		return ""
	}
	buf := strings.Builder{}
	for range keyCount {
		buf.WriteString(" ")
		buf.WriteString(genKey(src))
		buf.WriteString("=")
		buf.WriteString(genVal(src))
	}
	return buf.String()
}

func genKey(src rand.Source) string {
	i := src.Uint64()
	dice := int(i % 100)
	switch {
	case dice >= 0 && dice < 4:
		return []string{
			"request_id",
			"trace_id",
			"RequestID",
			"req.id",
		}[dice]
	case dice >= 4 && dice < 6:
		return "user"
	case dice >= 6 && dice < 8:
		return "org"
	case dice >= 8 && dice < 12:
		keys := []string{
			"index",
			"project",
			"car",
			"idk",
		}
		return keys[dice%len(keys)]
	default:
		return genString(src, false)
	}
}

var bases = []int{2, 8, 10, 16}
var fmtbytes = []byte{'b', 'e', 'E', 'f', 'g', 'G', 'x', 'X'}
var bools = []string{"true", "True", "false", "False"}
var bitsizes = []int{32, 64}

func genVal(src rand.Source) string {
	i := src.Uint64()
	switch i % 4 {
	case 0:
		base := randel(src, bases)
		return strconv.FormatUint(i, base)
	case 1:
		f := (distuv.Normal{
			Mu:  float64(i),
			Src: src,
		}).Rand()
		fmt := randel(src, fmtbytes)
		prec := int(src.Uint64()) % 10
		if prec == 0 {
			prec = -1
		}
		bitsize := randel(src, bitsizes)
		return strconv.FormatFloat(f,
			fmt,
			prec,
			bitsize,
		)
	case 2:
		return randel(src, bools)
	case 3:
		return genString(src, true)
	}
	panic("missing case")
}

func randel[T any](src rand.Source, sl []T) T {
	i := src.Uint64() % uint64(len(sl))
	return sl[i]
}

func lvl(src rand.Source) string {
	key := " " + randel(src, opts.LevelFields)
	switch src.Uint64() % 5 {
	case 0:
		return key + "=DEBUG"
	case 1:
		return key + "=INFO"
	case 2:
		return key + "=WARN"
	case 3:
		return key + "=ERROR"
	case 4:
		return ""
	}
	panic("missing case")
}

func msg(src rand.Source) string {
	words := (src.Uint64() % 10)
	if words > 10 {
		panic(words)
	}
	if words == 0 {
		return ""
	}
	key := " " + randel(src, opts.MessageFields) + "="
	buf := strings.Builder{}
	buf.WriteString(genString(src, false))
	for range words {
		buf.WriteRune(' ')
		buf.WriteString(genString(src, false))
	}
	return key + strconv.Quote(buf.String())
}

func genString(src rand.Source, genIDs bool) string {
	if !genIDs {
		switch i := src.Uint64() % 3; i {
		case 0, 1:
			return randel(src, nouns)
		case 2:
			return randel(src, adjectives)
		}
	}
	switch i := src.Uint64() % 4; i {
	case 0, 1:
		return randel(src, nouns)
	case 2:
		return randel(src, adjectives)
	case 3:
		return uuid.NewString()
	case 4:
		return gonanoid.MustID(int(src.Uint64() % 20))
	default:
		panic(i)
	}
}

var adjectives = []string{
	"aged",
	"ancient",
	"billowing",
	"black",
	"blue",
	"cold",
	"cool",
	"crimson",
	"damp",
	"dawn",
	"delicate",
	"divine",
	"falling",
	"floral",
	"fragrant",
	"frosty",
	"green",
	"holy",
	"late",
	"lingering",
	"little",
	"lively",
	"long",
	"morning",
	"muddy",
	"nameless",
	"old",
	"patient",
	"polished",
	"proud",
	"purple",
	"quiet",
	"red",
	"rough",
	"shy",
	"small",
	"snowy",
	"solitary",
	"spring",
	"still",
	"throbbing",
	"wandering",
	"weathered",
	"white",
	"wild",
	"winter",
	"wispy",
	"withered",
	"bold",
	"broken",
	"icy",
	"restless",
	"sparkling",
	"twilight",
	"young",
	"bitter",
	"dark",
	"dry",
	"empty",
	"hidden",
	"misty",
	"silent",
	"summer",
	"autumn",
}

var nouns = []string{
	"bird",
	"breeze",
	"brook",
	"bush",
	"butterfly",
	"cherry",
	"cloud",
	"darkness",
	"dawn",
	"dew",
	"dream",
	"dust",
	"feather",
	"field",
	"fire",
	"firefly",
	"flower",
	"fog",
	"forest",
	"frog",
	"frost",
	"glade",
	"glitter",
	"grass",
	"haze",
	"hill",
	"lake",
	"leaf",
	"meadow",
	"moon",
	"morning",
	"mountain",
	"night",
	"paper",
	"pine",
	"pond",
	"rain",
	"resonance",
	"river",
	"sea",
	"shadow",
	"shape",
	"silence",
	"sky",
	"smoke",
	"snow",
	"snowflake",
	"sound",
	"star",
	"sun",
	"sun",
	"sunset",
	"surf",
	"thunder",
	"tree",
	"violet",
	"voice",
	"water",
	"water",
	"waterfall",
	"wave",
	"wildflower",
	"wind",
	"wood",
}
