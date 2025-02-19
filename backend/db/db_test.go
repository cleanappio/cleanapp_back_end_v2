package db

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"cleanapp/backend/server/api"
	"cleanapp/backend/util"
	"cleanapp/common/disburse"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
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

func testRefGen() string {
	return "testrefid"
}

var it = beforeeach.Create(setUp, tearDown)

func testTeamGen(string) util.TeamColor {
	return util.Blue
}

func TestUpdateOrCreateUser(t *testing.T) {
	it(func() {
		testCases := []struct {
			name        string
			version     string
			id          string
			avatar      string
			referral    string
			team        int
			initialKitn int

			retList      []string
			execExpected bool

			expectResponse *api.UserResp
			expectError    bool
		}{
			{
				name:        "New user",
				version:     "2.0",
				id:          "0x12345678",
				avatar:      "user1",
				referral:    "abcdef",
				team:        util.Blue,
				initialKitn: 1,

				retList:      []string{},
				execExpected: true,

				expectResponse: &api.UserResp{
					Team:      util.Blue,
					DupAvatar: false,
				},
				expectError: false,
			}, {
				name:        "Existing user",
				version:     "2.0",
				id:          "0x123456768",
				avatar:      "user1",
				referral:    "abcdef",
				team:        util.Blue,
				initialKitn: 0,

				retList:      []string{"0x123456768"},
				execExpected: true,

				expectError: false,
				expectResponse: &api.UserResp{
					Team:      util.Blue,
					DupAvatar: false,
				},
			}, {
				name:     "Duplicate avatar",
				version:  "2.0",
				id:       "0x123456768",
				avatar:   "user1",
				referral: "abcdef",
				team:     util.Blue,

				retList:      []string{"0x87654321"},
				execExpected: false,

				expectError: true,
				expectResponse: &api.UserResp{
					Team:      0,
					DupAvatar: true,
				},
			},
		}

		recordColumns := []string{"id"}
		for _, testCase := range testCases {
			setUp()
			mock.ExpectQuery("SELECT id FROM users WHERE avatar = (.+)").
				WithArgs(testCase.avatar).
				WillReturnRows(
					sqlmock.NewRows(recordColumns).
						FromCSVString(strings.Join(testCase.retList, "\n")))
			mock.ExpectQuery("SELECT id FROM users WHERE id = (.+)").
				WithArgs(testCase.id).
				WillReturnRows(
					sqlmock.NewRows(recordColumns).
						FromCSVString(strings.Join(testCase.retList, "\n")))
			if testCase.execExpected {
				mock.ExpectExec(
					"INSERT INTO users \\(id, avatar, referral, team, kitns_disbursed\\) VALUES \\((.+), (.+), (.+), (.+), (.+)\\) ON DUPLICATE KEY UPDATE avatar=(.+), referral=(.+), team=(.+)").
					WithArgs(testCase.id, testCase.avatar, testCase.referral, testCase.team, testCase.initialKitn, testCase.avatar, testCase.referral, testCase.team).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(
					"INSERT INTO users_shadow \\(id, avatar, referral, team, kitns_disbursed\\) VALUES \\((.+), (.+), (.+), (.+), (.+)\\) ON DUPLICATE KEY UPDATE avatar=(.+), referral=(.+), team=(.+)").
					WithArgs(testCase.id, testCase.avatar, testCase.referral, testCase.team, testCase.initialKitn, testCase.avatar, testCase.referral, testCase.team).
					WillReturnResult(sqlmock.NewResult(1, 1))
			}
			resp, err := CreateOrUpdateUser(db, &api.UserArgs{
				Version:  testCase.version,
				Id:       testCase.id,
				Avatar:   testCase.avatar,
				Referral: testCase.referral,
			}, testTeamGen, []*disburse.Disburser{})
			if testCase.expectError != (err != nil) {
				t.Errorf("%s, updateUser: expected error: %v, got error: %v", testCase.name, testCase.expectError, err)
			}
			if !reflect.DeepEqual(resp, testCase.expectResponse) {
				t.Errorf("%s, updateUser: expected %v, got %v", testCase.name, testCase.expectResponse, resp)
			}
		}
	})
}

func TestUpdateUserAction(t *testing.T) {
	it(func() {
		testCases := []struct {
			name     string
			version  string
			id       string
			actionId string

			rowsAffected int64

			errorExpected bool
		}{
			{
				name:     "Update user action success",
				version:  "2.0",
				id:       "0x123456768",
				actionId: "abcdef",

				rowsAffected: 1,

				errorExpected: false,
			},
			{
				name:     "Update user action error",
				version:  "2.0",
				id:       "0x123456768",
				actionId: "absdef",

				errorExpected: true,
			},
		}

		for _, testCase := range testCases {
			setUp()
			if !testCase.errorExpected {
				mock.ExpectExec("UPDATE users SET action_id = (.+) WHERE id = (.+)").
					WithArgs(testCase.actionId, testCase.id).
					WillReturnResult(sqlmock.NewResult(1, testCase.rowsAffected))
			} else {
				mock.ExpectExec("UPDATE users SET action_id = (.+) WHERE id = (.+)").
					WithArgs(testCase.actionId, testCase.id).
					WillReturnError(fmt.Errorf("Error updating user action"))
			}
			if err := UpdateUserAction(db, &api.UserActionArgs{
				Version:  testCase.version,
				Id:       testCase.id,
				ActionId: testCase.actionId,
			}); testCase.errorExpected != (err != nil) {
				t.Errorf("%s, UpdateUserAction: expected error: %v, got error: %v", testCase.name, testCase.errorExpected, err)
			}
		}
	})
}

func TestSaveReport(t *testing.T) {
	const (
		ERROR_NONE = iota
		ERROR_COMMIT_TRAN
		ERROR_UPDATE_USER
		ERROR_INSERT_REPORT
		ERROR_BEGIN_TRAN
	)
	it(func() {
		testCases := []struct {
			name string
			r    *api.ReportArgs

			expectError int
		}{
			{
				name: "Add report success",
				r: &api.ReportArgs{
					Version:  "2.0",
					Id:       "0x1234",
					ActionId: "abcdef",
					Latitude: 40.12345,
					Longitue: 8.12345,
					X:        0.5,
					Y:        0.5,
					Image:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
				},
				expectError: ERROR_NONE,
			},
			{
				name: "Add report begin transaction error",
				r: &api.ReportArgs{
					Version:  "2.0",
					Id:       "0x5678",
					ActionId: "abcdef",
					Latitude: 40.67890,
					Longitue: 8.67890,
					X:        0.1,
					Y:        0.1,
					Image:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
				},
				expectError: ERROR_BEGIN_TRAN,
			},
			{
				name: "Add report insert error",
				r: &api.ReportArgs{
					Version:  "2.0",
					Id:       "0x9012",
					ActionId: "abcdef",
					Latitude: 41.67890,
					Longitue: 9.67890,
					X:        0.2,
					Y:        0.2,
					Image:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
				},
				expectError: ERROR_INSERT_REPORT,
			},
			{
				name: "Add report user update error",
				r: &api.ReportArgs{
					Version:  "2.0",
					Id:       "0x3456",
					ActionId: "abcdef",
					Latitude: 42.67890,
					Longitue: 10.67890,
					X:        0.3,
					Y:        0.3,
					Image:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
				},
				expectError: ERROR_UPDATE_USER,
			},
			{
				name: "Add report commit transaction error",
				r: &api.ReportArgs{
					Version:  "2.0",
					Id:       "0x7890",
					ActionId: "abcdef",
					Latitude: 43.67890,
					Longitue: 11.67890,
					X:        0.4,
					Y:        0.4,
					Image:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
				},
				expectError: ERROR_COMMIT_TRAN,
			},
		}

		for _, testCase := range testCases {
			setUp()
			if testCase.expectError == ERROR_BEGIN_TRAN {
				mock.ExpectBegin().WillReturnError(fmt.Errorf("begin transaction error"))
			} else if testCase.expectError < ERROR_BEGIN_TRAN {
				mock.ExpectBegin()
			}
			if testCase.expectError == ERROR_INSERT_REPORT {
				mock.ExpectExec("INSERT	INTO reports \\(id, team, action_id, latitude, longitude, x, y, image\\)	VALUES \\((.+), (.+), (.+), (.+), (.+), (.+), (.+), (.+)\\)").
					WithArgs(
						testCase.r.Id,
						1,
						testCase.r.ActionId,
						testCase.r.Latitude,
						testCase.r.Longitue,
						testCase.r.X,
						testCase.r.Y,
						testCase.r.Image).
					WillReturnError(fmt.Errorf("insert report error"))
			} else if testCase.expectError < ERROR_INSERT_REPORT {
				mock.ExpectExec("INSERT	INTO reports \\(id, team, action_id,  latitude, longitude, x, y, image\\)	VALUES \\((.+), (.+), (.+), (.+), (.+), (.+), (.+), (.+)\\)").
					WithArgs(
						testCase.r.Id,
						1,
						testCase.r.ActionId,
						testCase.r.Latitude,
						testCase.r.Longitue,
						testCase.r.X,
						testCase.r.Y,
						testCase.r.Image).
					WillReturnResult(sqlmock.NewResult(1, 1))
			}
			if testCase.expectError == ERROR_UPDATE_USER {
				mock.ExpectExec("UPDATE users SET kitns_daily \\= kitns_daily \\+ 1 WHERE id = (.+)").
					WithArgs(testCase.r.Id).
					WillReturnError(fmt.Errorf("update user error"))
			} else if testCase.expectError < ERROR_UPDATE_USER {
				mock.ExpectExec("UPDATE users SET kitns_daily \\= kitns_daily \\+ 1 WHERE id = (.+)").
					WithArgs(testCase.r.Id).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec("UPDATE users_shadow SET kitns_daily \\= kitns_daily \\+ 1 WHERE id = (.+)").
					WithArgs(testCase.r.Id).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			}
			if testCase.expectError == ERROR_COMMIT_TRAN {
				mock.ExpectCommit().WillReturnError(fmt.Errorf("error commit transaction"))
			} else if testCase.expectError < ERROR_COMMIT_TRAN {
				mock.ExpectCommit()
			}
			if err := SaveReport(db, testCase.r); (testCase.expectError == ERROR_NONE) != (err == nil) {
				t.Errorf("%s, saveReport: expected %v, got %v", testCase.name, testCase.expectError, err)
			}
		}
	})
}

func TestGetTopScores(t *testing.T) {
	it(func() {
		testCases := []struct {
			name         string
			base         *api.BaseArgs
			topN         int
			retList      []string
			youRet       string
			yourCnt      float64
			cntBeforeYou string

			expectResponse *api.TopScoresResponse
			expectError    bool
		}{
			{
				name: "You're in top",
				base: &api.BaseArgs{
					Version: "2.0",
					Id:      "0x1234",
				},
				topN: 3,
				retList: []string{
					"0x5678,Ava1,1095",
					"0x1234,AvaYou,1003",
					"0x9012,Ava3, 988",
				},

				expectResponse: &api.TopScoresResponse{
					Records: []api.TopScoresRecord{
						{
							Place: 1,
							Title: "Ava1",
							Kitn:  1095,
						}, {
							Place: 2,
							Title: "AvaYou",
							Kitn:  1003,
							IsYou: true,
						}, {
							Place: 3,
							Title: "Ava3",
							Kitn:  988,
						},
					},
				},
			}, {
				name: "You're not in top",
				base: &api.BaseArgs{
					Version: "2.0",
					Id:      "0x1234",
				},
				topN: 3,
				retList: []string{
					"0x5678,Ava1,1095",
					"0x7777,Ava2,1003",
					"0x9012,Ava3, 988",
				},
				youRet:       "0x1234,AvaYou,99",
				yourCnt:      99,
				cntBeforeYou: "49",

				expectResponse: &api.TopScoresResponse{
					Records: []api.TopScoresRecord{
						{
							Place: 1,
							Title: "Ava1",
							Kitn:  1095,
						}, {
							Place: 2,
							Title: "Ava2",
							Kitn:  1003,
						}, {
							Place: 3,
							Title: "Ava3",
							Kitn:  988,
						}, {
							Place: 50,
							Title: "AvaYou",
							Kitn:  99,
							IsYou: true,
						},
					},
				},
			}, {
				name: "Error in query",
				base: &api.BaseArgs{
					Version: "2.0",
					Id:      "0x1234",
				},
				topN: 3,
				retList: []string{
					"0x5678,Ava1,1095",
					"0x7777,Ava2,1003",
					"0x9012,Ava3, 988",
				},
				youRet:       "0x1234,AvaYou,99",
				yourCnt:      99,
				cntBeforeYou: "49",

				expectResponse: nil,
				expectError:    true,
			},
		}

		recordColumns := []string{"id", "avatar", "cnt"}
		countColunms := []string{"c"}
		for _, testCase := range testCases {
			setUp()
			if testCase.expectError {
				mock.ExpectQuery("SELECT id, avatar, kitns_daily \\+ kitns_disbursed \\+ kitns_ref_daily \\+ kitns_ref_disbursed AS cnt	FROM users ORDER BY cnt DESC LIMIT (.+)").
					WithArgs(testCase.topN).
					WillReturnError(fmt.Errorf("query error"))
			} else {
				mock.ExpectQuery("SELECT id, avatar, kitns_daily \\+ kitns_disbursed \\+ kitns_ref_daily \\+ kitns_ref_disbursed AS cnt	FROM users ORDER BY cnt DESC LIMIT (.+)").
					WithArgs(testCase.topN).
					WillReturnRows(
						sqlmock.NewRows(recordColumns).
							FromCSVString(strings.Join(testCase.retList, "\n")))
			}
			if testCase.youRet != "" {
				mock.ExpectQuery("SELECT id, avatar, kitns_daily \\+ kitns_disbursed \\+ kitns_ref_daily \\+ kitns_ref_disbursed AS cnt	FROM users WHERE id \\= (.+)").
					WithArgs(testCase.base.Id).
					WillReturnRows(
						sqlmock.NewRows(recordColumns).
							FromCSVString(testCase.youRet))
				mock.ExpectQuery("SELECT count\\(\\*\\) AS c FROM users	WHERE kitns_daily \\+ kitns_disbursed \\+ kitns_ref_daily \\+ kitns_ref_disbursed \\> (.+)").
					WithArgs(testCase.yourCnt).
					WillReturnRows(
						sqlmock.NewRows(countColunms).
							FromCSVString(testCase.cntBeforeYou))
			}

			response, err := GetTopScores(db, testCase.base, testCase.topN)
			if testCase.expectError != (err != nil) {
				t.Errorf("%s, getTopScores: expected error: %v, got error: %v", testCase.name, testCase.expectError, err)
			}

			if !reflect.DeepEqual(response, testCase.expectResponse) {
				t.Errorf("%s, getTopScores: expected %v, got %v", testCase.name, testCase.expectResponse, response)
			}
		}
	})
}

func TestGetStats(t *testing.T) {
	it(func() {
		testCases := []struct {
			name string
			id   string

			expectResponse *api.StatsResponse
			expectError    bool
		}{
			{
				name: "Get stats success",
				id:   "0x1234",

				expectResponse: &api.StatsResponse{
					Version:           "2.0",
					Id:                "0x1234",
					KitnsDaily:        10,
					KitnsDisbursed:    1000,
					KitnsRefDaily:     0.25,
					KitnsRefDisbusded: 5.5,
				},
				expectError: false,
			}, {
				name: "Get stats error",
				id:   "0x5678",

				expectResponse: nil,
				expectError:    true,
			},
		}

		recordColumns := []string{
			"kitns_daily",
			"kitns_disbursed",
			"kitns_ref_daily",
			"kitns_ref_disbursed",
		}
		for _, testCase := range testCases {
			setUp()
			if testCase.expectError {
				mock.ExpectQuery("SELECT kitns_daily, kitns_disbursed, kitns_ref_daily, kitns_ref_disbursed	FROM users WHERE id = (.+)").
					WithArgs(testCase.id).
					WillReturnError(fmt.Errorf("error getting kitns"))
			} else {
				mock.ExpectQuery("SELECT kitns_daily, kitns_disbursed, kitns_ref_daily, kitns_ref_disbursed	FROM users WHERE id = (.+)").
					WithArgs(testCase.id).
					WillReturnRows(sqlmock.NewRows(recordColumns).FromCSVString("10,1000,0.25,5.5"))
			}

			response, err := GetStats(db, testCase.id)
			if testCase.expectError != (err != nil) {
				t.Errorf("%s, getStats: expected error: %v, got error: %v", testCase.name, testCase.expectError, err)
			}

			if !reflect.DeepEqual(response, testCase.expectResponse) {
				t.Errorf("%s, getStats: expected %v, got %v", testCase.name, testCase.expectResponse, response)
			}
		}
	})
}

func TestReadReport(t *testing.T) {
	it(func() {
		testCases := []struct {
			name      string
			id        string
			seq       int
			seqExists bool
			sharing   string

			expectResponse *api.ReadReportResponse
			expectError    bool
		}{
			{
				name:      "Request existing report with enabled avatar",
				id:        "0x5678",
				seq:       123,
				seqExists: true,
				sharing:   "share_data_live",
				expectResponse: &api.ReadReportResponse{
					Id:       "0x1234",
					ActionId: "abcdef",
					Image:    []byte{97, 98, 99, 100, 101, 102, 103, 104},
					Avatar:   "testuser",
					Own:      false,
				},
				expectError: false,
			},
			{
				name:      "Request existing report with disabled avatar",
				id:        "0x5678",
				seq:       123,
				seqExists: true,
				sharing:   "not_sharing_data_live",
				expectResponse: &api.ReadReportResponse{
					Id:       "0x1234",
					ActionId: "abcdef",
					Image:    []byte{97, 98, 99, 100, 101, 102, 103, 104},
					Avatar:   "",
					Own:      false,
				},
				expectError: false,
			},
			{
				name:      "Request existing report from own user",
				id:        "0x1234",
				seq:       123,
				seqExists: true,
				sharing:   "not_sharing_data_live",
				expectResponse: &api.ReadReportResponse{
					Id:       "0x1234",
					ActionId: "abcdef",
					Image:    []byte{97, 98, 99, 100, 101, 102, 103, 104},
					Avatar:   "testuser",
					Own:      true,
				},
				expectError: false,
			},
			{
				name:           "Request non-existing report",
				seq:            99999,
				seqExists:      false,
				sharing:        "share_data_live",
				expectResponse: nil,
				expectError:    true,
			},
		}

		columns := []string{
			"id",
			"action_id",
			"image",
			"avatar",
			"privacy",
		}
		for _, testCase := range testCases {
			setUp()
			values := ""
			if testCase.seqExists {
				values = fmt.Sprintf("0x1234,abcdef,abcdefgh,testuser,%s", testCase.sharing)
			}
			mock.ExpectQuery("SELECT r.id, r.action_id, r.image, u.avatar, u.privacy FROM reports AS r JOIN users AS u	ON r.id = u.id WHERE r.seq = ?").WithArgs(testCase.seq).
				WillReturnRows(sqlmock.NewRows(columns).
					FromCSVString(values))

			response, err := ReadReport(db, &api.ReadReportArgs{
				Id:  testCase.id,
				Seq: testCase.seq,
			})

			if testCase.expectError != (err != nil) {
				t.Errorf("%s, readReport: expected error: %v, got error: %v", testCase.name, testCase.expectError, err)
			}

			if !reflect.DeepEqual(response, testCase.expectResponse) {
				t.Errorf("%s, readReport: expected %v, got %v", testCase.name, testCase.expectResponse, response)
			}
		}
	})
}

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
			setUp()
			if testCase.errorExpected {
				mock.ExpectQuery("SELECT refvalue	FROM referrals WHERE refkey = (.+)").WithArgs(testCase.refKey).
					WillReturnError(fmt.Errorf("test fetch error"))
			} else {
				mock.ExpectQuery("SELECT refvalue	FROM referrals WHERE refkey = (.+)").WithArgs(testCase.refKey).
					WillReturnRows(sqlmock.NewRows(columns).
						FromCSVString(strings.Join(testCase.refValues, "\n")))
			}

			refvalue, err := ReadReferral(db, testCase.refKey)
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
			setUp()
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

			if err := WriteReferral(db, testCase.refKey, testCase.refValue); testCase.errorExpected != (err != nil) {
				t.Errorf("%s, refDB.WriteReferral: expected error: %v, got error: %e", testCase.name, testCase.errorExpected, err)
			}
		}
	})
}

func TestGenerateReferral(t *testing.T) {
	it(func() {
		testCases := []struct {
			name    string
			version string
			id      string
			refcode string

			refExists     bool
			errorExpected bool

			expectedResponse *api.GenRefResponse
		}{
			{
				name:    "Success referral generation",
				version: "2.0",
				id:      "0x1234",
				refcode: "testrefid",

				refExists:     false,
				errorExpected: false,

				expectedResponse: &api.GenRefResponse{
					RefValue: "testrefid",
				},
			}, {
				name:    "Success existing referral retrieval",
				version: "2.0",
				id:      "0x5678",
				refcode: "testrefid",

				refExists:     true,
				errorExpected: false,

				expectedResponse: &api.GenRefResponse{
					RefValue: "testrefid",
				},
			}, {
				name:    "Error in referral generation storing",
				version: "2.0",
				id:      "0x9012",
				refcode: "testrefid",

				refExists:     false,
				errorExpected: true,

				expectedResponse: nil,
			},
		}

		columns := []string{
			"referral",
		}
		for _, testCase := range testCases {
			setUp()
			if testCase.refExists {
				mock.ExpectQuery("SELECT referral FROM users_refcodes WHERE id = (.+)").
					WithArgs(testCase.id).
					WillReturnRows(sqlmock.NewRows(columns).FromCSVString(testCase.refcode))
			} else {
				mock.ExpectQuery("SELECT referral FROM users_refcodes WHERE id = (.+)").
					WithArgs(testCase.id).
					WillReturnRows(sqlmock.NewRows(columns))
				if testCase.errorExpected {
					mock.ExpectExec("INSERT INTO users_refcodes \\(id, referral\\) VALUES \\((.+), (.+)\\)").
						WithArgs(testCase.id, testCase.refcode).
						WillReturnError(fmt.Errorf("ref update error"))
				} else {
					mock.ExpectExec("INSERT INTO users_refcodes \\(id, referral\\) VALUES \\((.+), (.+)\\)").
						WithArgs(testCase.id, testCase.refcode).
						WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}

			response, err := GenerateReferral(db, &api.GenRefRequest{
				Version: testCase.version,
				Id:      testCase.id,
			}, testRefGen)

			if testCase.errorExpected != (err != nil) {
				t.Errorf("%s, generateReferral: expected error: %v, got error: %e", testCase.name, testCase.errorExpected, err)
			}

			if !reflect.DeepEqual(response, testCase.expectedResponse) {
				t.Errorf("%s, generateReferral: expected %v, got %v", testCase.name, testCase.expectedResponse, response)
			}
		}
	})
}

func TestCleanupReferral(t *testing.T) {
	it(func() {
		testCases := []struct {
			name string
			ref  string

			errorExpected bool
		}{
			{
				name: "Success cleanup",
				ref:  "abcdef",

				errorExpected: false,
			}, {
				name: "Failed cleanup",
				ref:  "uvwxyz",

				errorExpected: true,
			},
		}
		for _, testCase := range testCases {
			setUp()
			if testCase.errorExpected {
				mock.ExpectExec("DELETE FROM referrals WHERE refvalue = (.+)").
					WithArgs(testCase.ref).
					WillReturnError(fmt.Errorf("ref delete error"))
			} else {
				mock.ExpectExec("DELETE FROM referrals WHERE refvalue = (.+)").
					WithArgs(testCase.ref).
					WillReturnResult(sqlmock.NewResult(1, 1))
			}

			if err := CleanupReferral(db, testCase.ref); testCase.errorExpected != (err != nil) {
				t.Errorf("%s, cleanupReferral: expected error: %v, got error: %v", testCase.name, testCase.errorExpected, err)
			}
		}
	})
}

func TestCreateAction(t *testing.T) {
	it(func() {
		testCases := []struct {
			name           string
			actionId       string
			actionName     string
			isActive       bool
			isActiveInt    int
			expirationDate string

			execSuccess bool
			err         error
		}{
			{
				name:           "Create action, success",
				actionId:       "abcdef",
				actionName:     "Name1",
				isActive:       true,
				isActiveInt:    1,
				expirationDate: "2024-09-09",

				execSuccess: true,
			}, {
				name:           "Create action, error",
				actionId:       "abcdef",
				isActive:       true,
				isActiveInt:    1,
				expirationDate: "2024-09-09",

				execSuccess: false,
				err:         fmt.Errorf("Error inserting into actions"),
			},
		}

		for _, testCase := range testCases {
			setUp()
			if testCase.execSuccess {
				mock.ExpectExec("INSERT INTO actions\\(id, name, is_active, expiration_date\\) VALUES\\((.+), (.+), (.+), (.+)\\)").
					WithArgs(testCase.actionId, testCase.actionName, testCase.isActiveInt, testCase.expirationDate).
					WillReturnResult(sqlmock.NewResult(1, 1))
			} else {
				mock.ExpectExec("INSERT INTO actions\\(id, name, is_active, expiration_date\\) VALUES\\((.+), (.+), (.+), (.+)\\)").
					WithArgs(testCase.actionId, testCase.actionName, testCase.isActiveInt, testCase.expirationDate).
					WillReturnError(testCase.err)
			}

			request := &api.ActionModifyArgs{
				Record: api.ActionRecord{
					Id:             testCase.actionId,
					Name:           testCase.actionName,
					IsActive:       testCase.isActive,
					ExpirationDate: testCase.expirationDate,
				},
			}

			resp, err := CreateAction(db, request)
			if testCase.execSuccess != (err == nil) {
				t.Errorf("%s: expected return error: %v, got error: %v", testCase.name, testCase.err != nil, err)
			}
			if err == nil && !reflect.DeepEqual(resp.Record, request.Record) {
				t.Errorf("%s, expected result: %v, got result: %v", testCase.name, request.Record, resp.Record)
			}
		}
	})
}

func TestUpdateAction(t *testing.T) {
	it(func() {
		testCases := []struct {
			name           string
			actionId       string
			actionName     string
			isActive       bool
			isActiveInt    int
			expirationDate string

			rowsAffected int
			execSuccess  bool
			err          error
		}{
			{
				name:           "Update action, success",
				actionId:       "abcdef",
				actionName:     "Name1",
				isActive:       true,
				isActiveInt:    1,
				expirationDate: "2024-09-09",

				rowsAffected: 1,
				execSuccess:  true,
			}, {
				name:           "Update action, error",
				actionId:       "abcdef",
				isActive:       true,
				isActiveInt:    1,
				expirationDate: "2024-09-09",

				execSuccess: false,
				err:         fmt.Errorf("Error inserting into actions"),
			}, {
				name:           "Update action, zero rows",
				actionId:       "abcdef",
				isActive:       true,
				isActiveInt:    1,
				expirationDate: "2024-09-09",

				rowsAffected: 0,
				execSuccess:  false,
			},
		}

		for _, testCase := range testCases {
			setUp()
			if testCase.execSuccess {
				mock.ExpectExec("UPDATE actions	SET name = (.+), is_active = (.+), expiration_date = (.+) WHERE id = (.+)").
					WithArgs(testCase.actionName, testCase.isActiveInt, testCase.expirationDate, testCase.actionId).
					WillReturnResult(sqlmock.NewResult(1, int64(testCase.rowsAffected)))
			} else {
				mock.ExpectExec("UPDATE actions	SET name = (.+), is_active = (.+), expiration_date = (.+) WHERE id = (.+)").
					WithArgs(testCase.actionName, testCase.isActiveInt, testCase.expirationDate, testCase.actionId).
					WillReturnError(testCase.err)
			}

			request := &api.ActionModifyArgs{
				Record: api.ActionRecord{
					Id:             testCase.actionId,
					Name:           testCase.actionName,
					IsActive:       testCase.isActive,
					ExpirationDate: testCase.expirationDate,
				},
			}

			resp, err := UpdateAction(db, request)
			if testCase.execSuccess != (err == nil) {
				t.Errorf("%s: expected return error: %v, got error: %v", testCase.name, testCase.err != nil, err)
			}
			expError := (testCase.rowsAffected == 1) != (err == nil)
			if expError {
				t.Errorf("%s: expected rows: %d, got error: %v", testCase.name, testCase.rowsAffected, err)
			}
			if err == nil && testCase.rowsAffected == 1 && !reflect.DeepEqual(resp.Record, request.Record) {
				t.Errorf("%s, expected result: %v, got result: %v", testCase.name, request.Record, resp.Record)
			}
		}
	})
}

func TestDeleteAction(t *testing.T) {
	it(func() {
		testCases := []struct {
			name     string
			actionId string

			rowsAffected int
			execSuccess  bool
			err          error
		}{
			{
				name:     "Delete action, success",
				actionId: "abcdef",

				rowsAffected: 1,
				execSuccess:  true,
			}, {
				name:     "Delete action, error",
				actionId: "abcdef",

				execSuccess: false,
				err:         fmt.Errorf("Error inserting into actions"),
			}, {
				name:     "Delete action, zero rows",
				actionId: "abcdef",

				rowsAffected: 0,
				execSuccess:  false,
			},
		}

		for _, testCase := range testCases {
			setUp()
			if testCase.execSuccess {
				mock.ExpectExec("DELETE FROM actions	WHERE id = (.+)").
					WithArgs(testCase.actionId).
					WillReturnResult(sqlmock.NewResult(1, int64(testCase.rowsAffected)))
			} else {
				mock.ExpectExec("DELETE FROM actions	WHERE id = (.+)").
					WithArgs(testCase.actionId).
					WillReturnError(testCase.err)
			}

			err := DeleteAction(db, &api.ActionModifyArgs{
				Record: api.ActionRecord{
					Id: testCase.actionId,
				},
			})
			if testCase.execSuccess != (err == nil) {
				t.Errorf("%s: expected return error: %v, got error: %v", testCase.name, testCase.err != nil, err)
			}
			expError := (testCase.rowsAffected == 1) != (err == nil)
			if expError {
				t.Errorf("%s: expected rows: %d, got error: %v", testCase.name, testCase.rowsAffected, err)
			}
		}
	})
}

func TestGetActions(t *testing.T) {
	it(func() {
		testCases := []struct {
			name     string
			actionId string

			expectSuccess bool
			expectReturn  *api.ActionsResponse
		}{
			{
				name: "Get all actions, success",

				expectSuccess: true,
				expectReturn: &api.ActionsResponse{
					Records: []api.ActionRecord{
						{
							Id:             "abcdef",
							Name:           "action1",
							IsActive:       true,
							ExpirationDate: "2024-12-12",
						}, {
							Id:             "123456",
							Name:           "action2",
							IsActive:       true,
							ExpirationDate: "2024-12-13",
						},
					},
				},
			},
			{
				name:     "Get one action, success",
				actionId: "123456",

				expectSuccess: true,
				expectReturn: &api.ActionsResponse{
					Records: []api.ActionRecord{
						{
							Id:             "123456",
							Name:           "action2",
							IsActive:       true,
							ExpirationDate: "2024-12-13",
						},
					},
				},
			},
			{
				name: "Get actions, error",

				expectSuccess: false,
			},
		}

		cols := []string{
			"id",
			"name",
			"is_active",
			"expiration_date",
		}

		for _, testCase := range testCases {
			setUp()

			if testCase.expectSuccess {
				if testCase.actionId == "" {
					mock.ExpectQuery("SELECT id, name, is_active, expiration_date FROM actions").
						WillReturnRows(sqlmock.NewRows(cols).FromCSVString("abcdef,action1,1,2024-12-12\n123456,action2,1,2024-12-13"))
				} else {
					mock.ExpectQuery("SELECT id, name, is_active, expiration_date FROM actions WHERE id = (.+)").
					WithArgs(testCase.actionId).
					WillReturnRows(sqlmock.NewRows(cols).FromCSVString("123456,action2,1,2024-12-13"))
				}
			} else {
				mock.ExpectQuery("SELECT id, name, is_active, expiration_date FROM actions").
					WillReturnError(fmt.Errorf("error selecting from actions"))
			}

			result, err := GetActions(db, testCase.actionId)
			if testCase.expectSuccess == (err != nil) {
				t.Errorf("%s, expected error: %v, got error: %v", testCase.name, testCase.expectSuccess, err)
			}
			if !reflect.DeepEqual(result, testCase.expectReturn) {
				t.Errorf("%s, expected result: %v, got result: %v", testCase.name, testCase.expectReturn, result)
			}
		}
	})
}
