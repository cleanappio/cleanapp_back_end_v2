package disburse

import (
	"cleanapp/common"
	"cleanapp/pipelines/disburse/contract"
	"context"
	"crypto/ecdsa"
	"database/sql"
	"flag"
	"fmt"
	"math/big"

	"github.com/apex/log"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	gasLimit = uint64(300000)
	validity = int64(60)
)

var (
	ethNetworkAddress = flag.String("eth_network_address", "", "Ethereum network address.")
	privateKey        = flag.String("eth_private_key", "", "The private key for connecting to the smart contract.")
	contractAddress   = flag.String("contract_address", "", "The contract address in HEX")
)

type Disburser struct {
	db          *sql.DB
	client      *ethclient.Client
	chainID     *big.Int
	privateKey  *ecdsa.PrivateKey
	fromAddress ethcommon.Address
	contract    *contract.KitnDisbursement
}

func NewDisburser(db *sql.DB) (*Disburser, error) {
	d := &Disburser{}

	client, err := ethclient.Dial(*ethNetworkAddress)
	if err != nil {
		return nil, err
	}
	d.client = client
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return nil, err
	}
	d.chainID = chainID

	d.privateKey, err = crypto.HexToECDSA(*privateKey)
	if err != nil {
		return nil, err
	}

	publicKey := d.privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("error creating ECDSA public key from %v", publicKey)
	}

	d.fromAddress = crypto.PubkeyToAddress(*publicKeyECDSA)
	d.contract, err = contract.NewKitnDisbursement(ethcommon.HexToAddress(*contractAddress), client)
	if err != nil {
		return nil, err
	}

	return d, nil
}

func (d *Disburser) Disburse() (int, int, error) {
	// Go through all users, get tokens to be disbursed.
	rows, err := d.db.Query(`
	  SELECT id, kitns_daily
	  FROM users
	  WHERE kitns_daily > 0
	`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	successRows := 0
	failRows := 0
	for rows.Next() {
		var (
			id         string
			referral   string
			dailyKitns int
		)
		if err := rows.Scan(&id, &referral, &dailyKitns); err != nil {
			log.Errorf("Cannot scan a row: %w", err)
			failRows += 1
			continue
		}
		log.Infof("Disbursing %d kitns to the user %s", dailyKitns, id)

		if err := d.disburseOneUser(id, dailyKitns); err != nil {
			log.Errorf("Error while disbursing %d tokens to the user %s: %w", dailyKitns, id, err)
			failRows += 1
			continue
		}
		result, err := d.db.Exec(`
			UPDATE users
			SET kitns_daily = 0, kitns_disbursed = kitns_disbursed + ?
			WHERE id = ?
		`, dailyKitns, id)
		if err != nil {
			common.LogResult("Error while updating disbursed tokens for the user", result, err)
		}

		successRows += 1
	}

	return successRows, failRows, nil
}

func (d *Disburser) disburseOneUser(toAddress string, dailyKitns int) error {
	nonce, err := d.client.PendingNonceAt(context.Background(), d.fromAddress)
	if err != nil {
		return err
	}

	gasPrice, err := d.client.SuggestGasPrice(context.Background())
	if err != nil {
		return err
	}

	auth, err := bind.NewKeyedTransactorWithChainID(d.privateKey, d.chainID)
	if err != nil {
		return err
	}
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0) // in wei
	auth.GasLimit = gasLimit   // in units
	auth.GasPrice = gasPrice

	amount := big.NewInt(int64(dailyKitns))
	_, err = d.contract.RenewAllowance(auth, d.fromAddress, amount, big.NewInt(validity))
	if err != nil {
		return err
	}
	tx, err := d.contract.SpendCoins(auth, ethcommon.HexToAddress(toAddress), amount)
	if err != nil {
		return err
	}
	log.Infof("Transaction sent: %v", tx)

	return nil
}
