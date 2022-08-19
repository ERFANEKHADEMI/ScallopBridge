// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package ethereum

import (
	"bytes"
    "encoding/json"
	"encoding/hex"
	"net/http"
	"github.com/Satosh-J/ScallopBridge/bindings/Bridge"
	"github.com/Satosh-J/scallopbridge-utils/core"
	metrics "github.com/Satosh-J/scallopbridge-utils/metrics/types"
	"github.com/Satosh-J/scallopbridge-utils/msg"
	"github.com/ChainSafe/log15"
)

var _ core.Writer = &writer{}

// https://github.com/ChainSafe/chainbridge-solidity/blob/b5ed13d9798feb7c340e737a726dd415b8815366/contracts/Bridge.sol#L20
var PassedStatus uint8 = 2
var TransferredStatus uint8 = 3
var CancelledStatus uint8 = 4

type writer struct {
	cfg            Config
	conn           Connection
	bridgeContract *Bridge.Bridge // instance of bound receiver bridgeContract
	log            log15.Logger
	stop           <-chan int
	sysErr         chan<- error // Reports fatal error to core
	metrics        *metrics.ChainMetrics
}

// NewWriter creates and returns writer
func NewWriter(conn Connection, cfg *Config, log log15.Logger, stop <-chan int, sysErr chan<- error, m *metrics.ChainMetrics) *writer {
	return &writer{
		cfg:     *cfg,
		conn:    conn,
		log:     log,
		stop:    stop,
		sysErr:  sysErr,
		metrics: m,
	}
}

func (w *writer) start() error {
	w.log.Debug("Starting ethereum writer...")
	return nil
}

// setContract adds the bound receiver bridgeContract to the writer
func (w *writer) setContract(bridge *Bridge.Bridge) {
	w.bridgeContract = bridge
}

// ResolveMessage handles any given message based on type
// A bool is returned to indicate failure/success, this should be ignored except for within tests.
func (w *writer) ResolveMessage(m msg.Message) bool {
	w.log.Info("Attempting to resolve message", "type", m.Type, "src", m.Source, "dst", m.Destination, "nonce", m.DepositNonce, "rId", m.ResourceId.Hex())

	w.log.Debug("Logging message", "tokenAddress", hex.EncodeToString(m.Payload[0].([]byte)))
	w.log.Debug("Logging message", "amount", hex.EncodeToString(m.Payload[1].([]byte)))
	w.log.Debug("Logging message", "recepient", hex.EncodeToString(m.Payload[2].([]byte)))
	w.log.Debug("Logging message", "txHash", hex.EncodeToString(m.Payload[3].([]byte)))
	w.log.Debug("Logging message", "handlerAddress", hex.EncodeToString(m.Payload[4].([]byte)))
	w.log.Debug("Logging message", "tokenSymbol", string(m.Payload[5].([]byte)))

	// only check transactions from Ethereum
	if (m.Source == msg.ChainId(1)) {
		values := map[string]string{
			"network": "Ethereum",
			"symbol": string(m.Payload[5].([]byte)),
			"user": "0x" + hex.EncodeToString(m.Payload[2].([]byte)),
			"tx": "0x" + hex.EncodeToString(m.Payload[3].([]byte)),
			"handler": "0x" + hex.EncodeToString(m.Payload[4].([]byte)),
		}
		json_data, err := json.Marshal(values)
		if err != nil {
			w.log.Error("JSON error", "err", err)
			return false
		}
	
		resp, err := http.Post("https://scallopbridge-backend.herokuapp.com/kyt-status", "application/json",
			bytes.NewBuffer(json_data))
	
		if err != nil {
			w.log.Error("Response error", "err", err)
			return false
		}
	
		var res map[string]interface{}
	
		json.NewDecoder(resp.Body).Decode(&res)
	
		w.log.Debug("Logging message", "response", res["status"])

		if res["status"] != true {
			w.log.Error("KYT chainalysis alert", "status", res["status"])
			return false
		}
	}

	switch m.Type {
	case msg.FungibleTransfer:
		return w.createErc20Proposal(m)
	case msg.NonFungibleTransfer:
		return w.createErc721Proposal(m)
	case msg.GenericTransfer:
		return w.createGenericDepositProposal(m)
	default:
		w.log.Error("Unknown message type received", "type", m.Type)
		return false
	}
}
