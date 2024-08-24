package disburse

import (
	"database/sql"
	"math/big"
	"testing"

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
			if res := FromWei(test.src); res != test.expected {
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
			if res := ToWei(test.src); res.Cmp(test.expected) != 0 {
				t.Errorf("toWei(%v): want %v, got %v", test.src, test.expected, res)
			}
		}
	})
}
