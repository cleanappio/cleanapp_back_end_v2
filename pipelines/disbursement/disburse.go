package disbursement

import (
	"cleanapp/pipelines/disbursement/contract"
	"context"
	"crypto/ecdsa"
	"database/sql"
	"flag"

	"github.com/apex/log"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	ethNetworkAddress = flag.String("eth_network_address", "", "Ethereum network address.")
	privateKey        = flag.String("eth_private_key", "", "The private key for connecting to the smart contract.")
)

func Disburse(db *sql.DB) (int, int, error) {
	// Create the Ethereum client
	client, err := ethclient.Dial(*ethNetworkAddress)
	if err != nil {
		return 0, 0, err
	}

	privateKey, err := crypto.HexToECDSA(*privateKey)
	if err != nil {
		return 0, 0, err
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return 0, 0, err
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		return 0, 0, err
	}

	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		return 0, 0, err
	}

	contract := bind.NewKeyedTransactorWithChainID()

	// Go through all users, get tokens to be disbursed.
	rows, err := db.Query(`
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

		if err := redeemOneUser(db, id, referral, kitnsToRefer); err != nil {
			log.Errorf("Error while redeeming the user %s: %w", id, err)
			failRows += 1
			continue
		}
		successRows += 1
	}

	return successRows, failRows, nil

}
