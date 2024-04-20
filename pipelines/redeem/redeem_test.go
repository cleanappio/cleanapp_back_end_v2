package redeem

import (
	"database/sql"
	"fmt"
	"strings"
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

var it = beforeeach.Create(setUp, tearDown)

type ref struct {
	parentId    string
	refcode     string
	id          string
	kitns       int
	award       float32
	expectError bool
	nextRef     *ref
}

func TestRedeem(t *testing.T) {
	it(func() {
		testCases := []struct {
			name     string
			usersRet []string
			refs     []ref

			expectGeneralError bool
			expectSuccessCount int
			expectFailCount    int
		}{
			{
				name: "Standard referral chain",
				usersRet: []string{
					"0x123,aabbcc,10",
					"0x456,ddeeff,20",
				},
				refs: []ref{
					{
						parentId:    "0x123",
						refcode:     "aabbcc",
						id:          "0x111",
						kitns:       10,
						award:       1.0,
						expectError: false,
						nextRef: &ref{
							refcode:     "gghhii",
							id:          "0x333",
							award:       0.5,
							expectError: false,
						},
					}, {
						parentId:    "0x456",
						refcode:     "ddeeff",
						id:          "0x222",
						kitns:       20,
						award:       2.0,
						expectError: false,
						nextRef: &ref{
							refcode:     "gghhii",
							id:          "0x333",
							award:       1.0,
							expectError: false,
						},
					},
				},
				expectGeneralError: false,
				expectSuccessCount: 2,
				expectFailCount:    0,
			}, {
				name: "Referral chain with error",
				usersRet: []string{
					"0x123,aabbcc,10",
					"0x456,ddeeff,20",
				},
				refs: []ref{
					{
						parentId:    "0x123",
						refcode:     "aabbcc",
						id:          "0x111",
						kitns:       10,
						award:       1.0,
						expectError: false,
						nextRef: &ref{
							refcode:     "gghhii",
							id:          "0x333",
							award:       0.5,
							expectError: true,
						},
					}, {
						parentId:    "0x456",
						refcode:     "ddeeff",
						id:          "0x222",
						kitns:       20,
						award:       2.0,
						expectError: false,
						nextRef: &ref{
							refcode:     "gghhii",
							id:          "0x333",
							award:       1.0,
							expectError: false,
						},
					},
				},
				expectGeneralError: false,
				expectSuccessCount: 1,
				expectFailCount:    1,
			},
		}

		selectUsersColumns := []string{
			"id",
			"referral",
			"kitns_to_refer",
		}
		selectRefsColumns := []string{
			"id",
		}
		selectNextRefColumns := []string{
			"referral",
		}

		for _, testCase := range testCases {
			setUp()
			if testCase.expectGeneralError {
				mock.ExpectQuery("SELECT id, referral, kitns_daily \\+ kitns_disbursed - kitns_ref_redeemed AS kitns_to_refer	FROM users WHERE referral != ''	AND kitns_daily \\+ kitns_disbursed - kitns_ref_redeemed > 0").
					WillReturnError(fmt.Errorf("General error"))
			} else {
				mock.ExpectQuery("SELECT id, referral, kitns_daily \\+ kitns_disbursed - kitns_ref_redeemed AS kitns_to_refer	FROM users WHERE referral != ''	AND kitns_daily \\+ kitns_disbursed - kitns_ref_redeemed > 0").
					WillReturnRows(
						sqlmock.NewRows(selectUsersColumns).
							FromCSVString(strings.Join(testCase.usersRet, "\n")))
				for _, ref := range testCase.refs {
					mock.ExpectBegin()
					hasStepError := false
					for nextRef := &ref; nextRef != nil; nextRef = nextRef.nextRef {
						mock.ExpectQuery("SELECT id	FROM users_refcodes	WHERE referral = (.+)").
							WithArgs(nextRef.refcode).
							WillReturnRows(
								sqlmock.NewRows(selectRefsColumns).
									FromCSVString(nextRef.id))
						if nextRef.expectError {
							mock.ExpectExec(
								"UPDATE users	SET kitns_ref_daily = kitns_ref_daily \\+ (.+) WHERE id = (.+)").
								WithArgs(nextRef.award, nextRef.id).
								WillReturnError(fmt.Errorf("Update error"))
							mock.ExpectRollback()
							hasStepError = true
							break
						} else {
							mock.ExpectExec(
								"UPDATE users	SET kitns_ref_daily = kitns_ref_daily \\+ (.+) WHERE id = (.+)").
								WithArgs(nextRef.award, nextRef.id).
								WillReturnResult(sqlmock.NewResult(1, 1))
							nextRefCode := ""
							if nextRef.nextRef != nil {
								nextRefCode = nextRef.nextRef.refcode
							}
							mock.ExpectQuery("SELECT referral	FROM users WHERE id = (.+)").
								WithArgs(nextRef.id).
								WillReturnRows(
									sqlmock.NewRows(selectNextRefColumns).
										FromCSVString(nextRefCode))
						}
					}
					if !hasStepError {
						mock.ExpectExec(
							"UPDATE users	SET kitns_ref_redeemed = kitns_ref_redeemed \\+ (.+) WHERE id = (.+)").
							WithArgs(ref.kitns, ref.parentId).
							WillReturnResult(sqlmock.NewResult(1, 1))
						mock.ExpectCommit()
					}
				}
			}

			successCount, failCount, err := Redeem(db)
			if testCase.expectGeneralError != (err != nil) {
				t.Errorf("%s, Redeem(): expected error: %v, got error: %v", testCase.name, testCase.expectGeneralError, err)
			}
			if failCount != testCase.expectFailCount {
				t.Errorf("%s, Redeem(): expected failures: %d, got failures: %d", testCase.name, testCase.expectFailCount, failCount)
			}
			if successCount != testCase.expectSuccessCount {
				t.Errorf("%s, Redeem(): expected succeeded: %d, got succeeded: %d", testCase.name, testCase.expectSuccessCount, successCount)
			}
		}
	})
}
