package sender

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/paxeer-network/pax-load/types"
)

// implements `Send`

type TxsWriter struct {
	gasPerBlock     uint64
	nextHeight      uint64
	txsDir          string
	blocksGenerated uint64
	numBlocks       uint64

	bufferGas uint64
	txBuffer  []*types.LoadTx
}

func NewTxsWriter(gasPerBlock uint64, txsDir string, startHeight uint64, numBlocks uint64) *TxsWriter {
	// what height to start at?
	return &TxsWriter{
		gasPerBlock:     gasPerBlock,
		nextHeight:      startHeight,
		txsDir:          txsDir,
		blocksGenerated: 0,
		numBlocks:       numBlocks,

		bufferGas: 0,
		txBuffer:  make([]*types.LoadTx, 0),
	}
}

// Send writes the transaction to the writer
func (w *TxsWriter) Send(ctx context.Context, tx *types.LoadTx) error {
	// if bwe would exceed gasPerBlock, flush
	if w.bufferGas+tx.EthTx.Gas() > w.gasPerBlock {
		if err := w.Flush(); err != nil {
			return err
		}
	}

	// add to buffer
	w.txBuffer = append(w.txBuffer, tx)
	w.bufferGas += tx.EthTx.Gas()
	return nil
}

type TxWriteData struct {
	TxPayloads [][]byte `json:"tx_payloads"`
}

func (w *TxsWriter) Flush() error {
	defer func() {
		// clear buffer and reset bufferGas and increment nextHeight
		w.txBuffer = make([]*types.LoadTx, 0)
		w.bufferGas = 0
		w.nextHeight++
		w.blocksGenerated++
	}()
	// write to dir `~/load_txs`
	// make dir if it doesn't exist
	err := os.MkdirAll(w.txsDir, 0755)
	if err != nil {
		return err
	}
	txsFile := filepath.Join(w.txsDir, fmt.Sprintf("%d_txs.json", w.nextHeight))
	txData := TxWriteData{
		TxPayloads: make([][]byte, 0),
	}
	for _, tx := range w.txBuffer {
		txData.TxPayloads = append(txData.TxPayloads, tx.Payload)
	}

	txDataJSON, err := json.Marshal(txData)
	if err != nil {
		return err
	}

	if err := os.WriteFile(txsFile, txDataJSON, 0644); err != nil {
		return err
	}

	log.Printf("Flushed %d transactions to %s", len(w.txBuffer), txsFile)

	if w.blocksGenerated >= w.numBlocks {
		return fmt.Errorf("reached max number of blocks: %d", w.numBlocks)
	}

	return nil
}
