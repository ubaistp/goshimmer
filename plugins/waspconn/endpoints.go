package waspconn

import (
	"net/http"

	"github.com/iotaledger/goshimmer/dapps/valuetransfers/packages/address"
	"github.com/iotaledger/goshimmer/dapps/valuetransfers/packages/transaction"
	"github.com/iotaledger/goshimmer/packages/waspconn/apilib"
	"github.com/iotaledger/goshimmer/packages/waspconn/utxodb"
	"github.com/iotaledger/goshimmer/plugins/webapi"
	"github.com/labstack/echo"
	"github.com/mr-tron/base58"
)

func addEndpoints() {
	webapi.Server.GET("/utxodb/outputs/:address", handleGetAddressOutputs)
	webapi.Server.POST("/utxodb/tx", handlePostTransaction)
}

func handleGetAddressOutputs(c echo.Context) error {
	addr, err := address.FromBase58(c.Param("address"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, &apilib.GetAccountOutputsResponse{Err: err.Error()})
	}
	outputs := utxodb.GetAddressOutputs(addr)

	out := make(map[string][]apilib.OutputBalance)
	for txOutId, txOutputs := range outputs {
		txOut := make([]apilib.OutputBalance, len(txOutputs))
		for i, txOutput := range txOutputs {
			txOut[i] = apilib.OutputBalance{
				Value: txOutput.Value(),
				Color: transaction.ID(txOutput.Color()).String(),
			}
		}
		out[txOutId.String()] = txOut
	}
	return c.JSONPretty(http.StatusOK, &apilib.GetAccountOutputsResponse{
		Address: c.Param("address"),
		Outputs: out,
	}, " ")
}

func handlePostTransaction(c echo.Context) error {
	var req apilib.PostTransactionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, &apilib.PostTransactionResponse{Err: err.Error()})
	}

	txBytes, err := base58.Decode(req.Tx)
	if err != nil {
		return c.JSON(http.StatusBadRequest, &apilib.PostTransactionResponse{Err: err.Error()})
	}

	tx, _, err := transaction.FromBytes(txBytes)
	if err != nil {
		return c.JSON(http.StatusBadRequest, &apilib.PostTransactionResponse{Err: err.Error()})
	}

	err = utxodb.AddTransaction(tx)
	if err != nil {
		return c.JSON(http.StatusConflict, &apilib.PostTransactionResponse{Err: err.Error()})
	}

	EventValueTransactionReceived.Trigger(tx)

	return c.JSON(http.StatusOK, &apilib.PostTransactionResponse{})
}
