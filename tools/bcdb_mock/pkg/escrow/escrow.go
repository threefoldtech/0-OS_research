package escrow

import (
	"context"
	"fmt"
	"strconv"

	"github.com/pkg/errors"
	"github.com/stellar/go/amount"
	"github.com/stellar/go/xdr"
	"github.com/threefoldtech/zos/pkg/schema"
	"github.com/threefoldtech/zos/tools/bcdb_mock/models/generated/workloads"
	"github.com/threefoldtech/zos/tools/bcdb_mock/pkg/directory"
	directorytypes "github.com/threefoldtech/zos/tools/bcdb_mock/pkg/directory/types"
	"github.com/threefoldtech/zos/tools/bcdb_mock/pkg/escrow/types"
	"github.com/threefoldtech/zos/tools/bcdb_mock/pkg/stellar"
	"go.mongodb.org/mongo-driver/mongo"
)

type (
	// Escrow service manages a dedicate wallet for payments for reservations.
	Escrow struct {
		wallet             *stellar.Wallet
		db                 *mongo.Database
		reservationChannel chan reservationRegisterJob
		farmAPI            FarmAPI
	}

	// FarmAPI interface
	FarmAPI interface {
		GetByID(ctx context.Context, db *mongo.Database, id int64) (directorytypes.Farm, error)
	}

	reservationRegisterJob struct {
		reservation  workloads.Reservation
		responseChan chan reservationRegisterJobResponse
	}

	reservationRegisterJobResponse struct {
		data []types.EscrowDetail
		err  error
	}
)

// New creates a new escrow object and fetches all addresses for the escrow wallet
func New(wallet *stellar.Wallet, db *mongo.Database) (*Escrow, error) {
	jobChannel := make(chan reservationRegisterJob)
	return &Escrow{
		wallet:             wallet,
		db:                 db,
		farmAPI:            &directory.FarmAPI{},
		reservationChannel: jobChannel,
	}, nil
}

// Run the escrow until the context is done
func (e *Escrow) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case job := <-e.reservationChannel:
			rsuPerFarmer, err := processReservation(job.reservation.DataReservation, &dbNodeSource{ctx: ctx, db: e.db})
			if err != nil {
				job.responseChan <- reservationRegisterJobResponse{
					err: err,
				}
				close(job.responseChan)
				continue
			}
			res, err := e.CalculateReservationCost(rsuPerFarmer)
			if err != nil {
				job.responseChan <- reservationRegisterJobResponse{
					err: err,
				}
				close(job.responseChan)
				continue
			}
			details := make([]types.EscrowDetail, 0, len(res))
			for farmer, value := range res {
				address, err := e.CreateOrLoadAccount(farmer, job.reservation.CustomerTid)
				if err != nil {
					job.responseChan <- reservationRegisterJobResponse{
						err: err,
					}
					close(job.responseChan)
					break
				}
				details = append(details, types.EscrowDetail{
					FarmerID:      schema.ID(farmer),
					EscrowAddress: address,
					TotalAmount:   value,
				})
			}
			if err != nil {
				continue
			}
			reservationPaymentInfo := types.ReservationPaymentInformation{
				Infos:         details,
				ReservationID: job.reservation.ID,
				Expiration:    job.reservation.DataReservation.ExpirationProvisioning,
				Paid:          false,
			}
			err = types.ReservationPaymentInfoCreate(ctx, e.db, reservationPaymentInfo)
			job.responseChan <- reservationRegisterJobResponse{
				err:  err,
				data: details,
			}
		}
	}
}

// CreateOrLoadAccount creates or loads account based on farmer - customer id
func (e *Escrow) CreateOrLoadAccount(farmerID int64, customerTID int64) (string, error) {
	res, err := types.Get(context.Background(), e.db, farmerID, customerTID)
	if err != nil {
		if err == types.ErrAddressNotFound {
			addr, err := e.wallet.CreateAccount()
			if err != nil {
				return "", errors.Wrap(err, "failed to create a new address for farmer - customer")
			}
			err = types.FarmerCustomerAddressCreate(context.Background(), e.db, types.FarmerCustomerAddress{
				CustomerTID: customerTID,
				Address:     addr,
				FarmerID:    farmerID,
			})
			if err != nil {
				return "", errors.Wrap(err, "failed to save a new address for farmer - customer")
			}
			return addr, nil
		}
		return "", errors.Wrap(err, "failed to get farmer - customer address")
	}
	return res.Address, nil
}

// RegisterReservation registers a workload reservation
func (e *Escrow) RegisterReservation(reservation workloads.Reservation) ([]types.EscrowDetail, error) {
	job := reservationRegisterJob{
		reservation:  reservation,
		responseChan: make(chan reservationRegisterJobResponse),
	}
	e.reservationChannel <- job

	response := <-job.responseChan

	return response.data, response.err
}

// CalculateReservationCost calculates the cost of reservation based on a resource per farmer map
func (e *Escrow) CalculateReservationCost(rsuPerFarmerMap rsuPerFarmer) (map[int64]xdr.Int64, error) {
	costPerFarmerMap := make(map[int64]xdr.Int64)
	for id, rsu := range rsuPerFarmerMap {
		farm, err := e.farmAPI.GetByID(context.Background(), e.db, id)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get farm with id: %d", id)
		}
		// why is this a list ?!
		if len(farm.ResourcePrices) == 0 {
			return nil, fmt.Errorf("farm with id: %d does not have price setup", id)
		}
		price := farm.ResourcePrices[0]
		var cost xdr.Int64

		cruPriceCoin, err := amount.Parse(strconv.FormatFloat(price.Cru, 'f', 7, 64))
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse cru price")
		}
		if cruPriceCoin < 0 {
			return nil, errors.New("cru price is invalid")
		}

		sruPriceCoin, err := amount.Parse(strconv.FormatFloat(price.Sru, 'f', 7, 64))
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse sru price")
		}
		if sruPriceCoin < 0 {
			return nil, errors.New("sru price is invalid")
		}

		hruPriceCoin, err := amount.Parse(strconv.FormatFloat(price.Hru, 'f', 7, 64))
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse hru price")
		}
		if hruPriceCoin < 0 {
			return nil, errors.New("hru price is invalid")
		}

		mruPriceCoin, err := amount.Parse(strconv.FormatFloat(price.Mru, 'f', 7, 64))
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse mru price")
		}
		if mruPriceCoin < 0 {
			return nil, errors.New("mru price is invalid")
		}

		cost += cruPriceCoin * (xdr.Int64(rsu.cru))
		cost += sruPriceCoin * (xdr.Int64(rsu.sru))
		cost += hruPriceCoin * (xdr.Int64(rsu.hru))
		cost += mruPriceCoin * (xdr.Int64(rsu.mru))

		costPerFarmerMap[id] = cost
	}
	return costPerFarmerMap, nil
}
