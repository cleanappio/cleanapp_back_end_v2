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

func TestFromWei(t *testing.T) {
	it(func() {
		tests := []struct {
			src      *big.Int
			expected float32
		}{
			{
				src:      makeWei("120000000000000000000"),
				expected: 120.0,
			}, {
				src:      makeWei("7250000000000000000"),
				expected: 7.25,
			}, {
				src:      makeWei("0"),
				expected: 0.0,
			},
		}
		for _, test := range tests {
			if res := fromWei(test.src); res != test.expected {
				t.Errorf("fromWei(%v): want %v, got %v", test.src, test.expected, res)
			}
		}
	})
}

func TestToWei(t *testing.T) {
	it(func() {
		tests := []struct {
			src      float32
			expected *big.Int
		}{
			{
				src:      10000.0,
				expected: big.NewInt(0).Mul(big.NewInt(10000), big.NewInt(1e18)),
			}, {
				src:      12.345,
				expected: big.NewInt(0).Mul(big.NewInt(12345), big.NewInt(1e15)),
			}, {
				src:      0.0,
				expected: big.NewInt(0),
			},
		}
		for _, test := range tests {
			if res := toWei(test.src); res.Cmp(test.expected) != 0 {
				t.Errorf("toWei(%v): want %v, got %v", test.src, test.expected, res)
			}
		}
	})
}

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
				"UPDATE users	SET kitns_daily = kitns_daily - (.+), kitns_daily_ref = kitns_daily_ref - (.+), kitns_disbursed = kitns_disbursed \\+ (.+), kitns_disbursed_ref = kitns_disbursed_ref \\+ (.+) WHERE id = ?").
				WithArgs(test.expectedDaily, test.expectedDailyRef, test.expectedDaily, test.expectedDailyRef, test.address.String()).
				WillReturnResult(sqlmock.NewResult(1, 1))

			d := Disburser{
				db: db,
			}
			if err := d.updateDisbursed(test.address, test.daily, test.dailyRef); err != nil {
				t.Errorf("updateDisbursed(%v, %v, %v): got an error %v", test.address, test.daily, test.dailyRef, err)
			}
		}
	})
}
