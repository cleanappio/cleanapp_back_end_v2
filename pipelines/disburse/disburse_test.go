package disburse

import (
	"database/sql"
	"math/big"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jknair0/beforeeach"
)

var (
	db   *sql.DB
	mock sqlmock.Sqlmock
)

func setUp() {
	db, mock, _ = sqlmock.New()
}

func tearDown() {
	db.Close()
}

func makeWei(arg string) *big.Int {
	res, _ := big.NewInt(1).SetString(arg, 10)
	return res
}

var it = beforeeach.Create(setUp, tearDown)

func TestUpdateDisbursed(t *testing.T) {
	it(func() {
		tests := []struct {
			address          ethcommon.Address
			daily            *big.Int
			dailyRef         *big.Int
			expectedDaily    int
			expectedDailyRef float32
		}{
			{
				address:          ethcommon.HexToAddress("0xE8790e5AF794E2Db8e1517B0700B33cAE580f119"),
				daily:            makeWei("100000000000000000000"),
				dailyRef:         makeWei("7250000000000000000"),
				expectedDaily:    100,
				expectedDailyRef: 7.25,
			},
		}
		for _, test := range tests {
			mock.ExpectExec(
				"UPDATE users	SET kitns_daily = kitns_daily - (.+), kitns_ref_daily = kitns_ref_daily - (.+), kitns_disbursed = kitns_disbursed \\+ (.+), kitns_ref_disbursed = kitns_ref_disbursed \\+ (.+) WHERE id = (.+)").
				WithArgs(test.expectedDaily, test.expectedDailyRef, test.expectedDaily, test.expectedDailyRef, test.address.String()).
				WillReturnResult(sqlmock.NewResult(1, 1))

			d := DailyDisburser{
				db: db,
			}
			if err := d.updateDisbursed(test.address, test.daily, test.dailyRef); err != nil {
				t.Errorf("updateDisbursed(%v, %v, %v): got an error %v", test.address, test.daily, test.dailyRef, err)
			}
		}
	})
}
