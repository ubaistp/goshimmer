package connector

import (
	"github.com/iotaledger/goshimmer/dapps/valuetransfers/packages/address"
	"github.com/iotaledger/goshimmer/dapps/valuetransfers/packages/balance"
	"github.com/iotaledger/goshimmer/dapps/valuetransfers/packages/transaction"
	"github.com/iotaledger/goshimmer/dapps/waspconn/packages/chopper"
	"github.com/iotaledger/goshimmer/dapps/waspconn/packages/waspconn"
	"github.com/iotaledger/goshimmer/packages/binary/messagelayer/payload"
	"io"
	"strings"
)

func (wconn *WaspConnector) sendMsgToWasp(msg interface{ Write(io.Writer) error }) error {
	data, err := waspconn.EncodeMsg(msg)
	if err != nil {
		return err
	}
	choppedData, chopped := chopper.ChopData(data, payload.MaxMessageSize-waspconn.ChunkMessageHeaderSize)
	if !chopped {
		if len(data) > payload.MaxMessageSize {
			panic("sendMsgToWasp: internal inconsistency 1")
		}
		_, err = wconn.bconn.Write(data)
		return err
	}

	wconn.log.Debugf("+++++++++++++ %d bytes long message was split into %d chunks", len(data), len(choppedData))

	// sending piece by piece wrapped in WaspMsgChunk
	for _, piece := range choppedData {
		dataToSend, err := waspconn.EncodeMsg(&waspconn.WaspMsgChunk{
			Data: piece,
		})
		if err != nil {
			return err
		}
		if len(dataToSend) > payload.MaxMessageSize {
			wconn.log.Panicf("sendMsgToWasp: internal inconsistency 3 size too big: %d", len(dataToSend))
		}
		_, err = wconn.bconn.Write(dataToSend)
		if err != nil {
			return err
		}
	}
	return nil
}

func (wconn *WaspConnector) sendConfirmedTransactionToWasp(vtx *transaction.Transaction) error {
	return wconn.sendMsgToWasp(&waspconn.WaspFromNodeConfirmedTransactionMsg{
		Tx: vtx,
	})
}

func (wconn *WaspConnector) sendAddressUpdateToWasp(addr *address.Address, balances map[transaction.ID][]*balance.Balance, tx *transaction.Transaction) error {
	return wconn.sendMsgToWasp(&waspconn.WaspFromNodeAddressUpdateMsg{
		Address:  *addr,
		Balances: balances,
		Tx:       tx,
	})
}

func (wconn *WaspConnector) sendAddressOutputsToWasp(address *address.Address, balances map[transaction.ID][]*balance.Balance) error {
	return wconn.sendMsgToWasp(&waspconn.WaspFromNodeAddressOutputsMsg{
		Address:  *address,
		Balances: balances,
	})
}

func (wconn *WaspConnector) sendTxInclusionLevelToWasp(inclLevel byte, txid *transaction.ID, addrs []address.Address) error {
	return wconn.sendMsgToWasp(&waspconn.WaspFromNodeTransactionInclusionLevelMsg{
		Level:               inclLevel,
		TxId:                *txid,
		SubscribedAddresses: addrs,
	})
}

// query outputs database and collects transactions containing unprocessed requests
func (wconn *WaspConnector) pushBacklogToWasp(addr *address.Address) {
	outs, err := wconn.vtangle.GetConfirmedAddressOutputs(*addr)
	if err != nil {
		wconn.log.Error(err)
		return
	}
	if len(outs) == 0 {
		return
	}
	outputs := waspconn.OutputsToBalances(outs)
	allColors := make(map[transaction.ID]bool)
	for _, bals := range outputs {
		for _, b := range bals {
			col := b.Color
			if col == balance.ColorIOTA {
				continue
			}
			if col == balance.ColorNew {
				wconn.log.Errorf("unexpected balance.ColorNew")
				continue
			}
			allColors[(transaction.ID)(b.Color)] = true
		}
	}

	wconn.log.Infof("backlog -> Wasp for addr: %s, txs: [%s]", addr.String(), txidList(allColors))
	for txid := range allColors {
		tx := wconn.vtangle.GetConfirmedTransaction(&txid)
		if tx == nil {
			wconn.log.Errorf("inconsistency: can't find txid = %s", txid.String())
			continue
		}
		if err := wconn.sendAddressUpdateToWasp(addr, outputs, tx); err != nil {
			wconn.log.Errorf("sendAddressUpdateToWasp: %v", err)
		}
	}
}

func txidList(txidSet map[transaction.ID]bool) string {
	ret := make([]string, 0, len(txidSet))
	for txid := range txidSet {
		ret = append(ret, txid.String())
	}
	return strings.Join(ret, ", ")
}
