package gormbulk

import (
	"sort"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jinzhu/gorm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_scopeFromObjects(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)

	gdb, err := gorm.Open("mysql", db)
	require.NoError(t, err)

	pastDate := time.Date(1985, 1, 1, 0, 0, 0, 0, time.UTC)

	type test struct {
		Foo string
		Bar string
	}

	type timeT struct {
		CreatedAt time.Time
	}

	type T struct {
		ID    int `gorm:"primary_key"`
		Field string
	}

	type TT struct {
		ID    int `gorm:"primary_key"`
		Field string
		T     T
		TId   int
	}

	type TTT struct {
		ID       int `gorm:"primary_key"`
		Field    string
		T        T `gorm:"foreignkey:RandomID"`
		RandomID int
	}

	cases := []struct {
		description     string
		slice           []interface{}
		execFunc        ExecFunc
		scopes          map[string]string
		allVarsSame     bool
		expectedSQL     string
		expectedSQLVars []interface{}
		errContains     string
	}{
		{
			description: "invalid objects",
			slice: []interface{}{
				"string", 1,
			},
			errContains: "value must be kind of Struct",
		},
		{
			description: "scope returned ok",
			slice: []interface{}{
				test{"one", "two"},
			},
			execFunc: func(scope *gorm.Scope, _, _ []string) {
				scope.Raw("INSERT INTO")
				scope.SQLVars = []interface{}{}
			},
			expectedSQL: "INSERT INTO",
		},
		{
			description: "scope returned ok with existing execFunc",
			slice: []interface{}{
				test{"one", "two"},
			},
			execFunc:    InsertFunc,
			scopes:      map[string]string{"gorm:insert_option": "ON DUPLICATE KEY UPDATE foo = VALUES(foo)"},
			expectedSQL: "INSERT INTO `tests` (`bar`, `foo`) VALUES (?, ?) ON DUPLICATE KEY UPDATE foo = VALUES(foo)",
		},
		{
			description: "pointers are de-references OK",
			slice: []interface{}{
				struct {
					ID  int `gorm:"primary_key"` // Should be skipped
					Foo string
				}{
					ID:  0,
					Foo: "foo",
				},
			},
			execFunc:    InsertFunc,
			expectedSQL: "INSERT INTO `` (`foo`) VALUES (?)",
		},
		{
			description: "relationships ignored",
			slice: []interface{}{
				TT{
					Field: "keep me",
					TId:   3,
					T:     T{},
				},
			},
			execFunc:        InsertFunc,
			expectedSQL:     "INSERT INTO `tts` (`field`, `t_id`) VALUES (?, ?)",
			expectedSQLVars: []interface{}{"keep me", 3},
		},
		{
			description: "foreign key ignored",
			slice: []interface{}{
				TTT{
					Field:    "keep me",
					RandomID: 3,
					T:        T{},
				},
			},
			execFunc:        InsertFunc,
			expectedSQL:     "INSERT INTO `ttts` (`field`, `random_id`) VALUES (?, ?)",
			expectedSQLVars: []interface{}{"keep me", 3},
		},
		{
			description: "test primary keys",
			slice: []interface{}{
				&test{"expected foo", "expected bar"},
			},
			execFunc:        InsertFunc,
			expectedSQL:     "INSERT INTO `tests` (`bar`, `foo`) VALUES (?, ?)",
			expectedSQLVars: []interface{}{"expected bar", "expected foo"},
		},
		{
			description: "test auto increment",
			slice: []interface{}{
				struct {
					ID  int `gorm:"auto_increment"` // Should be skipped
					Foo string
				}{
					ID:  0,
					Foo: "foo",
				},
			},
			execFunc:    InsertFunc,
			expectedSQL: "INSERT INTO `` (`foo`) VALUES (?)",
		},
		{
			description: "test auto increment set to true and ignored fields",
			slice: []interface{}{
				struct {
					NotID    int `gorm:"auto_increment:true"` // Should be skipped
					Foo      string
					IgnoreMe string `gorm:"-"`
				}{
					NotID: 0,
					Foo:   "foo",
				},
			},
			execFunc:    InsertFunc,
			expectedSQL: "INSERT INTO `` (`foo`) VALUES (?)",
		},
		{
			description: "test auto increment set to false",
			slice: []interface{}{
				struct {
					NotID int `gorm:"auto_increment:false"` // Should NOT be skipped
					Foo   string
				}{
					NotID: 0,
					Foo:   "foo",
				},
			},
			execFunc:    InsertFunc,
			expectedSQL: "INSERT INTO `` (`foo`, `not_id`) VALUES (?, ?)",
		},
		{
			description: "test setting default value (by omitting it and let DBM set it)",
			slice: []interface{}{
				struct {
					ID  int    `gorm:"auto_increment"`   // Should be skipped
					Foo string `gorm:"default:'foobar'"` // Should be skipped
					Bar string
				}{
					ID:  0,
					Foo: "",
					Bar: "barbar",
				},
			},
			execFunc:        InsertFunc,
			expectedSQL:     "INSERT INTO `` (`bar`) VALUES (?)",
			expectedSQLVars: []interface{}{"barbar"},
		},
		{
			description: "test non default CreatedAt and UpdatedAt",
			slice: []interface{}{
				struct {
					ID        int `gorm:"auto_increment"` // Should be skipped
					CreatedAt time.Time
					UpdatedAt time.Time
				}{
					ID:        0,
					CreatedAt: pastDate,
					UpdatedAt: pastDate,
				},
			},
			execFunc:        InsertFunc,
			expectedSQL:     "INSERT INTO `` (`created_at`, `updated_at`) VALUES (?, ?)",
			expectedSQLVars: []interface{}{pastDate, pastDate},
		},
		{
			description: "ensure exact same time for all records",
			slice: []interface{}{
				timeT{}, timeT{}, timeT{},
			},
			execFunc:    InsertFunc,
			allVarsSame: true,
			expectedSQL: "INSERT INTO `time_ts` (`created_at`) VALUES (?), (?), (?)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			db := gdb
			for k, v := range tc.scopes {
				db = gdb.Set(k, v)
			}

			scope, err := scopeFromObjects(db, tc.slice, tc.execFunc)

			if tc.errContains != "" {
				require.Nil(t, scope)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)

				return
			}

			require.NotNil(t, scope)
			require.NoError(t, err)

			assert.Equal(t, tc.expectedSQL, scope.SQL)

			if tc.expectedSQLVars != nil {
				assert.Equal(t, tc.expectedSQLVars, scope.SQLVars)
			}

			if tc.allVarsSame {
				require.True(t, len(scope.SQLVars) > 0)

				first := scope.SQLVars[0]
				for i := range scope.SQLVars[1:] {
					assert.Equal(t, first, scope.SQLVars[i])
				}
			}
		})
	}
}

func TestBulkExecChunk(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	gdb, err := gorm.Open("mysql", db)
	require.NoError(t, err)

	type test struct {
		ID  int `gorm:"primary_key"`
		Foo string
		Bar string
	}

	cases := []struct {
		description      string
		execFunc         ExecFunc
		slices           []interface{}
		chunkSize        int
		expectedMockFunc func(mock sqlmock.Sqlmock)
		countErrors      int
	}{
		{
			description: "errors returned",
			execFunc:    InsertFunc,
			slices: []interface{}{
				"string", "string",
				1, 2,
			},
			expectedMockFunc: func(mock sqlmock.Sqlmock) {},
			chunkSize:        1,
			countErrors:      4,
		},
		{
			description: "six rows in chunks of 3 - will be two calls with 6 args",
			execFunc:    InsertFunc,
			slices: []interface{}{
				test{Foo: "one", Bar: "two"},
				test{Foo: "one", Bar: "two"},
				test{Foo: "one", Bar: "two"},
				test{Foo: "one", Bar: "two"},
				test{Foo: "one", Bar: "two"},
				test{Foo: "one", Bar: "two"},
			},
			chunkSize: 3,
			expectedMockFunc: func(mock sqlmock.Sqlmock) {
				// We expect two insert statements
				mock.ExpectExec("INSERT INTO `tests`").
					WithArgs("two", "one", "two", "one", "two", "one").
					WillReturnResult(sqlmock.NewResult(0, 0))

				mock.ExpectExec("INSERT INTO `tests`").
					WithArgs("two", "one", "two", "one", "two", "one").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
		{
			description: "uneven row count, chunk size of 3, calls with different arg count",
			execFunc:    InsertFunc,
			slices: []interface{}{
				test{Foo: "one", Bar: "two"},
				test{Foo: "one", Bar: "two"},
				test{Foo: "one", Bar: "two"},
				test{Foo: "one", Bar: "two"},
				test{Foo: "one", Bar: "two"},
				test{Foo: "one", Bar: "two"},
				test{Foo: "one", Bar: "two"},
			},
			chunkSize: 3,
			expectedMockFunc: func(mock sqlmock.Sqlmock) {
				// We expect two insert statements
				mock.ExpectExec("INSERT INTO `tests`").
					WithArgs("two", "one", "two", "one", "two", "one").
					WillReturnResult(sqlmock.NewResult(0, 0))

				mock.ExpectExec("INSERT INTO `tests`").
					WithArgs("two", "one", "two", "one", "two", "one").
					WillReturnResult(sqlmock.NewResult(0, 0))

				mock.ExpectExec("INSERT INTO `tests`").
					WithArgs("two", "one").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			tc.expectedMockFunc(mock)

			err := BulkExecChunk(gdb, tc.slices, tc.execFunc, tc.chunkSize)

			if tc.countErrors > 0 {
				assert.Len(t, err, tc.countErrors)
				return
			}

			require.Nil(t, err)
		})
	}
}

func Test_columnOrder(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)

	gdb, err := gorm.Open("mysql", db)
	require.NoError(t, err)

	type test struct {
		Xxx      int
		Time     time.Time
		TimeFrom time.Time
		Aaa      string
		N1       string `gorm:"column:100_aa"`
		N2       string `gorm:"column:100_a"`
	}

	cases := []struct {
		description   string
		slices        []interface{}
		expectedOrder []string
	}{
		{
			description: "sorted equally",
			slices: []interface{}{
				test{},
			},
			expectedOrder: []string{"`100_a`", "`100_aa`", "`aaa`", "`time`", "`time_from`", "`xxx`"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			scopeFunc := func(scope *gorm.Scope, columns, _ []string) {
				assert.Equal(t, tc.expectedOrder, columns)

				// Prove that sorting quited columns differ from sorted string
				// columns.
				sort.Strings(columns)
				assert.NotEqual(t, tc.expectedOrder, columns)

				t.Logf("expected order: %s", tc.expectedOrder)
				t.Logf("sort after quite yields different result: %s", columns)
			}

			scope, err := scopeFromObjects(gdb, tc.slices, scopeFunc)

			require.NoError(t, err)
			require.NotNil(t, scope)
		})
	}
}
