/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Opera/Lachesis full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"fantom-api-graphql/internal/repository"
	"fantom-api-graphql/internal/types"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.uber.org/atomic"
	"sync"
	"time"
)

// trxAddressQueueCapacity is the number of addresses kept in the dispatch buffer.
const trxAddressQueueCapacity = 1000

// trxLogQueueCapacity is the number of transaction logs kept in the dispatch buffer.
const trxLogQueueCapacity = 5000

// trxDispatchBlockUpdateTicker represents the period of block registry updater.
const trxDispatchBlockUpdateTicker = 15 * time.Second

// eventAcc represents a structure of a mentioned account.
type eventAcc struct {
	watchDog *sync.WaitGroup
	addr     *common.Address
	act      string
	blk      *types.Block
	trx      *types.Transaction
	deploy   *common.Hash
}

// trxDispatcher implements dispatcher of new transactions in the blockchain.
type trxDispatcher struct {
	or            *Orchestrator
	sigStop       chan bool
	onTransaction chan *types.Transaction

	bot           *time.Ticker
	blkObserver   *atomic.Uint64
	inTransaction chan *eventTrx
	outAccount    chan *eventAcc
	outLog        chan *types.LogRecord
}

// name returns the name of the service.
func (trd *trxDispatcher) name() string {
	return "transaction dispatcher"
}

// init prepares the transaction dispatcher to perform its function.
func (trd *trxDispatcher) init() {
	trd.sigStop = make(chan bool, 1)
	trd.blkObserver = atomic.NewUint64(1)
	trd.outAccount = make(chan *eventAcc, trxAddressQueueCapacity)
	trd.outLog = make(chan *types.LogRecord, trxLogQueueCapacity)
}

// run starts the transaction dispatcher job
func (trd *trxDispatcher) run() {
	// make sure we are orchestrated
	if trd.or == nil {
		panic(fmt.Errorf("no orchestrator set"))
	}

	// start the block observer ticker
	trd.bot = time.NewTicker(trxDispatchBlockUpdateTicker)

	// signal orchestrator we started and go
	trd.or.started(trd)
	go trd.dispatch()
}

// close terminates the block dispatcher.
func (trd *trxDispatcher) close() {
	trd.bot.Stop()
	trd.sigStop <- true
}

// dispatch implements the dispatcher reader and router routine.
func (trd *trxDispatcher) dispatch() {
	// don't forget to sign off after we are done
	defer func() {
		close(trd.sigStop)
		close(trd.outAccount)
		close(trd.outLog)

		trd.or.finished(trd)
	}()

	// wait for transactions and process them
	for {
		// try to read next transaction
		select {
		case <-trd.sigStop:
			return
		case <-trd.bot.C:
			trd.updateLastSeenBlock()
		case evt, ok := <-trd.inTransaction:
			// is the channel even available for reading
			if !ok {
				trd.or.log.Notice("trx channel closed, terminating %s", trd.name())
				return
			}

			// validate incoming data
			if evt.blk == nil || evt.trx == nil {
				trd.or.log.Criticalf("dispatcher dry loop")
				continue
			}

			// dispatch the received
			trd.process(evt)
		}
	}
}

// updateLastSeenBlock updates the information about last known block
// in the persistent database.
func (trd *trxDispatcher) updateLastSeenBlock() {
	// get the current value
	lsb := trd.blkObserver.Load()

	// log where we are
	trd.or.log.Noticef("last seen block is #%d", lsb)

	// make the change in the database so the progress persists
	err := repository.R().UpdateLastKnownBlock((*hexutil.Uint64)(&lsb))
	if err != nil {
		trd.or.log.Errorf("could not update last seen block; %s", err.Error())
	}
}

// process the given transaction event into the required targets.
func (trd *trxDispatcher) process(evt *eventTrx) {
	// make the work group for this trx
	var wg sync.WaitGroup

	// process transaction accounts
	trd.pushAccounts(evt, &wg)

	// process transaction logs
	for _, lg := range evt.trx.Logs {
		wg.Add(1)
		trd.outLog <- &types.LogRecord{
			WatchDog: &wg,
			Block:    evt.blk,
			Trx:      evt.trx,
			Log:      lg,
		}
	}

	// store the transaction into the database once the processing is done
	// we spawn a lot of go-routines here, so we should test the optimal queue length above
	go trd.waitAndStore(evt, &wg)

	// broadcast new transaction
	trd.onTransaction <- evt.trx
}

// waitAndStore waits for the transaction processing to finish and stores the transaction into db.
func (trd *trxDispatcher) waitAndStore(evt *eventTrx, wg *sync.WaitGroup) {
	// wait until the trx is processed
	wg.Wait()

	// store to the db
	if err := repository.R().StoreTransaction(evt.blk, evt.trx); err != nil {
		trd.or.log.Errorf("can not store trx %s from block #%d", evt.trx.Hash.String(), evt.blk.Number)
	}

	// update estimator
	repository.R().IncTrxCountEstimate(1)

	// add the transaction to the ring cache
	repository.R().CacheTransaction(evt.trx)

	// update internal block observer value
	trd.blkObserver.Store(uint64(evt.blk.Number))
}

// pushAccounts pushes given transaction accounts on both sides.
func (trd *trxDispatcher) pushAccounts(evt *eventTrx, wg *sync.WaitGroup) {
	// the sender is always present
	wg.Add(1)
	trd.outAccount <- &eventAcc{
		watchDog: wg,
		addr:     &evt.trx.From,
		act:      types.AccountTypeWallet,
		blk:      evt.blk,
		trx:      evt.trx,
		deploy:   nil,
	}

	// do we have a recipient?
	if evt.trx.To != nil {
		wg.Add(1)
		trd.outAccount <- &eventAcc{
			watchDog: wg,
			act:      types.AccountTypeWallet,
			addr:     evt.trx.To,
			blk:      evt.blk,
			trx:      evt.trx,
			deploy:   nil,
		}
	}

	// if there is no contract created, we are done here
	if evt.trx.ContractAddress == nil {
		return
	}

	// queue the new contract to be processed as well
	trd.or.log.Debugf("contract %s found at trx %s", evt.trx.ContractAddress.String(), evt.trx.Hash.String())
	wg.Add(1)
	trd.outAccount <- &eventAcc{
		watchDog: wg,
		addr:     evt.trx.ContractAddress,
		act:      types.AccountTypeContract,
		blk:      evt.blk,
		trx:      evt.trx,
		deploy:   &evt.trx.Hash,
	}
}
