package types

import (
	"time"

	"github.com/pkg/errors"
	generated "github.com/threefoldtech/zos/tools/bcdb_mock/models/generated/workloads"
)

// Pipeline changes Reservation R as defined by the reservation pipeline
// returns new reservation object, and true if the reservation has changed
type Pipeline struct {
	r Reservation
}

// NewPipeline creates a reservation pipeline, all reservation must be processes
// through the pipeline before any action is taken. This will always make sure
// that reservation is in the right state.
func NewPipeline(R Reservation) (*Pipeline, error) {
	if err := R.validate(); err != nil {
		return nil, errors.Wrap(err, "invalid reservation object")
	}

	return &Pipeline{R}, nil
}

func (p *Pipeline) checkSignatures() bool {

	// Note: signatures validatation already done in the
	// signature add operation. Here we just make sure the
	// required quorum has been reached

	request := p.r.DataReservation.SigningRequestProvision
	if request.QuorumMin == 0 {
		return true
	}

	in := func(i int64, l []int64) bool {
		for _, x := range l {
			if x == i {
				return true
			}
		}
		return false
	}

	signatures := p.r.SignaturesProvision
	var count int64 = 0
	for _, signature := range signatures {
		if !in(signature.Tid, request.Signers) {
			continue
		}
		count++
	}
	if count >= request.QuorumMin {
		return true
	}

	return false
}

// Next gets new modified reservation, and true if the reservation has changed from the input
func (p *Pipeline) Next() (Reservation, bool) {
	if p.r.NextAction == generated.TfgridWorkloadsReservation1NextActionDelete {
		return p.r, false
	}

	// reseration expiration time must be checked, once expiration time is exceeded
	// the reservation must be deleted
	if p.r.Expired() {
		// reservation has expired
		// set its status (next action) to delete
		p.r.NextAction = generated.TfgridWorkloadsReservation1NextActionDelete
		return p.r, true
	}

	current := p.r.NextAction
	modified := false
	for {
		switch p.r.NextAction {
		case generated.TfgridWorkloadsReservation1NextActionCreate:
			// provision expiration, if exceeded, the node should not
			// try to deploy this reservation.
			if time.Until(p.r.DataReservation.ExpirationProvisioning.Time) <= 0 {
				// exceeded
				// TODO: I think this should be set to "delete" not "invalid"
				p.r.NextAction = generated.TfgridWorkloadsReservation1NextActionInvalid
			} else {
				p.r.NextAction = generated.TfgridWorkloadsReservation1NextActionSign
			}
		case generated.TfgridWorkloadsReservation1NextActionSign:
			// this stage will not change unless all
			if p.checkSignatures() {
				p.r.NextAction = generated.TfgridWorkloadsReservation1NextActionPay
			}
		case generated.TfgridWorkloadsReservation1NextActionPay:
			// TODO: here we should actually start the payment process
			// but this is not implemented yet, so now we just need to move
			// to deploy
			p.r.NextAction = generated.TfgridWorkloadsReservation1NextActionDeploy
		case generated.TfgridWorkloadsReservation1NextActionDeploy:
			//nothing to do
		}

		if current == p.r.NextAction {
			// no more changes in stage
			break
		}

		current = p.r.NextAction
		modified = true
	}

	return p.r, modified
}