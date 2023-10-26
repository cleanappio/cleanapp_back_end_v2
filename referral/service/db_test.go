package service

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jknair0/beforeeach"
)

var (
	refDB *referralDB
	mock  sqlmock.Sqlmock
)

func setUp() {
	var db *sql.DB
	db, mock, _ = sqlmock.New()
	refDB = &referralDB{db}
}

func tearDown() {
	refDB.db.Close()
}

var it = beforeeach.Create(setUp, tearDown)

func TestReadReferral(t *testing.T) {
	it(func() {
		testCases := []struct {
			name      string
			refKey    string
			refValues []string

			expectedValue string
			errorExpected bool
		}{
			{
				name:      "Found referral",
				refKey:    "192.168.0.34:300:670",
				refValues: []string{"abcdef"},

				expectedValue: "abcdef",
				errorExpected: false,
			},
			{
				name:      "Referral not found",
				refKey:    "192.168.0.34:300:670",
				refValues: []string{},

				expectedValue: "",
				errorExpected: false,
			},
			{
				name:      "Fetch Error",
				refKey:    "192.168.0.34:300:670",
				refValues: []string{},

				errorExpected: true,
			},
		}

		columns := []string{
			"refvalue",
		}
		for _, testCase := range testCases {
			if testCase.errorExpected {
				mock.ExpectQuery("SELECT refvalue	FROM referrals WHERE refkey = (.+)").WithArgs(testCase.refKey).
					WillReturnError(fmt.Errorf("test fetch error"))
			} else {
				mock.ExpectQuery("SELECT refvalue	FROM referrals WHERE refkey = (.+)").WithArgs(testCase.refKey).
					WillReturnRows(sqlmock.NewRows(columns).
						FromCSVString(strings.Join(testCase.refValues, "\n")))
			}

			refvalue, err := refDB.ReadReferral(testCase.refKey)
			if testCase.errorExpected != (err != nil) {
				t.Errorf("%s, refDB.ReadReferral: expected error: %v, got error: %e", testCase.name, testCase.errorExpected, err)
			}
			if refvalue != testCase.expectedValue {
				t.Errorf("%s, refDB.ReadReferral: expected %s, got %s", testCase.name, testCase.expectedValue, refvalue)
			}
		}
	})
}

func TestWriteReferral(t *testing.T) {
	it(func() {
		testCases := []struct {
			name      string
			refKey    string
			refValue  string
			refExists bool

			errorExpected bool
		}{
			{
				name:      "New referral",
				refKey:    "192.168.0.34:300:670",
				refValue:  "abcdef",
				refExists: false,

				errorExpected: false,
			},
			{
				name:      "Existing referral",
				refKey:    "192.168.0.34:300:670",
				refValue:  "abcdef",
				refExists: true,

				errorExpected: false,
			},
			{
				name:      "Exec Error",
				refKey:    "192.168.0.34:300:670",
				refValue:  "abcdef",
				refExists: false,

				errorExpected: true,
			},
		}

		columns := []string{
			"refvalue",
		}
		for _, testCase := range testCases {
			if testCase.refExists {
				mock.ExpectQuery("SELECT refvalue	FROM referrals WHERE refkey = (.+)").WithArgs(testCase.refKey).
					WillReturnRows(sqlmock.NewRows(columns).
						FromCSVString(testCase.refValue))
			} else {
				mock.ExpectQuery("SELECT refvalue	FROM referrals WHERE refkey = (.+)").WithArgs(testCase.refKey).
					WillReturnRows(sqlmock.NewRows(columns).
						FromCSVString(""))
			}

			if !testCase.refExists {
				if testCase.errorExpected {
					mock.ExpectExec("INSERT INTO referrals \\(refkey, refvalue\\) VALUES (.+)").
						WithArgs(testCase.refKey, testCase.refValue).
						WillReturnError(fmt.Errorf("update test error"))
				} else {
					mock.ExpectExec("INSERT INTO referrals \\(refkey, refvalue\\) VALUES (.+)").
						WithArgs(testCase.refKey, testCase.refValue).
						WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}

			if err := refDB.WriteReferral(testCase.refKey, testCase.refValue); testCase.errorExpected != (err != nil) {
				t.Errorf("%s, refDB.WriteReferral: expected error: %v, got error: %e", testCase.name, testCase.errorExpected, err)
			}
		}
	})
}
