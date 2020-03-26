package types

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/stellar/go/xdr"

	rivtypes "github.com/threefoldtech/rivine/types"
	"github.com/threefoldtech/zos/pkg/schema"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	// EscrowCollection db collection name
	EscrowCollection = "escrow"
)

var (
	// ErrEscrowExists is returned when trying to save escrow information for a
	// reservation that already has escrow information
	ErrEscrowExists = errors.New("escrow(s) for reservation already exists")
	// ErrEscrowNotFound is returned if escrow information is not found
	ErrEscrowNotFound = errors.New("escrow information not found")
)

type (
	// ReservationPaymentInformation stores the reservation payment information
	ReservationPaymentInformation struct {
		ReservationID schema.ID      `bson:"_id" json:"_id"`
		Expiration    schema.Date    `bson:"expiration" json:"expiration"`
		Infos         []EscrowDetail `bson:"infos" json:"infos"`
		Paid          bool           `bson:"paid" json:"paid"`
	}

	// EscrowDetail hold the details of an escrow address
	EscrowDetail struct {
		FarmerID      schema.ID `bson:"farmer_id" json:"farmer_id"`
		TotalAmount   xdr.Int64 `bson:"total_amount" json:"total_amount"`
		EscrowAddress string    `bson:"escrow_address" json:"escrow_address"`
	}

	// Currency is an amount of tokens
	Currency struct {
		rivtypes.Currency
	}

	// Address is an on chain address
	Address struct {
		rivtypes.UnlockHash
	}
)

// ReservationPaymentInfoCreate creates the reservation payment information
func ReservationPaymentInfoCreate(ctx context.Context, db *mongo.Database, reservationPaymentInfo ReservationPaymentInformation) error {
	col := db.Collection(EscrowCollection)
	_, err := col.InsertOne(ctx, reservationPaymentInfo)
	if err != nil {
		if merr, ok := err.(mongo.WriteException); ok {
			errCode := merr.WriteErrors[0].Code
			if errCode == 11000 {
				return ErrEscrowExists
			}
		}
		return err
	}
	return nil
}

// ReservationPaymentInfoUpdate update reservation payment info
func ReservationPaymentInfoUpdate(ctx context.Context, db *mongo.Database, update ReservationPaymentInformation) error {
	filter := bson.M{"_id": update.ReservationID}
	// actually update the user with final data
	if _, err := db.Collection(EscrowCollection).UpdateOne(ctx, filter, bson.M{"$set": update}); err != nil {
		return err
	}

	return nil
}

// GetAllActiveReservationPaymentInfos get all active reservation payment information
func GetAllActiveReservationPaymentInfos(ctx context.Context, db *mongo.Database) ([]ReservationPaymentInformation, error) {
	filter := bson.M{"paid": false, "expiration": bson.M{"$gt": schema.Date{Time: time.Now()}}}
	cursor, err := db.Collection(EscrowCollection).Find(ctx, filter)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get cursor over active payment infos")
	}
	paymentInfos := make([]ReservationPaymentInformation, 0)
	err = cursor.All(ctx, &paymentInfos)
	if err != nil {
		err = errors.Wrap(err, "failed to decode active payment information")
	}
	return paymentInfos, err
}

// GetAllAddresses gets all in use addresses from the escrowcollections
func GetAllAddresses(ctx context.Context, db *mongo.Database) ([]string, error) {
	cursor, err := db.Collection(EscrowCollection).Find(ctx, bson.M{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create cursor over payment infos")
	}
	defer cursor.Close(ctx)
	addresses := make([]string, 0)
	var reservationPaymentInfo ReservationPaymentInformation
	for cursor.Next(ctx) {
		err = cursor.Decode(&reservationPaymentInfo)
		if err != nil {
			return nil, errors.Wrap(err, "failed to decode reservation payment info")
		}
		for _, paymentInfo := range reservationPaymentInfo.Infos {
			addresses = append(addresses, paymentInfo.EscrowAddress)
		}
	}
	return addresses, nil
}
