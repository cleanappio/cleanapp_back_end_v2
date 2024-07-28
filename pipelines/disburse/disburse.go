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
	gasLimit  = uint64(0)
	batchSize = 100
)

var (
	ethNetworkUrl   = flag.String("eth_network_url", "", "Ethereum network address.")
	privateKey      = flag.String("eth_private_key", "", "The private key for connecting to the smart contract.")
	contractAddress = flag.String("contract_address", "", "The contract address in HEX")
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

func NewDisburser(db *sql.DB) (*Disburser, error) {
	d := &Disburser{db: db}

	client, err := ethclient.Dial(*ethNetworkUrl)
	if err != nil {
		return nil, fmt.Errorf("error creating ethclient with the network url %s: %w", *ethNetworkUrl, err)
	}

	d.client = client
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error getting network ID: %w", err)
	}
	d.chainID = chainID

	if (len(*privateKey) == 0) {
		return nil, fmt.Errorf("the eth_private_key key param isn't specified")
	}
	d.privateKey, err = crypto.HexToECDSA(*privateKey)
	if err != nil {
		return nil, fmt.Errorf("error converting private key: %w", err)
	}

	publicKey := d.privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("error creating ECDSA public key from %v", publicKey)
	}

	d.fromAddress = crypto.PubkeyToAddress(*publicKeyECDSA)
	d.contractAddress = ethcommon.HexToAddress(*contractAddress)
	d.contract, err = contract.NewKitnDisbursement(d.contractAddress, client)
	if err != nil {
		return nil, fmt.Errorf("error creating the contract interface: %w", err)
	}

	h, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("error getting header block: %w", err)
	}
	d.header = h

	log.Infof("Disburser initialized, chain ID: %v, contract address: %v, contract owner: %v", d.chainID, d.contractAddress, d.fromAddress)

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
	  WHERE kitns_daily > 0 OR kitns_ref_daily > 0.0
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
		if err := d.disburseBatch(batchKitns); err != nil {
			return err
		}
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
	log.Infof("=== Disbursing tokens:\n%v", kitns)
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
	auth.Nonce = big.NewInt(int64(nonce + 1))
	auth.Value = big.NewInt(0) // in wei
	auth.GasLimit = gasLimit   // in units
	auth.GasPrice = gasPrice

	// Prepare a list of addresses and amounts to send to the disbursement contract
	addresses := []ethcommon.Address{}
	amounts := []*big.Int{}
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
				succeedCnt, failCnt := 0, 0
				succeedKitns, failKitns := float32(0.0), float32(0.0)
				for _, r := range f.Event.Results {
					if r.Result {
						if k, ok := kitns[r.Receiver]; ok {
							if err := d.updateDisbursed(r.Receiver, k.daily, k.dailyRef); err != nil {
								return fmt.Errorf("error updating disbursed KITNs: %w", err)
							}
							succeedCnt += 1
							succeedKitns += fromWei(r.Amount)
						} else {
							return fmt.Errorf("inconsistent data structure, amount for %v not found", r.Receiver)
						}
					} else {
						log.Errorf("Failed disbursing %f tokens to %v", fromWei(r.Amount), r.Receiver)
						failCnt += 1
						failKitns += fromWei(r.Amount)
					}
				}
				log.Infof(
					"=== Dusbursement completed. Succeeded: %d addresses, %f KITNs; failed: %d addresses, %f KITNs",
					succeedCnt, succeedKitns, failCnt, failKitns)
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
			kitns_ref_daily = kitns_ref_daily - ?,
			kitns_disbursed = kitns_disbursed + ?,
			kitns_ref_disbursed = kitns_ref_disbursed + ?
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
