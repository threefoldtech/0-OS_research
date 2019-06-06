package main

import (
	"github.com/threefoldtech/zbus"
	"github.com/threefoldtech/zosv2/modules"
	"github.com/threefoldtech/zosv2/modules/stubs"
)

func main() {
	client, err := zbus.NewRedisClient("tcp://localhost:6379")
	if err != nil {
		panic(client)
	}

	containerd := stubs.NewContainerModuleStub(client)
	namespace := "example"

	// make sure u have a network namespace ready using ip
	// sudo ip netns add mynetns

	info := modules.Container{
		Name: "test",
		FList: "https://hub.grid.tf/thabet/redis.flist",
		Env: []string{},
		Network: modules.NetworkInfo{Namespace: "mynetns"},
		Mounts: nil,
		Entrypoint: "redis-server",
	}

	id, err := containerd.Run(namespace, info)

	if err != nil {
		panic(err)
	}

	// DO WORK WITH CONTAINER ...

	if err = containerd.Delete(namespace, id); err != nil {
		panic(err)
	}

}
