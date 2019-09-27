package main

import (
	"fmt"
	"net"

	"github.com/threefoldtech/zosv2/modules"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli"
)

func giveAlloc(c *cli.Context) error {

	farmID, err := loadFarmID(c.String("seed"))
	if err != nil {
		log.Error().Err(err).Msg("impossible to load farm id, user register command first")
		return err
	}

	alloc := c.Args().First()
	_, allocation, err := net.ParseCIDR(alloc)
	if err != nil {
		log.Error().Err(err).Msg("prefix format not valid, use ip/mask")
		return err
	}

	if err := db.RegisterAllocation(farmID, allocation); err != nil {
		log.Error().Err(err).Msg("failed to register prefix")
		return err
	}

	fmt.Println("prefix registered successfully")
	return nil
}

func configPublic(c *cli.Context) error {
	var (
		ips   []*net.IPNet
		gws   []net.IP
		iface = c.String("iface")
	)

	for _, ip := range c.StringSlice("ip") {
		i, ipnet, err := net.ParseCIDR(ip)
		if err != nil {
			return err
		}
		ipnet.IP = i
		ips = append(ips, ipnet)
	}

	for _, gw := range c.StringSlice("gw") {
		gws = append(gws, net.ParseIP(gw))
	}

	node := c.Args().First()

	if err := db.ConfigurePublicIface(modules.StrIdentifier(node), ips, gws, iface); err != nil {
		return err
	}
	fmt.Printf("public interface configured on node %s\n", node)
	return nil
}

func selectExit(c *cli.Context) error {
	node := c.Args().First()

	if err := db.SelectExitNode(modules.StrIdentifier(node)); err != nil {
		return err
	}
	fmt.Printf("Node %s marked as exit node\n", node)
	return nil
}
