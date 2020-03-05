package workloads

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/zos/pkg/schema"
	"github.com/threefoldtech/zos/tools/bcdb_mock/models"
	generated "github.com/threefoldtech/zos/tools/bcdb_mock/models/generated/workloads"
	"github.com/threefoldtech/zos/tools/bcdb_mock/mw"
	phonebook "github.com/threefoldtech/zos/tools/bcdb_mock/pkg/phonebook/types"
	"github.com/threefoldtech/zos/tools/bcdb_mock/pkg/workloads/types"
	"go.mongodb.org/mongo-driver/mongo"
)

// API struct
type API struct{}

func (a *API) create(r *http.Request) (interface{}, mw.Response) {
	defer r.Body.Close()
	var reservation types.Reservation
	if err := json.NewDecoder(r.Body).Decode(&reservation); err != nil {
		return nil, mw.BadRequest(err)
	}

	if reservation.Expired() {
		return nil, mw.BadRequest(fmt.Errorf("creating for a reservation that expires in the past"))
	}

	// we make sure those arrays are initialized correctly
	// this will make updating the document in place much easier
	// in later stages
	reservation.SignaturesProvision = make([]generated.TfgridWorkloadsReservationSigningSignature1, 0)
	reservation.SignaturesDelete = make([]generated.TfgridWorkloadsReservationSigningSignature1, 0)
	reservation.SignaturesFarmer = make([]generated.TfgridWorkloadsReservationSigningSignature1, 0)
	reservation.Results = make([]generated.TfgridWorkloadsReservationResult1, 0)

	reservation, err := a.pipeline(reservation, nil)
	if err != nil {
		// if failed to create pipeline, then
		// this reservation has failed initial validation
		return nil, mw.BadRequest(err)
	}

	if reservation.IsAny(types.Invalid, types.Delete) {
		return nil, mw.BadRequest(fmt.Errorf("invalid request wrong status '%s'", reservation.NextAction.String()))
	}

	db := mw.Database(r)
	var filter phonebook.UserFilter
	filter = filter.WithID(schema.ID(reservation.CustomerTid))
	user, err := filter.Get(r.Context(), db)
	if err != nil {
		return nil, mw.BadRequest(errors.Wrapf(err, "cannot find user with id '%d'", reservation.CustomerTid))
	}

	signature, err := hex.DecodeString(reservation.CustomerSignature)
	if err := reservation.Verify(user.Pubkey, signature); err != nil {
		return nil, mw.BadRequest(errors.Wrap(err, "failed to verify customer signature"))
	}

	reservation.Epoch = schema.Date{Time: time.Now()}

	id, err := types.ReservationCreate(r.Context(), db, reservation)
	if err != nil {
		return nil, mw.Error(err)
	}

	return id, mw.Created()
}

func (a *API) parseID(id string) (schema.ID, error) {
	v, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, errors.Wrap(err, "invalid id format")
	}

	return schema.ID(v), nil
}

func (a *API) pipeline(r types.Reservation, err error) (types.Reservation, error) {
	if err != nil {
		return r, err
	}
	pl, err := types.NewPipeline(r)
	if err != nil {
		return r, errors.Wrap(err, "failed to process reservation state pipeline")
	}

	r, _ = pl.Next()
	return r, nil
}

func (a *API) get(r *http.Request) (interface{}, mw.Response) {
	id, err := a.parseID(mux.Vars(r)["res_id"])
	if err != nil {
		return nil, mw.BadRequest(fmt.Errorf("invalid reservation id"))
	}

	var filter types.ReservationFilter
	filter = filter.WithID(id)

	db := mw.Database(r)
	reservation, err := a.pipeline(filter.Get(r.Context(), db))
	if err != nil {
		return nil, mw.NotFound(err)
	}

	return reservation, nil
}

func (a *API) updateMany(db *mongo.Database, rs []types.Reservation) {
	if len(rs) == 0 {
		return
	}
}

func (a *API) list(r *http.Request) (interface{}, mw.Response) {
	var filter types.ReservationFilter

	db := mw.Database(r)
	cur, err := filter.Find(r.Context(), db, models.PageFromRequest(r))
	if err != nil {
		return nil, mw.Error(err)
	}

	defer cur.Close(r.Context())

	reservations := []types.Reservation{}

	for cur.Next(r.Context()) {
		var reservation types.Reservation
		if err := cur.Decode(&reservation); err != nil {
			return nil, mw.Error(err)
		}

		reservation, err := a.pipeline(reservation, nil)
		if err != nil {
			log.Error().Err(err).Int64("id", int64(reservation.ID)).Msg("failed to process reservation")
			continue
		}

		reservations = append(reservations, reservation)
	}

	return reservations, nil
}

func (a *API) markDelete(r *http.Request) (interface{}, mw.Response) {
	// WARNING: #TODO
	// This method does not validate the signature of the caller
	// because there is no payload in a delete call.
	// may be a simple body that has "reservation id" and "signature"
	// can be used, we use the reservation id to avoid using the same
	// request body to delete other reservations

	// HTTP Delete should not have a body though, so may be this should be
	// changed to a PUT operation.

	id, err := a.parseID(mux.Vars(r)["res_id"])
	if err != nil {
		return nil, mw.Error(err)
	}

	var filter types.ReservationFilter
	filter = filter.WithID(id)
	db := mw.Database(r)
	reservation, err := a.pipeline(filter.Get(r.Context(), db))
	if err != nil {
		return nil, mw.NotFound(err)
	}

	if reservation.NextAction == generated.TfgridWorkloadsReservation1NextActionDeleted ||
		reservation.NextAction == generated.TfgridWorkloadsReservation1NextActionDelete {
		return nil, mw.BadRequest(fmt.Errorf("resource already deleted"))
	}

	if err = types.ReservationSetNextAction(r.Context(), db, id, generated.TfgridWorkloadsReservation1NextActionDelete); err != nil {
		return nil, mw.Error(err)
	}

	return nil, nil
}

func (a *API) workloads(r *http.Request) (interface{}, mw.Response) {
	const (
		maxPageSize = 200
	)

	var (
		nodeID = mux.Vars(r)["node_id"]
	)

	from, err := a.parseID(r.FormValue("from"))
	if err != nil {
		return nil, mw.BadRequest(err)
	}

	find := func(ctx context.Context, db *mongo.Database, filter types.ReservationFilter) ([]types.Workload, error) {
		cur, err := filter.Find(r.Context(), db)
		if err != nil {
			return nil, err
		}

		defer cur.Close(r.Context())

		workloads := []types.Workload{}

		for cur.Next(r.Context()) {
			var reservation types.Reservation
			if err := cur.Decode(&reservation); err != nil {
				return nil, err
			}

			reservation, err = a.pipeline(reservation, nil)
			if err != nil {
				log.Error().Err(err).Int64("id", int64(reservation.ID)).Msg("failed to process reservation")
				continue
			}

			// only reservations that is in right status
			if !reservation.IsAny(types.Deploy, types.Delete) {
				continue
			}

			resLoads := reservation.Workloads(nodeID)
			if reservation.NextAction == types.Deploy {
				workloads = append(workloads, resLoads...)
			} else {
				for _, wl := range resLoads {
					result := reservation.ResultOf(wl.WorkloadId)
					if result != nil && result.State == generated.TfgridWorkloadsReservationResult1StateDeleted {
						// so this workload has been already deleted by the node.
						// hence we don't need to serve it again
						continue
					}

					workloads = append(workloads, wl)
				}
			}

			if len(workloads) >= maxPageSize {
				break
			}
		}

		return workloads, nil
	}

	filter := types.ReservationFilter{}.WithIdGE(from).Or(types.ReservationFilter{}.WithNextAction(generated.TfgridWorkloadsReservation1NextActionDelete))
	filter = filter.WithNodeID(nodeID)

	db := mw.Database(r)
	//NOTE: the filter will find old reservations that has explicitly set to delete
	//not the ones that expired. The node should take care of the ones that expires
	//naturally.
	workloads, err := find(r.Context(), db, filter)
	if err != nil {
		return nil, mw.Error(errors.Wrap(err, "failed to list reservations to delete"))
	}

	return workloads, nil
}

func (a *API) workloadGet(r *http.Request) (interface{}, mw.Response) {
	gwid := mux.Vars(r)["gwid"]

	rid, err := a.parseID(strings.Split(gwid, "-")[0])
	if err != nil {
		return nil, mw.BadRequest(errors.Wrap(err, "invalid reservation id part"))
	}

	var filter types.ReservationFilter
	filter = filter.WithID(rid)

	db := mw.Database(r)
	reservation, err := a.pipeline(filter.Get(r.Context(), db))
	if err != nil {
		return nil, mw.NotFound(err)
	}
	// we use an empty node-id in listing to return all workloads in this reservation
	workloads := reservation.Workloads("")

	var workload *types.Workload
	for _, wl := range workloads {
		if wl.WorkloadId == gwid {
			workload = &wl
			break
		}
	}

	if workload == nil {
		return nil, mw.NotFound(fmt.Errorf("workload not found"))
	}

	var result struct {
		types.Workload
		Result *types.Result `json:"result"`
	}
	result.Workload = *workload
	for _, rs := range reservation.Results {
		if rs.WorkloadId == workload.WorkloadId {
			t := types.Result(rs)
			result.Result = &t
			break
		}
	}

	return result, nil
}

func (a *API) workloadPutResult(r *http.Request) (interface{}, mw.Response) {
	defer r.Body.Close()

	nodeID := mux.Vars(r)["node_id"]
	gwid := mux.Vars(r)["gwid"]

	rid, err := a.parseID(strings.Split(gwid, "-")[0])
	if err != nil {
		return nil, mw.BadRequest(errors.Wrap(err, "invalid reservation id part"))
	}

	var result types.Result
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		return nil, mw.BadRequest(err)
	}

	var filter types.ReservationFilter
	filter = filter.WithID(rid)

	db := mw.Database(r)
	reservation, err := a.pipeline(filter.Get(r.Context(), db))
	if err != nil {
		return nil, mw.NotFound(err)
	}
	// we use an empty node-id in listing to return all workloads in this reservation
	workloads := reservation.Workloads(nodeID)
	var workload *types.Workload
	for _, wl := range workloads {
		if wl.WorkloadId == gwid {
			workload = &wl
			break
		}
	}

	if workload == nil {
		return nil, mw.NotFound(errors.New("workload not found"))
	}

	result.WorkloadId = gwid
	result.Epoch = schema.Date{Time: time.Now()}

	if err := result.Verify(nodeID); err != nil {
		return nil, mw.UnAuthorized(errors.Wrap(err, "invalid result signature"))
	}

	if err := types.PushResult(r.Context(), db, rid, result); err != nil {
		return nil, mw.Error(err)
	}

	return nil, mw.Created()
}

func (a *API) workloadPutDeleted(r *http.Request) (interface{}, mw.Response) {
	// WARNING: #TODO
	// This method does not validate the signature of the caller
	// because there is no payload in a delete call.
	// may be a simple body that has "reservation id" and "signature"
	// can be used, we use the reservation id to avoid using the same
	// request body to delete other reservations

	// HTTP Delete should not have a body though, so may be this should be
	// changed to a PUT operation.

	nodeID := mux.Vars(r)["node_id"]
	gwid := mux.Vars(r)["gwid"]

	rid, err := a.parseID(strings.Split(gwid, "-")[0])
	if err != nil {
		return nil, mw.BadRequest(errors.Wrap(err, "invalid reservation id part"))
	}

	var filter types.ReservationFilter
	filter = filter.WithID(rid)

	db := mw.Database(r)
	reservation, err := a.pipeline(filter.Get(r.Context(), db))
	if err != nil {
		return nil, mw.NotFound(err)
	}

	// we use an empty node-id in listing to return all workloads in this reservation
	workloads := reservation.Workloads(nodeID)
	var workload *types.Workload
	for _, wl := range workloads {
		if wl.WorkloadId == gwid {
			workload = &wl
			break
		}
	}

	if workload == nil {
		return nil, mw.NotFound(errors.New("workload not found"))
	}

	result := reservation.ResultOf(gwid)
	if result == nil {
		// no result for this work load
		// QUESTION: should we still mark the result as deleted?
		result = &types.Result{
			WorkloadId: gwid,
			Epoch:      schema.Date{Time: time.Now()},
		}
	}

	result.State = generated.TfgridWorkloadsReservationResult1StateDeleted

	if err := types.PushResult(r.Context(), db, rid, *result); err != nil {
		return nil, mw.Error(err)
	}

	// get it from store again (make sure we are up to date)
	reservation, err = a.pipeline(filter.Get(r.Context(), db))
	if err != nil {
		return nil, mw.Error(err)
	}

	if !reservation.AllDeleted() {
		return nil, nil
	}

	if err := types.ReservationSetNextAction(r.Context(), db, reservation.ID, generated.TfgridWorkloadsReservation1NextActionDeleted); err != nil {
		return nil, mw.Error(err)
	}

	return nil, nil
}
