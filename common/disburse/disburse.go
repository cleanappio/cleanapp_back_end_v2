package disburse

import (
	"cleanapp/common/disburse/contract"
	"context"
	"crypto/ecdsa"
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

func FromWei(src *big.Int) float32 {
	res, _ := decimal.NewFromBigInt(src, -18).Float64()
	return float32(res)
}

func ToWei(src float32) *big.Int {
	srcDec := decimal.NewFromFloat32(src)
	weiInt := big.NewInt(0).Mul(srcDec.Coefficient(), big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(int32(18)+srcDec.Exponent())), nil))
	return weiInt
}

type Disburser struct {
	client          *ethclient.Client
	chainID         *big.Int
	privateKey      *ecdsa.PrivateKey
	fromAddress     ethcommon.Address
	contractAddress ethcommon.Address
	contract        *contract.KitnDisbursement
	header          *types.Header
}

func NewDisburser(ethNetworkUrl, privateKey, contractAddress string) (*Disburser, error) {
	d := &Disburser{}

	client, err := ethclient.Dial(ethNetworkUrl)
	if err != nil {
		return nil, fmt.Errorf("error creating ethclient with the network url %s: %w", ethNetworkUrl, err)
	}

	d.client = client
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error getting network ID: %w", err)
	}
	d.chainID = chainID

	if len(privateKey) == 0 {
		return nil, fmt.Errorf("the eth_private_key key param isn't specified")
	}
	d.privateKey, err = crypto.HexToECDSA(privateKey)
	if err != nil {
		return nil, fmt.Errorf("error converting private key: %w", err)
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

func (d *Disburser) DisburseBatch(kitns map[ethcommon.Address]*big.Int) ([]ethcommon.Address, error) {
	nonce, err := d.client.PendingNonceAt(context.Background(), d.fromAddress)
	if err != nil {
		return nil, err
	}

	gasPrice, err := d.client.SuggestGasPrice(context.Background())
	if err != nil {
		return nil, err
	}

	auth, err := bind.NewKeyedTransactorWithChainID(d.privateKey, d.chainID)
	if err != nil {
		return nil, err
	}
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0) // in wei
	auth.GasLimit = gasLimit   // in units
	auth.GasPrice = gasPrice

	// Prepare a list of addresses and amounts to send to the disbursement contract
	addresses := []ethcommon.Address{}
	amounts := []*big.Int{}
	totalAmount := big.NewInt(0)
	log.Info("=== Disbursing tokens:")
	for k, v := range kitns {
		log.Infof("%v: %f", k, FromWei(v))
		addresses = append(addresses, k)
		amounts = append(amounts, v)
		totalAmount.Add(totalAmount, v)
	}

	// Check if the contract has enough funds for disbursing the amount in the batch
	// Requires funds an=mount equal to the total amount plus 10 KITN
	requiredAmount := big.NewInt(0).Add(totalAmount, ToWei(10.0))
	balance, err := d.contract.GetKitnBalance(&bind.CallOpts{})
	if err != nil {
		return nil, fmt.Errorf("error getting contract balance: %w", err)
	}
	if requiredAmount.Cmp(balance) == 1 {
		return nil, fmt.Errorf("not enough funds on the contract: %v, required at least %v", balance, requiredAmount)
	}

	// Do disbursement
	tx, err := d.contract.SpendCoins(auth, addresses, amounts)
	if err != nil {
		return nil, fmt.Errorf("call contract spend coins: %w", err)
	}
	log.Infof("Transaction %s", tx.Hash().String())

	// Getting transaction events
	filterer, err := contract.NewKitnDisbursementFilterer(d.contractAddress, d.client)
	if err != nil {
		return nil, fmt.Errorf("create events filterer: %w", err)
	}

	filterOpts := bind.FilterOpts{
		Start:   d.header.Number.Uint64(),
		Context: context.Background(),
	}

	succeeded := []ethcommon.Address{}
	// Do several iterations until transaction events are available.
	incomplete := true
	for incomplete {
		f, err := filterer.FilterCoinsSpent(&filterOpts)
		if err != nil {
			return nil, fmt.Errorf("filter events: %w", err)
		}

		for f.Next() {
			if f.Event.Raw.TxHash == tx.Hash() {
				incomplete = false
				succeedCnt, failCnt := 0, 0
				succeedKitns, failKitns := float32(0.0), float32(0.0)
				for _, r := range f.Event.Results {
					if r.Result {
						if _, ok := kitns[r.Receiver]; ok {
							succeeded = append(succeeded, r.Receiver)
							succeedCnt += 1
							succeedKitns += FromWei(r.Amount)
						} else {
							return nil, fmt.Errorf("inconsistent data structure, amount for %v not found", r.Receiver)
						}
					} else {
						log.Errorf("Failed disbursing %f tokens to %v", FromWei(r.Amount), r.Receiver)
						failCnt += 1
						failKitns += FromWei(r.Amount)
					}
				}
				log.Infof(
					"=== Dusbursement completed. Succeeded: %d addresses, %f KITNs; failed: %d addresses, %f KITNs",
					succeedCnt, succeedKitns, failCnt, failKitns)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return succeeded, nil
}
