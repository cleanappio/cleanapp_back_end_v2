package disburse

import (
	"cleanapp/common"
	commondisburse "cleanapp/common/disburse"
	"database/sql"
	"flag"
	"fmt"
	"math/big"

	"github.com/apex/log"
	ethcommon "github.com/ethereum/go-ethereum/common"
)

const (
	gasLimit  = uint64(0)
	batchSize = 100
)

var (
	ethNetworkUrl   = flag.String("eth_network_url", "", "Ethereum network address.")
	privateKey      = flag.String("eth_private_key", "", "The private key for connecting to the smart contract.")
	contractAddress = flag.String("contract_address", "", "The contract address in HEX.")
	usersTable      = flag.String("users_table", "users", "The name of the users table.")
)

type DailyDisburser struct {
	db        *sql.DB
	disburser *commondisburse.Disburser
}

type KitnRefs struct {
	kitn    *big.Int
	kitnref *big.Int
}

func NewDailyDisburser(db *sql.DB) (*DailyDisburser, error) {
	disburser, err := commondisburse.NewDisburser(*ethNetworkUrl, *privateKey, *contractAddress)
	if err != nil {
		return nil, err
	}
	return &DailyDisburser{
		db: db,
		disburser: disburser,
	}, nil
}

func (d *DailyDisburser) Disburse() error {
	// Go through all users, get tokens to be disbursed.
	rows, err := d.db.Query(fmt.Sprintf(`
	  SELECT id, kitns_daily, kitns_ref_daily
	  FROM %s
	  WHERE id != '' AND (kitns_daily > 0 OR kitns_ref_daily > 0.0)
	`, *usersTable))
	if err != nil {
		return err
	}
	defer rows.Close()

	batchKitns := make(map[ethcommon.Address]*big.Int)
	batchKitnsRef := make(map[ethcommon.Address]*KitnRefs)
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
		batchKitns[ethcommon.HexToAddress(id)] = commondisburse.ToWei(float32(dailyKitns) + dailyRefKitns)
		batchKitnsRef[ethcommon.HexToAddress(id)] = &KitnRefs{
			kitn:    commondisburse.ToWei(float32(dailyKitns)),
			kitnref: commondisburse.ToWei(dailyRefKitns),
		}

		currIdx += 1

		if currIdx >= batchSize {
			succeeded, err := d.disburser.DisburseBatch(batchKitns)
			if err != nil {
				return err
			}
			for _, addr := range succeeded {
				if kitns, ok := batchKitnsRef[addr]; ok {
					d.updateDisbursed(addr, kitns.kitn, kitns.kitnref)
				}
			}
			batchKitns = make(map[ethcommon.Address]*big.Int)
			batchKitnsRef = make(map[ethcommon.Address]*KitnRefs)
			currIdx = 0
		}
	}
	if currIdx > 0 {
		succeeded, err := d.disburser.DisburseBatch(batchKitns)
		if err != nil {
			return err
		}
		for _, addr := range succeeded {
			if kitns, ok := batchKitnsRef[addr]; ok {
				d.updateDisbursed(addr, kitns.kitn, kitns.kitnref)
			}
		}
	}

	return nil
}

func (d *DailyDisburser) updateDisbursed(address ethcommon.Address, daily, dailyRef *big.Int) error {
	kitnsDaily := int(commondisburse.FromWei(daily))
	kitnsDailyRef := commondisburse.FromWei(dailyRef)
	res, err := d.db.Exec(fmt.Sprintf(`
		UPDATE %s
		SET
			kitns_daily = kitns_daily - ?,
			kitns_ref_daily = kitns_ref_daily - ?,
			kitns_disbursed = kitns_disbursed + ?,
			kitns_ref_disbursed = kitns_ref_disbursed + ?
		WHERE id = ?`, *usersTable),
		kitnsDaily,
		kitnsDailyRef,
		kitnsDaily,
		kitnsDailyRef,
		address.String())
	if err != nil {
		return err
	}
	common.LogResult(fmt.Sprintf("Update %d + %f disbursed kitns for %s", kitnsDaily, kitnsDailyRef, address.String()), res, err, true)
	return nil
}
