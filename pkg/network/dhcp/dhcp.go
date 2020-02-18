package dhcp

import (
	"context"
	"os/exec"

	"github.com/rs/zerolog/log"
)

// Probe is used to do some DHCP request on a interface
type Probe struct {
	ctx context.Context
	cmd *exec.Cmd
}

// NewPrope returns a Probe
func NewPrope(ctx context.Context) *Probe {
	return &Probe{
		ctx: ctx,
	}
}

// Start starts the DHCP client process
func (d *Probe) Start(inf string) error {

	d.cmd = exec.Command("udhcpc",
		"-f", //foreground
		"-i", inf,
		"-t", "3", //try 3 times before giving up
		"-A", "3", //wait 3 seconds between each trial
		"-s", "/usr/share/udhcp/simple.script",
		"--now", // exit if lease is not obtained
	)

	if err := d.cmd.Start(); err != nil {
		return err
	}

	return nil
}

// Stop kills the DHCP client process
func (d *Probe) Stop() error {
	if d.cmd.ProcessState != nil && d.cmd.ProcessState.Exited() {
		return nil
	}

	if err := d.cmd.Process.Kill(); err != nil {
		log.Error().Err(err).Msg("could not kill proper")
		return err
	}

	_ = d.cmd.Wait()

	return nil
}
