package gedis

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"

	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/zosv2/modules/geoip"

	"github.com/pkg/errors"
	"github.com/threefoldtech/zosv2/modules/gedis/types/directory"
	"github.com/threefoldtech/zosv2/modules/network"
	"github.com/threefoldtech/zosv2/modules/network/types"

	"github.com/threefoldtech/zosv2/modules"
)

//
// IDStore Interface
//

//RegisterNode implements modules.IdentityManager interface
func (g *Gedis) RegisterNode(nodeID, farmID modules.Identifier, version string) (string, error) {

	l, err := geoip.Fetch()
	if err != nil {
		log.Error().Err(err).Msg("failed to get location of the node")
	}

	resp, err := Bytes(g.Send("nodes", "add", Args{
		"node": directory.TfgridNode2{
			NodeId:       nodeID.Identity(),
			FarmId:       farmID.Identity(),
			OsVersion:    version,
			PublicKeyHex: nodeID.Hex(),
			Location: directory.TfgridLocation1{
				Longitude: l.Longitute,
				Latitude:  l.Latitude,
				Continent: l.Continent,
				Country:   l.Country,
				City:      l.City,
			},
		},
	}))

	if err != nil {
		return "", parseError(err)
	}

	var out struct {
		Node directory.TfgridNode2 `json:"node"`
	}

	if err := json.Unmarshal(resp, &out); err != nil {
		return "", err
	}

	// no need to do data conversion here, returns the id
	return out.Node.NodeId, nil
}

// ListNode implements modules.IdentityManager interface
func (g *Gedis) ListNode(farmID modules.Identifier, country string, city string) ([]types.Node, error) {
	resp, err := Bytes(g.Send("nodes", "list", Args{
		"farm_id": farmID.Identity(),
		"country": country,
		"city":    city,
	}))

	if err != nil {
		return nil, err
	}

	var out struct {
		Nodes []directory.TfgridNode2 `json:"nodes"`
	}

	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, err
	}

	var result []types.Node
	for _, node := range out.Nodes {
		result = append(result, nodeFromSchema(node))
	}

	return result, nil
}

//RegisterFarm implements modules.IdentityManager interface
func (g *Gedis) RegisterFarm(farm modules.Identifier, name string, email string, wallet []string) (string, error) {
	resp, err := Bytes(g.Send("farms", "register", Args{
		"farm": directory.TfgridFarm1{
			ThreebotId:      farm.Identity(),
			Name:            name,
			Email:           email,
			WalletAddresses: wallet,
		},
	}))

	if err != nil {
		return "", err
	}

	var out struct {
		FarmID json.Number `json:"farm_id"`
	}

	if err := json.Unmarshal(resp, &out); err != nil {
		return "", err
	}

	return out.FarmID.String(), nil
}

//GetNode implements modules.IdentityManager interface
func (g *Gedis) GetNode(nodeID modules.Identifier) (*types.Node, error) {
	resp, err := Bytes(g.Send("nodes", "get", Args{
		"node_id": nodeID.Identity(),
	}))

	if err != nil {
		return nil, err
	}

	var out directory.TfgridNode2

	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, err
	}

	node := nodeFromSchema(out)
	return &node, nil
}

func infFromSchema(inf directory.TfgridNodeIface1) types.IfaceInfo {
	return types.IfaceInfo{
		Name:    inf.Name,
		Gateway: inf.Gateway,
		Addrs: func() []*net.IPNet {
			var r []*net.IPNet
			for _, addr := range inf.Addrs {
				r = append(r, &addr.IPNet)
			}
			return r
		}(),
	}
}

func nodeFromSchema(node directory.TfgridNode2) types.Node {
	return types.Node{
		NodeID: node.NodeId,
		FarmID: node.FarmId,
		Ifaces: func() []*types.IfaceInfo {
			var r []*types.IfaceInfo
			for _, iface := range node.Ifaces {
				v := infFromSchema(iface)
				r = append(r, &v)
			}
			return r
		}(),
		ExitNode: func() int {
			if node.ExitNode {
				return 1
			}
			return 0
		}(),
	}
}

func farmFromSchema(farm directory.TfgridFarm1) network.Farm {
	return network.Farm{
		ID:   fmt.Sprint(farm.ID),
		Name: farm.Name,
	}
}

func (g *Gedis) updateGenericNodeCapacity(captype string, node modules.Identifier, mru, cru, hru, sru uint64) error {
	_, err := g.Send("nodes", "update_"+captype+"_capacity", Args{
		"node_id": node.Identity(),
		"resource": directory.TfgridNodeResourceAmount1{
			Cru: int64(cru),
			Mru: int64(mru),
			Hru: int64(hru),
			Sru: int64(sru),
		},
	})

	return err
}

//UpdateTotalNodeCapacity implements modules.IdentityManager interface
func (g *Gedis) UpdateTotalNodeCapacity(node modules.Identifier, mru, cru, hru, sru uint64) error {
	return g.updateGenericNodeCapacity("total", node, mru, cru, hru, sru)
}

//UpdateReservedNodeCapacity implements modules.IdentityManager interface
func (g *Gedis) UpdateReservedNodeCapacity(node modules.Identifier, mru, cru, hru, sru uint64) error {
	return g.updateGenericNodeCapacity("reserved", node, mru, cru, hru, sru)
}

//UpdateUsedNodeCapacity implements modules.IdentityManager interface
func (g *Gedis) UpdateUsedNodeCapacity(node modules.Identifier, mru, cru, hru, sru uint64) error {
	return g.updateGenericNodeCapacity("used", node, mru, cru, hru, sru)
}

//GetFarm implements modules.IdentityManager interface
func (g *Gedis) GetFarm(farm string) (*network.Farm, error) {
	id, err := strconv.ParseInt(farm, 10, 64)
	if err != nil {
		return nil, errors.Wrap(err, "invalid farm id")
	}

	resp, err := Bytes(g.Send("farms", "get", Args{
		"farm_id": id,
	}))

	if err != nil {
		return nil, err
	}

	var out directory.TfgridFarm1

	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, err
	}
	f := farmFromSchema(out)
	return &f, nil
}

// //UpdateFarm implements modules.IdentityManager interface
// func (g *Gedis) UpdateFarm(farm modules.Identifier) error {
// 	req := gedisUpdateFarmBody{
// 		FarmID: farm.Identity(),
// 		Farm: tfgridFarm1{
// 			ThreebotID:      "",
// 			IyoOrganization: "",
// 			Name:            "",
// 			WalletAddresses: []string{},
// 			// Location: {},
// 			Vta: "",
// 			// ResourcePrice: {},
// 		},
// 	}

// 	b, err := json.Marshal(req)
// 	if err != nil {
// 		return err
// 	}

// 	_, err = g.sendCommand("farms", "update", b)
// 	if err != nil {
// 		return parseError(err)
// 	}

// 	return nil
// }

//ListFarm implements modules.IdentityManager interface
func (g *Gedis) ListFarm(country string, city string) ([]network.Farm, error) {
	resp, err := Bytes(g.Send("farms", "list", Args{
		"country": country,
		"city":    city,
	}))

	if err != nil {
		return nil, err
	}

	var out struct {
		Farms []directory.TfgridFarm1 `json:"farms"`
	}

	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, err
	}

	var result []network.Farm
	for _, farm := range out.Farms {
		result = append(result, farmFromSchema(farm))
	}

	return result, nil
}
