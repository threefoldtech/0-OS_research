package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path"
	"time"

	"github.com/cenkalti/backoff/v3"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/threefoldtech/zos/pkg"
	"github.com/threefoldtech/zos/pkg/container/logger"
	"github.com/threefoldtech/zos/pkg/gridtypes"
	"github.com/threefoldtech/zos/pkg/provision"
	"github.com/threefoldtech/zos/pkg/stubs"
)

type Container = gridtypes.Container
type ContainerResult = gridtypes.ContainerResult

func (p *Primitives) containerProvision(ctx context.Context, wl *gridtypes.Workload) (interface{}, error) {
	return p.containerProvisionImpl(ctx, wl)
}

// ContainerProvision is entry point to container reservation
func (p *Primitives) containerProvisionImpl(ctx context.Context, wl *gridtypes.Workload) (ContainerResult, error) {
	var (
		containerClient = stubs.NewContainerModuleStub(p.zbus)
		flistClient     = stubs.NewFlisterStub(p.zbus)
		storageClient   = stubs.NewStorageModuleStub(p.zbus)
		networkMgr      = stubs.NewNetworkerStub(p.zbus)
		tenantNS        = fmt.Sprintf("ns%s", wl.User)
		containerID     = wl.ID
	)

	var config Container
	if err := json.Unmarshal(wl.Data, &config); err != nil {
		return ContainerResult{}, err
	}

	// check if workload is already deployed
	_, err := containerClient.Inspect(tenantNS, pkg.ContainerID(containerID))
	if err == nil {
		log.Info().Stringer("id", containerID).Msg("container already deployed")
		return ContainerResult{
			ID:   string(containerID),
			IPv4: config.Network.IPs[0].String(),
		}, nil
	}

	if err := validateContainerConfig(config); err != nil {
		return ContainerResult{}, errors.Wrap(err, "container provision schema not valid")
	}

	netID := gridtypes.NetworkID(wl.User.String(), string(config.Network.NetworkID))
	log.Debug().
		Str("network-id", string(netID)).
		Str("config", fmt.Sprintf("%+v", config)).
		Msg("deploying network")

		// check to make sure the network is already installed on the node
	if _, err := networkMgr.GetSubnet(netID); err != nil {
		return ContainerResult{}, fmt.Errorf("network %s is not installed on this node", config.Network.NetworkID)
	}

	cache := provision.GetCache(ctx)
	// check to make sure the requested volume are accessible
	for _, mount := range config.Mounts {
		volumeRes, err := cache.Get(mount.VolumeID)
		if err != nil {
			return ContainerResult{}, errors.Wrapf(err, "failed to retrieve the owner of volume %s", mount.VolumeID)
		}

		if volumeRes.User != wl.User.String() {
			return ContainerResult{}, fmt.Errorf("cannot use volume %s, user %s is not the owner of it", mount.VolumeID, wl.User)
		}
	}

	// ensure we can decrypt all environment variables
	var env []string
	for k, v := range config.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	for k, v := range config.SecretEnv {
		v, err := decryptSecret(v, wl.User.String(), wl.Version, p.zbus)
		if err != nil {
			return ContainerResult{}, errors.Wrapf(err, "failed to decrypt secret env var '%s'", k)
		}
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	var logs []logger.Logs
	for _, log := range config.Logs {
		stdout := log.Data.Stdout
		stderr := log.Data.Stderr

		if len(log.Data.SecretStdout) > 0 {
			stdout, err = decryptSecret(log.Data.SecretStdout, wl.User.String(), wl.Version, p.zbus)
			if err != nil {
				return ContainerResult{}, errors.Wrap(err, "failed to decrypt log.secret_stdout var")
			}
		}

		if len(log.Data.SecretStderr) > 0 {
			stderr, err = decryptSecret(log.Data.SecretStderr, wl.User.String(), wl.Version, p.zbus)
			if err != nil {
				return ContainerResult{}, errors.Wrap(err, "failed to decrypt log.secret_stdout var")
			}
		}
		logs = append(logs, logger.Logs{
			Type: log.Type,
			Data: logger.LogsRedis{
				Stdout: stdout,
				Stderr: stderr,
			},
		})
	}

	// prepare container network
	ips := make([]string, len(config.Network.IPs))
	for i, ip := range config.Network.IPs {
		ips[i] = ip.String()
	}
	var join pkg.Member
	join, err = networkMgr.Join(netID, containerID.String(), pkg.ContainerNetworkConfig{
		IPs:         ips,
		PublicIP6:   config.Network.PublicIP6,
		YggdrasilIP: config.Network.YggdrasilIP,
	})
	if err != nil {
		return ContainerResult{}, err
	}
	log.Info().
		Str("ipv6", join.IPv6.String()).
		Str("ygg", join.YggdrasilIP.String()).
		Str("ipv4", join.IPv4.String()).
		Stringer("container", wl.ID).
		Msg("assigned an IP")

	defer func() {
		if err != nil {
			if err := networkMgr.Leave(netID, containerID.String()); err != nil {
				log.Error().Err(err).Msgf("failed leave container network namespace")
			}
		}
	}()

	// mount root flist
	log.Debug().Str("flist", config.FList).Msg("mounting flist")
	rootfsMntOpt := pkg.MountOptions{
		Limit:    config.Capacity.DiskSize,
		ReadOnly: false,
		Type:     config.Capacity.DiskType,
	}
	if rootfsMntOpt.Limit == 0 || rootfsMntOpt.Type == "" {
		rootfsMntOpt = pkg.DefaultMountOptions
	}

	var mnt string
	mnt, err = flistClient.NamedMount(FilesystemName(wl), config.FList, config.FlistStorage, rootfsMntOpt)
	if err != nil {
		return ContainerResult{}, err
	}

	// prepare mount info for volumes
	var mounts []pkg.MountInfo
	for _, mount := range config.Mounts {
		// we make sure that mountpoint in config doesn't have relative parts
		mountpoint := path.Join("/", mount.Mountpoint)

		if err := os.MkdirAll(path.Join(mnt, mountpoint), 0755); err != nil {
			return ContainerResult{}, err
		}
		var source pkg.Filesystem
		source, err = storageClient.Path(mount.VolumeID)
		if err != nil {
			return ContainerResult{}, errors.Wrapf(err, "failed to get the mountpoint path of the volume %s", mount.VolumeID)
		}

		mounts = append(
			mounts,
			pkg.MountInfo{
				Source: source.Path,
				Target: mountpoint,
			},
		)
	}

	defer func() {
		if err != nil {
			if err := containerClient.Delete(tenantNS, pkg.ContainerID(containerID)); err != nil {
				log.Error().Err(err).Stringer("container_id", containerID).Msg("error during delete of container")
			}

			if err := flistClient.Umount(mnt); err != nil {
				log.Error().Err(err).Str("path", mnt).Msgf("failed to unmount")
			}
		}
	}()

	var id pkg.ContainerID
	id, err = containerClient.Run(
		tenantNS,
		pkg.Container{
			Name:   containerID.String(),
			RootFS: mnt,
			Env:    env,
			Network: pkg.NetworkInfo{
				Namespace: join.Namespace,
			},
			Mounts:      mounts,
			Entrypoint:  config.Entrypoint,
			Interactive: config.Interactive,
			CPU:         config.Capacity.CPU,
			Memory:      config.Capacity.Memory * mib,
			Logs:        logs,
			Stats:       config.Stats,
		},
	)
	if err != nil {
		return ContainerResult{}, errors.Wrap(err, "error starting container")
	}

	if config.Network.PublicIP6 {
		ip, err := p.waitContainerIP(ctx, "pub", join.Namespace)
		if err != nil {
			return ContainerResult{}, errors.Wrap(err, "error reading container ipv6")
		}
		if len(ips) <= 0 {
			return ContainerResult{}, fmt.Errorf("no ipv6 found for container %s", id)
		}
		join.IPv6 = ip
	}

	log.Info().Msgf("container created with id: '%s'", id)
	return ContainerResult{
		ID:    string(id),
		IPv6:  join.IPv6.String(),
		IPv4:  join.IPv4.String(),
		IPYgg: join.YggdrasilIP.String(),
	}, nil
}

func (p *Primitives) containerDecommission(ctx context.Context, wl *gridtypes.Workload) error {
	container := stubs.NewContainerModuleStub(p.zbus)
	flist := stubs.NewFlisterStub(p.zbus)
	networkMgr := stubs.NewNetworkerStub(p.zbus)

	tenantNS := fmt.Sprintf("ns%s", wl.User)
	containerID := pkg.ContainerID(wl.ID)

	var config Container
	if err := json.Unmarshal(wl.Data, &config); err != nil {
		return err
	}

	info, err := container.Inspect(tenantNS, containerID)
	if err == nil {
		if err := container.Delete(tenantNS, containerID); err != nil {
			return errors.Wrapf(err, "failed to delete container %s", containerID)
		}

		rootFS := info.RootFS
		if info.Interactive {
			rootFS, err = findRootFS(info.Mounts)
			if err != nil {
				return err
			}
		}

		if err := flist.Umount(rootFS); err != nil {
			return errors.Wrapf(err, "failed to unmount flist at %s", rootFS)
		}

	} else {
		log.Error().Err(err).Str("container", string(containerID)).Msg("failed to inspect container for decomission")
	}

	netID := NetworkID(wl.User.String(), string(config.Network.NetworkID))
	if _, err := networkMgr.GetSubnet(netID); err == nil { // simple check to make sure the network still exists on the node
		if err := networkMgr.Leave(netID, string(containerID)); err != nil {
			return errors.Wrap(err, "failed to delete container network namespace")
		}
	}

	return nil
}

func (p *Primitives) waitContainerIP(ctx context.Context, ifaceName, namespace string) (net.IP, error) {
	var (
		network     = stubs.NewNetworkerStub(p.zbus)
		containerIP net.IP
	)

	getIP := func() error {

		ips, err := network.Addrs(ifaceName, namespace)
		if err != nil {
			log.Debug().Err(err).Msg("not ip public found, waiting")
			return err
		}

		for _, ip := range ips {
			if isPublic(ip) {
				containerIP = ip
				return nil
			}
		}

		return fmt.Errorf("waiting for more addresses")
	}

	notify := func(err error, d time.Duration) {
		log.Debug().Err(err).Str("duration", d.String()).Msg("failed to get zdb public IP")
	}

	bo := backoff.NewExponentialBackOff()
	bo.MaxInterval = time.Second * 20
	bo.MaxElapsedTime = time.Minute * 2

	if err := backoff.RetryNotify(getIP, bo, notify); err != nil {
		return nil, errors.Wrapf(err, "failed to get an IP for interface %s", ifaceName)
	}

	return containerIP, nil
}

func validateContainerConfig(config Container) error {
	if config.Network.NetworkID == "" {
		return fmt.Errorf("network ID cannot be empty")
	}

	if len(config.Network.IPs) == 0 {
		return fmt.Errorf("missing container IP address")
	}

	if config.FList == "" {
		return fmt.Errorf("missing flist url")
	}

	if config.Capacity.Memory < 1024 {
		return fmt.Errorf("amount of memory allocated for the container cannot be lower then 1024 megabytes")
	}

	if config.Capacity.CPU == 0 {
		return fmt.Errorf("cannot create a container with 0 CPU allocated")
	}

	return nil
}

func findRootFS(mounts []pkg.MountInfo) (string, error) {
	for _, m := range mounts {
		if m.Target == "/sandbox" {
			return m.Source, nil
		}
	}

	return "", fmt.Errorf("rootfs flist mountpoint not found")
}
