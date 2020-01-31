package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ipfs/testground/pkg/api"
	"github.com/ipfs/testground/pkg/client"
	"github.com/ipfs/testground/pkg/engine"
	"github.com/ipfs/testground/pkg/logging"

	"github.com/BurntSushi/toml"
	"github.com/urfave/cli"
)

var runners = func() []string {
	names := make([]string, 0, len(engine.AllRunners))
	for _, r := range engine.AllRunners {
		names = append(names, r.ID())
	}
	return names
}()

// RunCommand is the specification of the `run` command.
var RunCommand = cli.Command{
	Name:  "run",
	Usage: "(Builds and) runs a test case. List test cases with the `list` command.",
	Subcommands: cli.Commands{
		cli.Command{
			Name:    "composition",
			Aliases: []string{"c"},
			Usage:   "(Builds and) runs a composition.",
			Action:  runCompositionCmd,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "file, f",
					Usage: "path to a composition `FILE`",
				},
				cli.BoolFlag{
					Name:  "write-artifacts, w",
					Usage: "Writes the resulting build artifacts to the composition file.",
				},
				cli.BoolFlag{
					Name:  "ignore-artifacts, i",
					Usage: "Ignores any build artifacts present in the composition file.",
				},
			},
		},
		cli.Command{
			Name:      "single",
			Aliases:   []string{"s"},
			Usage:     "(Builds and) runs a single group.",
			Action:    runSingleCmd,
			ArgsUsage: "[name]",
			Flags: append(
				BuildCommand.Subcommands[1].Flags, // inject all build single command flags.
				cli.GenericFlag{
					Name: "runner, r",
					Value: &EnumValue{
						Allowed: runners,
						Default: "local:exec",
					},
					Usage: fmt.Sprintf("specifies the runner; options: %s", strings.Join(runners, ", ")),
				},
				cli.StringFlag{
					Name:  "use-build, ub",
					Usage: "specifies the artifact to use (from a previous build)",
				},
				cli.UintFlag{
					Name:  "instances, i",
					Usage: "number of instances of the test case to run",
				},
				cli.StringSliceFlag{
					Name:  "run-cfg",
					Usage: "override runner configuration",
				},
				cli.StringSliceFlag{
					Name:  "test-param, p",
					Usage: "provide a test parameter",
				},
			),
		},
	},
}

func runCompositionCmd(c *cli.Context) (err error) {
	comp := new(api.Composition)
	file := c.String("file")
	if file == "" {
		return fmt.Errorf("no composition file supplied")
	}

	if _, err = toml.DecodeFile(file, comp); err != nil {
		return fmt.Errorf("failed to process composition file: %w", err)
	}

	if err = comp.Validate(); err != nil {
		return fmt.Errorf("invalid composition file: %w", err)
	}

	err = doRun(c, comp)
	if err != nil {
		return err
	}

	if c.Bool("write-artifacts") {
		f, err := os.OpenFile(file, os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to write composition to file: %w", err)
		}
		enc := toml.NewEncoder(f)
		if err := enc.Encode(comp); err != nil {
			return fmt.Errorf("failed to encode composition into file: %w", err)
		}
	}

	return nil
}

func runSingleCmd(c *cli.Context) (err error) {
	var comp *api.Composition
	if comp, err = createSingletonComposition(c); err != nil {
		return err
	}
	return doRun(c, comp)
}

func doRun(c *cli.Context, comp *api.Composition) (err error) {
	cl, err := setupClient(c)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ProcessContext())
	defer cancel()

	// Check if we have any groups without an build artifact; if so, trigger a
	// build for those.
	var buildIdx []int
	ignore := c.Bool("ignore-artifacts")
	for i, grp := range comp.Groups {
		if grp.Run.Artifact == "" || ignore {
			buildIdx = append(buildIdx, i)
		}
	}

	if len(buildIdx) > 0 {
		bcomp, err := comp.PickGroups(buildIdx...)
		if err != nil {
			return err
		}

		bout, err := doBuild(c, &bcomp)
		if err != nil {
			return err
		}

		// Populate the returned build IDs.
		for i, groupIdx := range buildIdx {
			g := &comp.Groups[groupIdx]
			logging.S().Infow("generated build artifact", "group", g.ID, "artifact", bout[i].ArtifactPath)
			g.Run.Artifact = bout[i].ArtifactPath
		}
	}

	req := &client.RunRequest{
		Composition: *comp,
	}

	resp, err := cl.Run(ctx, req)
	switch err {
	case nil:
		// noop
	case context.Canceled:
		return fmt.Errorf("interrupted")
	default:
		return fmt.Errorf("fatal error from daemon: %w", err)
	}

	defer resp.Close()

	return client.ParseRunResponse(resp)
}
