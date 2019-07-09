# Network

- [How does a farmer configure a node as exit node](How-does-a-farmer-configure-a-node-as-exit-node)
- [How to create a user private network](#How-to-create-a-user-private-network)

## How does a farmer configure a node as exit node

For the network of the grid to work properly, some of the node in the grid needs to be configured as "exit nodes".  An "exit node" is a node that has a publicly accessible IP address and that is responsible to proxy the traffic from the inside of the grid to the outside internet.

A farmer that wants to configure one of his node as "exit node", needs to register it in the TNODB. The node will then automatically detect it has been configure to be an exit node and do the necessary network configuration to start acting as one.

At the current state of the development, we have a [TNODB mock](../../tools/tnodb_mock) server and a [tffarmer CLI](../../tools/tffarm) tool that can be used to do these configuration.

Here is an example of how a farmer could register one of his node as "exit node":

1. Farmer needs to create its farm identity

```bash
tffarmer register --seed myfarm.seed "mytestfarm"
Farm registered successfully
Name: mytestfarm
Identity: ZF6jtCblLhTgAqp2jvxKkOxBgSSIlrRh1mRGiZaRr7E=
```

2. Boot your nodes with your farm identity specified in the kernel parameters.

Take that farm identity create at step 1 and boot your node with the kernel parameters `farmer_id=<identity>`

for you test farm that would be `farmer_id=ZF6jtCblLhTgAqp2jvxKkOxBgSSIlrRh1mRGiZaRr7E=`

Once the node is booted, it will automatically register itself as being part of your farm into the [TNODB](../../tools/tnodb_mock) server.

You can verify that you node registered itself properly by listing all the node from the TNODB by doing a GET request on the `/nodes` endpoints:

```bash
curl http://tnodb_addr/nodes
[{"node_id":"kV3u7GJKWA7Js32LmNA5+G3A0WWnUG9h+5gnL6kr6lA=","farm_id":"ZF6jtCblLhTgAqp2jvxKkOxBgSSIlrRh1mRGiZaRr7E=","Ifaces":[]}]
```

3. Farmer needs to specify its public allocation range to the TNODB

```bash
tffarmer give-alloc 2a02:2788:0000::/32 --seed myfarm.seed
prefix registered successfully
```

4. Configure your node to be an exit node

In this step the farmer will tell his node how it needs to connect to the public internet. This configuration depends on the farm network setup, this is why this is up to the farmer to provide the detail on how the node needs to configure itself.

```bash
tffarmer configure-public --ip 172.20.0.2/24 --gw 172.20.0.1 --iface eth1 --node kV3u7GJKWA7Js32LmNA5+G3A0WWnUG9h+5gnL6kr6lA=
exit node configured
```

The node is now configured to be used as an exit node.


## How to create a user private network

The only thing a user needs to do before creating a new private network is to select a farm with an exit node. Then he needs to do a request to the TNODB for a new network. The request is a POST request to the `/networks` endpoint of the TNODB with the body of the request containing the identity of the chosen exit farm.

```json
{"exit_farm": "ZF6jtCblLhTgAqp2jvxKkOxBgSSIlrRh1mRGiZaRr7E="}
```

The response body will contain a [network objet](https://github.com/threefoldtech/zosv2/blob/09de5a396bf60b794d2930ced1079a38bd5a9724/modules/network.go#L63). The network objet has an identifier, the network ID. The user can now use this network ID when he wants to provision some container on the grid.