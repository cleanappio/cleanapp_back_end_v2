package disburse

import (
	"cleanapp/common"
	"cleanapp/pipelines/disburse/contract"
	"context"
	"crypto/ecdsa"
	"database/sql"
	"fmt"
	"math/big"
	"time"

	"github.com/apex/log"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/shopspring/decimal"
)

const (
	gasLimit  = uint64(300000)
	batchSize = 100
)

type Disburser struct {
	db              *sql.DB
	client          *ethclient.Client
	chainID         *big.Int
	privateKey      *ecdsa.PrivateKey
	fromAddress     ethcommon.Address
	contractAddress ethcommon.Address
	contract        *contract.KitnDisbursement
	header          *types.Header
}

func NewDisburser(db *sql.DB, client *ethclient.Client, privateKey, contractAddress string) (*Disburser, error) {
	d := &Disburser{db: db}

	d.client = client
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return nil, err
	}
	d.chainID = chainID

	d.privateKey, err = crypto.HexToECDSA(privateKey)
	if err != nil {
		return nil, err
	}

	publicKey := d.privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("error creating ECDSA public key from %v", publicKey)
	}

	d.fromAddress = crypto.PubkeyToAddress(*publicKeyECDSA)
	d.contractAddress = ethcommon.HexToAddress(contractAddress)
	d.contract, err = contract.NewKitnDisbursement(d.contractAddress, client)
	if err != nil {
		return nil, err
	}

	return d, nil
}

type Kitns struct {
	daily    *big.Int
	dailyRef *big.Int
}

func (d *Disburser) Disburse() error {
	// Go through all users, get tokens to be disbursed.
	rows, err := d.db.Query(`
	  SELECT id, kitns_daily, kitns_ref_daily
	  FROM users
	  WHERE kitns_daily > 0
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	batchKitns := make(map[ethcommon.Address]Kitns)
	currIdx := 0
	for rows.Next() {
		var (
			id            string
			dailyKitns    int
			dailyRefKitns float32
		)
		if err := rows.Scan(&id, &dailyKitns, &dailyRefKitns); err != nil {
			log.Errorf("Cannot scan a row: %w", err)
			continue
		}

		batchKitns[ethcommon.HexToAddress(id)] = Kitns{toWei(float32(dailyKitns)), toWei(dailyRefKitns)}

		currIdx += 1

		if currIdx >= batchSize {
			d.disburseBatch(batchKitns)
			batchKitns = make(map[ethcommon.Address]Kitns)
			currIdx = 0
		}
	}
	if currIdx > 0 {
		d.disburseBatch(batchKitns)
	}

	return nil
}

func fromWei(src *big.Int) float32 {
	res, _ := decimal.NewFromBigInt(src, -18).Float64()
	return float32(res)
}

func toWei(src float32) *big.Int {
	srcDec := decimal.NewFromFloat32(src)
	weiInt := big.NewInt(0).Mul(srcDec.Coefficient(), big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(int32(18)+srcDec.Exponent())), nil))
	return weiInt
}

func (d *Disburser) disburseBatch(kitns map[ethcommon.Address]Kitns) error {
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

	// Prepare a list of addresses and amounts to send to the disbursement contract
	addresses := make([]ethcommon.Address, len(kitns))
	amounts := make([]*big.Int, len(kitns))
	totalAmount := big.NewInt(0)
	for k, v := range kitns {
		addresses = append(addresses, k)
		amounts = append(amounts, big.NewInt(0).Add(v.daily, v.dailyRef))
		totalAmount.Add(totalAmount, v.daily)
		totalAmount.Add(totalAmount, v.dailyRef)
	}

	// Check if the contract has enough funds for disbursing the amount in the batch
	// Requires funds an=mount equal to the total amount plus 10 KITN
	requiredAmount := big.NewInt(0).Add(totalAmount, toWei(10.0))
	balance, err := d.contract.GetKitnBalance(&bind.CallOpts{})
	if err != nil {
		return fmt.Errorf("error getting contract balance: %w", err)
	}
	if requiredAmount.Cmp(balance) == 1 {
		return fmt.Errorf("not enough funds on the contract: %v, required at least %v", balance, requiredAmount)
	}

	// Do disbursement
	tx, err := d.contract.SpendCoins(auth, addresses, amounts)
	if err != nil {
		return fmt.Errorf("call contract spend coins: %w", err)
	}
	log.Infof("Transaction %s", tx.Hash().String())

	// Getting transaction events
	filterer, err := contract.NewKitnDisbursementFilterer(d.contractAddress, d.client)
	if err != nil {
		return fmt.Errorf("create events filterer: %w", err)
	}

	filterOpts := bind.FilterOpts{
		Start:   d.header.Number.Uint64(),
		Context: context.Background(),
	}

	// Do several iterations until transaction events are available.
	incomplete := true
	for incomplete {
		f, err := filterer.FilterCoinsSpent(&filterOpts)
		if err != nil {
			return fmt.Errorf("filter events: %w", err)
		}

		for f.Next() {
			if f.Event.Raw.TxHash == tx.Hash() {
				incomplete = false
				for _, r := range f.Event.Results {
					if r.Result {
						if k, ok := kitns[r.Receiver]; ok {
							d.updateDisbursed(r.Receiver, k.daily, k.dailyRef)
						}
					}
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

func (d *Disburser) updateDisbursed(address ethcommon.Address, daily, dailyRef *big.Int) error {
	kitnsDaily := int(fromWei(daily))
	kitnsDailyRef := fromWei(dailyRef)
	res, err := d.db.Exec(`
		UPDATE users
		SET
			kitns_daily = kitns_daily - ?,
			kitns_daily_ref = kitns_daily_ref - ?,
			kitns_disbursed = kitns_disbursed + ?,
			kitns_disbursed_ref = kitns_disbursed_ref + ?
		WHERE id = ?`,
		kitnsDaily,
		kitnsDailyRef,
		kitnsDaily,
		kitnsDailyRef,
		address.String())
	if err != nil {
		return err
	}
	common.LogResult(fmt.Sprintf("Update %d + %f disbursed kitns for %s", kitnsDaily, kitnsDailyRef, address.String()), res, err)
	return nil
}
