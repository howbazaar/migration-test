// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"
	"gopkg.in/yaml.v2"
	"launchpad.net/gnuflag"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/mongo"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/migration"
)

var logger = loggo.GetLogger("juju")

func main() {
	ctx, err := cmd.DefaultContext()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	os.Exit(cmd.Main(&MigrateCommand{}, ctx, os.Args[1:]))
}

type MigrateCommand struct {
	cmd.CommandBase

	operation  string
	dataDir    string
	modelUUID  string
	machineId  string
	filename   string
	machineTag names.MachineTag
}

func (c *MigrateCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "migration-test",
		Args:    "[export <uuid>]|[import yamlfile]",
		Purpose: "run the missing upgrade steps",
	}
}

func (c *MigrateCommand) SetFlags(f *gnuflag.FlagSet) {
	f.StringVar(&c.dataDir, "data-dir", "/var/lib/juju", "directory for juju data")
	f.StringVar(&c.machineId, "machine", "0", "id of the machine you are on")
}

func (c *MigrateCommand) Init(args []string) error {
	if len(args) == 0 {
		return errors.New("missing operation")
	}
	c.operation, args = args[0], args[1:]

	switch c.operation {
	case "export":
		if len(args) == 0 {
			return errors.New("missing model uuid")
		}
		c.modelUUID, args = args[0], args[1:]

	case "import":
		if len(args) == 0 {
			return errors.New("missing yaml filename")
		}
		c.filename, args = args[0], args[1:]
	default:
		return errors.Errorf("unknown operation %q", c.operation)
	}

	if !names.IsValidMachine(c.machineId) {
		return errors.Errorf("%q is not a valid machine id", c.machineId)
	}
	c.machineTag = names.NewMachineTag(c.machineId)
	return cmd.CheckEmpty(args)
}

func (c *MigrateCommand) Run(ctx *cmd.Context) error {

	loggo.GetLogger("juju").SetLogLevel(loggo.DEBUG)
	conf, err := agent.ReadConfig(agent.ConfigPath(c.dataDir, c.machineTag))
	if err != nil {
		return err
	}

	info, ok := conf.MongoInfo()
	if !ok {
		return errors.Errorf("no state info available")
	}
	st, err := state.Open(conf.Environment(), info, mongo.DefaultDialOpts(), environs.NewStatePolicy())
	if err != nil {
		return err
	}
	defer st.Close()

	if c.operation == "export" {
		return c.exportModel(ctx, st)
	}

	return c.importModel(ctx, st)

}

func (c *MigrateCommand) exportModel(ctx *cmd.Context, st *state.State) error {
	ctx.Infof("\nexport %s", c.modelUUID)

	// first make sure the uuid is good enough
	tag := names.NewEnvironTag(c.modelUUID)
	_, err := st.GetEnvironment(tag)
	if err != nil {
		return errors.Trace(err)
	}

	modelState, err := st.ForEnviron(tag)
	if err != nil {
		return errors.Trace(err)
	}
	defer modelState.Close()

	model, err := modelState.Export()
	if err != nil {
		return errors.Trace(err)
	}

	bytes, err := yaml.Marshal(model)
	if err != nil {
		return errors.Trace(err)
	}

	ctx.Stdout.Write(bytes)
	return nil
}

func (c *MigrateCommand) importModel(ctx *cmd.Context, st *state.State) error {
	ctx.Infof("\nimport ")

	bytes, err := ioutil.ReadFile(c.filename)
	if err != nil {
		return errors.Trace(err)
	}

	model, err := migration.DeserializeModel(bytes)
	if err != nil {
		return errors.Trace(err)
	}

	env, newSt, err := st.Import(model)
	if err != nil {
		return errors.Trace(err)
	}
	defer newSt.Close()

	ctx.Infof("success, env %s/%s imported", env.Owner().Canonical(), env.Name())

	return nil
}
