// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"fantom-api-graphql/internal/types"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"math/big"
)

const (
	colWithdrawals          = "withdraws"
	fiWithdrawalPk          = "_id"
	fiWithdrawalRequestID   = "req_id"
	fiWithdrawalAddress     = "addr"
	fiWithdrawalToValidator = "to"
	fiWithdrawalCreated     = "cr_time"
	fiWithdrawalValue       = "value"
)

// initWithdrawalsCollection initializes the withdraw requests collection with
// indexes and additional parameters needed by the app.
func (db *MongoDbBridge) initWithdrawalsCollection(col *mongo.Collection) {
	// prepare index models
	ix := make([]mongo.IndexModel, 0)

	// index delegator + validator
	unique := true
	ix = append(ix, mongo.IndexModel{
		Keys: bson.D{{fiWithdrawalAddress, 1}, {fiWithdrawalToValidator, 1}},
		Options: &options.IndexOptions{
			Unique: &unique,
		},
	})

	// index request ID, delegator address, and creation time stamp
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{fiWithdrawalRequestID, 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{fiWithdrawalAddress, 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{fiWithdrawalCreated, -1}}})

	// create indexes
	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
		db.log.Panicf("can not create indexes for withdrawals collection; %s", err.Error())
	}

	// log we done that
	db.log.Debugf("withdrawals collection initialized")
}

// Withdrawal returns details of a withdraw request specified by the request ID.
func (db *MongoDbBridge) Withdrawal(addr *common.Address, valID *hexutil.Big, reqID *hexutil.Big) (*types.WithdrawRequest, error) {
	// get the collection for withdrawals
	col := db.client.Database(db.dbName).Collection(colWithdrawals)

	// try to find the delegation in the database
	sr := col.FindOne(context.Background(), bson.D{
		{fiWithdrawalAddress, addr.String()},
		{fiWithdrawalToValidator, valID.String()},
		{fiWithdrawalRequestID, reqID.String()},
	})

	// try to decode
	var wr types.WithdrawRequest
	err := sr.Decode(&wr)
	if err != nil {
		return nil, err
	}

	return &wr, nil
}

// AddWithdrawal stores a withdraw request in the database if it doesn't exist.
func (db *MongoDbBridge) AddWithdrawal(wr *types.WithdrawRequest) error {
	// get the collection for withdrawals
	col := db.client.Database(db.dbName).Collection(colWithdrawals)

	// if the delegation already exists, update it with the new one
	if db.isWithdrawalKnown(col, wr) {
		return db.UpdateWithdrawal(wr)
	}

	// try to do the insert
	if _, err := col.InsertOne(context.Background(), wr); err != nil {
		db.log.Critical(err)
		return err
	}

	// make sure delegation collection is initialized
	if db.initWithdrawals != nil {
		db.initWithdrawals.Do(func() { db.initWithdrawalsCollection(col); db.initWithdrawals = nil })
	}
	return nil
}

// UpdateWithdrawal updates the given withdraw request in database.
func (db *MongoDbBridge) UpdateWithdrawal(wr *types.WithdrawRequest) error {
	// get the collection for withdrawals
	col := db.client.Database(db.dbName).Collection(colWithdrawals)

	// try to update a withdraw request by replacing it in the database
	// we use request ID identify unique withdrawal
	er, err := col.UpdateOne(context.Background(), bson.D{
		{fiWithdrawalAddress, wr.Address.String()},
		{fiWithdrawalToValidator, wr.StakerID.String()},
		{fiWithdrawalRequestID, wr.WithdrawRequestID.String()},
	}, bson.D{{"$set", wr}}, new(options.UpdateOptions).SetUpsert(true))
	if err != nil {
		db.log.Critical(err)
		return err
	}

	// do we actually have the document
	if 0 == er.MatchedCount {
		return fmt.Errorf("can not update, the withdraw request not found in database")
	}
	return nil
}

// isWithdrawalKnown checks if the given delegation exists in the database.
func (db *MongoDbBridge) isWithdrawalKnown(col *mongo.Collection, wr *types.WithdrawRequest) bool {
	// try to find the delegation in the database
	sr := col.FindOne(context.Background(), bson.D{
		{fiWithdrawalAddress, wr.Address.String()},
		{fiWithdrawalToValidator, wr.StakerID.String()},
		{fiWithdrawalRequestID, wr.WithdrawRequestID.String()},
	}, options.FindOne().SetProjection(bson.D{
		{fiWithdrawalPk, true},
	}))

	// error on lookup?
	if sr.Err() != nil {
		// may be ErrNoDocuments, which we seek
		if sr.Err() == mongo.ErrNoDocuments {
			return false
		}

		// inform that we can not get the PK; should not happen
		db.log.Errorf("can not get existing withdraw request pk; %s", sr.Err().Error())
		return false
	}

	return true
}

// WithdrawalCountFiltered calculates total number of withdraw requests in the database for the given filter.
func (db *MongoDbBridge) WithdrawalCountFiltered(filter *bson.D) (uint64, error) {
	// make sure some filter is used
	if nil == filter {
		filter = &bson.D{}
	}

	// get the collection for delegations
	col := db.client.Database(db.dbName).Collection(colWithdrawals)

	// do the counting
	val, err := col.CountDocuments(context.Background(), *filter)
	if err != nil {
		db.log.Errorf("can not count documents in withdrawals collection; %s", err.Error())
		return 0, err
	}

	return uint64(val), nil
}

// DelegationsCount calculates total number of delegations in the database.
func (db *MongoDbBridge) WithdrawalsCount() (uint64, error) {
	return db.WithdrawalCountFiltered(nil)
}

// wrListInit initializes list of withdraw requests based on provided cursor, count, and filter.
func (db *MongoDbBridge) wrListInit(col *mongo.Collection, cursor *string, count int32, filter *bson.D) (*types.WithdrawRequestList, error) {
	// make sure some filter is used
	if nil == filter {
		filter = &bson.D{}
	}

	// find how many transactions do we have in the database
	total, err := col.CountDocuments(context.Background(), *filter)
	if err != nil {
		db.log.Errorf("can not count withdraw requests")
		return nil, err
	}

	// make the list and notify the size of it
	db.log.Debugf("found %d filtered withdraw requests", total)
	list := types.WithdrawRequestList{
		Collection: make([]*types.WithdrawRequest, 0),
		Total:      uint64(total),
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
		Filter:     *filter,
	}

	// is the list non-empty? return the list with properly calculated range marks
	if 0 < total {
		return db.wrListCollectRangeMarks(col, &list, cursor, count)
	}

	// this is an empty list
	db.log.Debug("empty withdraw requests list created")
	return &list, nil
}

// wrListCollectRangeMarks returns the list of withdraw requests with proper First/Last marks.
func (db *MongoDbBridge) wrListCollectRangeMarks(col *mongo.Collection, list *types.WithdrawRequestList, cursor *string, count int32) (*types.WithdrawRequestList, error) {
	var err error

	// find out the cursor ordinal index
	if cursor == nil && count > 0 {
		// get the highest available pk
		list.First, err = db.wrListBorderPk(col,
			list.Filter,
			options.FindOne().SetSort(bson.D{{fiWithdrawalCreated, -1}, {fiWithdrawalPk, -1}}))
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// get the lowest available pk
		list.First, err = db.wrListBorderPk(col,
			list.Filter,
			options.FindOne().SetSort(bson.D{{fiWithdrawalCreated, 1}, {fiWithdrawalPk, 1}}))
		list.IsEnd = true

	} else if cursor != nil {
		// the cursor itself is the starting point
		list.First, err = db.wrListBorderPk(col,
			bson.D{{fiWithdrawalPk, *cursor}},
			options.FindOne())
	}

	// check the error
	if err != nil {
		db.log.Errorf("can not find the initial withdraw request")
		return nil, err
	}

	// inform what we are about to do
	db.log.Debugf("withdraw requests list initialized with PK %s", list.First)
	return list, nil
}

// wrListBorderPk finds the top PK of the withdraw requests collection based on given filter and options.
func (db *MongoDbBridge) wrListBorderPk(col *mongo.Collection, filter bson.D, opt *options.FindOneOptions) (uint64, error) {
	// prep container
	var row struct {
		Value uint64 `bson:"_id"`
	}

	// make sure we pull only what we need
	opt.SetProjection(bson.D{{fiWithdrawalPk, true}})
	sr := col.FindOne(context.Background(), filter, opt)

	// try to decode
	err := sr.Decode(&row)
	if err != nil {
		return 0, err
	}

	return row.Value, nil
}

// wrListFilter creates a filter for withdraw requests list loading.
func (db *MongoDbBridge) wrListFilter(cursor *string, count int32, list *types.WithdrawRequestList) *bson.D {
	// build an extended filter for the query; add PK (decoded cursor) to the original filter
	if cursor == nil {
		if count > 0 {
			list.Filter = append(list.Filter, bson.E{Key: fiWithdrawalPk, Value: bson.D{{"$gte", list.First}}})
		} else {
			list.Filter = append(list.Filter, bson.E{Key: fiWithdrawalPk, Value: bson.D{{"$lte", list.First}}})
		}
	} else {
		if count > 0 {
			list.Filter = append(list.Filter, bson.E{Key: fiWithdrawalPk, Value: bson.D{{"$gt", list.First}}})
		} else {
			list.Filter = append(list.Filter, bson.E{Key: fiWithdrawalPk, Value: bson.D{{"$lt", list.First}}})
		}
	}

	// return the new filter
	return &list.Filter
}

// wrListOptions creates a filter options set for withdraw requests list search.
func (db *MongoDbBridge) wrListOptions(count int32) *options.FindOptions {
	// prep options
	opt := options.Find()

	// how to sort results in the collection
	// from high (new) to low (old) by default; reversed if loading from bottom
	sd := -1
	if count < 0 {
		sd = 1
	}

	// sort with the direction we want
	opt.SetSort(bson.D{{fiWithdrawalCreated, sd}, {fiWithdrawalPk, sd}})

	// prep the loading limit
	var limit = int64(count)
	if limit < 0 {
		limit = -limit
	}

	// apply the limit, try to get one more record so we can detect list end
	opt.SetLimit(limit + 1)
	return opt
}

// wrListLoad load the initialized list of withdraw requests from database.
func (db *MongoDbBridge) wrListLoad(col *mongo.Collection, cursor *string, count int32, list *types.WithdrawRequestList) (err error) {
	// get the context for loader
	ctx := context.Background()

	// load the data
	ld, err := col.Find(ctx, db.wrListFilter(cursor, count, list), db.wrListOptions(count))
	if err != nil {
		db.log.Errorf("error loading with requests list; %s", err.Error())
		return err
	}

	// close the cursor as we leave
	defer func() {
		err = ld.Close(ctx)
		if err != nil {
			db.log.Errorf("error closing withdraw requests list cursor; %s", err.Error())
		}
	}()

	// loop and load the list; we may not store the last value
	var wr *types.WithdrawRequest
	for ld.Next(ctx) {
		// append a previous value to the list, if we have one
		if wr != nil {
			list.Collection = append(list.Collection, wr)
		}

		// try to decode the next row
		var row types.WithdrawRequest
		if err = ld.Decode(&row); err != nil {
			db.log.Errorf("can not decode the withdraw request list row; %s", err.Error())
			return err
		}

		// use this row as the next item
		wr = &row
	}

	// we should have all the items already; we may just need to check if a boundary was reached
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// add the last item as well if we hit the boundary
	if (list.IsStart || list.IsEnd) && wr != nil {
		list.Collection = append(list.Collection, wr)
	}
	return nil
}

// Withdrawals pulls list of withdraw requests starting at the specified cursor.
func (db *MongoDbBridge) Withdrawals(cursor *string, count int32, filter *bson.D) (*types.WithdrawRequestList, error) {
	// nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero withdrawals requested")
	}

	// get the collection and context
	col := db.client.Database(db.dbName).Collection(colWithdrawals)

	// init the list
	list, err := db.wrListInit(col, cursor, count, filter)
	if err != nil {
		db.log.Errorf("can not build withdraw requests list; %s", err.Error())
		return nil, err
	}

	// load data if there are any
	if list.Total > 0 {
		err = db.wrListLoad(col, cursor, count, list)
		if err != nil {
			db.log.Errorf("can not load withdraw requests list from database; %s", err.Error())
			return nil, err
		}

		// reverse on negative so new-er delegations will be on top
		if count < 0 {
			list.Reverse()
		}
	}

	return list, nil
}

// WithdrawalsSumValue calculates sum of values for all the withdrawals by a filter.
func (db *MongoDbBridge) WithdrawalsSumValue(filter *bson.D) (*big.Int, error) {
	// make sure we have at least some filter
	if filter == nil {
		filter = &bson.D{}
	}

	// get the collection
	col := db.client.Database(db.dbName).Collection(colWithdrawals)
	cr, err := col.Aggregate(context.Background(), mongo.Pipeline{
		{{"$match", filter}},
		{{"$group", bson.D{
			{"_id", nil},
			{"total", bson.D{{"$sum", fiWithdrawalValue}}},
		}}},
	})
	if err != nil {
		db.log.Errorf("can not calculate withdrawal sum value; %s", err.Error())
		return nil, err
	}

	// read the data and return result
	return db.readAggregatedSum(cr)
}

// readAggregatedValue extract the aggregated value from the given result set.
func (db *MongoDbBridge) readAggregatedSum(cr *mongo.Cursor) (*big.Int, error) {
	// make sure to close the cursor after we got the data
	defer func() {
		if err := cr.Close(context.Background()); err != nil {
			db.log.Errorf("can not close aggregate cursor; %s", err.Error())
		}
	}()

	// do we have any data to read?
	if !cr.Next(context.Background()) {
		return new(big.Int), nil
	}

	// try to get the calculated value
	var row struct {
		Total uint64 `bson:"total"`
	}
	if err := cr.Decode(&row); err != nil {
		db.log.Errorf("can not read withdrawal sum value; %s", err.Error())
		return nil, err
	}

	return new(big.Int).Mul(new(big.Int).SetUint64(row.Total), types.WithdrawDecimalsCorrection), nil
}
