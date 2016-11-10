//Package gostruct is an ORM that generates a golang package based on a MySQL database table including a DAO, BO, CRUX, test, and example file
//
//The CRUX file provides all basic CRUD functionality to handle any objects of the table. The test file is a skeleton file
//ready to use for unit testing.
package gostruct

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"log"
	"errors"
	"os"
	"strings"
	"fmt"
)

//TableObj is the result set returned from the MySQL information_schema that
//contains all data for a specific table
type TableObj struct {
	Name       string
	IsNullable string
	Key        string
	DataType   string
	ColumnType string
	Default    sql.NullString
	Extra      sql.NullString
}

type UsedColumn struct {
	Name string
}

type UniqueValues struct {
	Value sql.NullString
}

type Table struct {
	Name string
}

//Globals variables
var err error
var con *sql.DB
var tables []string
var tablesDone []string
var primaryKey string
var GOPATH string
var exampleIdStr string
var exampleColumn string
var exampleColumnStr string
var exampleOrderStr string

//initialize global GOPATH
func init() {
	GOPATH = os.Getenv("GOPATH")
}

//Generates a package for a single table
func (gs *Gostruct) Run(table string) error {
	//make sure models dir exists
	if !exists(GOPATH + "/src/models") {
		err = gs.CreateDirectory(GOPATH + "/src/models")
		if err != nil {
			return err
		}
	}

	err = gs.buildConnectionPackage()
	if err != nil {
		return err
	}

	//handle utils file
	err = gs.buildUtilsPackage()
	if err != nil {
		return err
	}

	err = gs.handleTable(table)
	if err != nil {
		return err
	}

	return nil
}

//Generates packages for all tables in a specific database and host
func (gs *Gostruct) RunAll() error {
	connection, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", gs.Username, gs.Password, gs.Host, gs.Port, gs.Database))
	if err != nil {
		panic(err)
	} else {
		rows, err := connection.Query("SELECT DISTINCT(TABLE_NAME) FROM `information_schema`.`COLUMNS` WHERE `TABLE_SCHEMA` LIKE ?", gs.Database)
		if err != nil {
			panic(err)
		} else {
			for rows.Next() {
				var table Table
				rows.Scan(&table.Name)

				err = gs.Run(table.Name)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

//Creates directory and sets permissions to 0777
func (gs *Gostruct) CreateDirectory(path string) error {
	err = os.Mkdir(path, 0777)
	if err != nil {
		return err
	}

	//give new directory full permissions
	err = os.Chmod(path, 0777)
	if err != nil {
		return err
	}

	return nil
}

//Main handler method for tables
func (gs *Gostruct) handleTable(table string) error {
	if inArray(table, tablesDone) {
		return nil
	} else {
		tablesDone = append(tablesDone, table)
	}

	log.Println("Generating Models for: " + table)

	con, err = sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", gs.Username, gs.Password, gs.Host, gs.Port, gs.Database))

	if err != nil {
		return err
	}

	rows1, err := con.Query("SELECT column_name, is_nullable, column_key, data_type, column_type, column_default, extra FROM information_schema.columns WHERE table_name = ? AND table_schema = ?", table, gs.Database)

	var object TableObj
	var objects []TableObj = make([]TableObj, 0)
	var columns []string

	if err != nil {
		return err
	} else {
		cntPK := 0
		for rows1.Next() {
			rows1.Scan(&object.Name, &object.IsNullable, &object.Key, &object.DataType, &object.ColumnType, &object.Default, &object.Extra)
			objects = append(objects, object)
			if object.Key == "PRI" {
				cntPK++
			}
			if cntPK > 1 && object.Key == "PRI" {
				continue
			}
			columns = append(columns, object.Name)
		}
	}
	defer rows1.Close()

	primaryKey = ""
	if len(objects) == 0 {
		return errors.New("No results for table: " + table)
	} else {
		//get PrimaryKey
		for i := 0; i < len(objects); i++ {
			object := objects[i]
			if object.Key == "PRI" {
				primaryKey = object.Name
				break
			}
		}

		//create directory
		dir := GOPATH + "/src/models/" + uppercaseFirst(table) + "/"
		if !exists(dir) {
			err := os.Mkdir(dir, 0777)
			if err != nil {
				return err
			}

			//give new directory full permissions
			err = os.Chmod(dir, 0777)
			if err != nil {
				return err
			}
		}

		//handle CRUX file
		err = gs.buildCruxFile(objects, table)
		if err != nil {
			return err
		}

		//handle DAO file
		err = gs.buildDaoFile(table)
		if err != nil {
			return err
		}

		//handle BO file
		err = gs.buildBoFile(table)
		if err != nil {
			return err
		}

		//handle Test file
		err = gs.buildTestFile(table)
		if err != nil {
			return err
		}

		//handle Example file
		err = gs.buildExamplesFile(table)
		if err != nil {
			return err
		}

		//handle Date file
		err = gs.buildDatePackage()
		if err != nil {
			return err
		}
	}

	return nil
}

//Builds CRUX_{table}.go file with main struct and CRUD functionality
func (gs *Gostruct) buildCruxFile(objects []TableObj, table string) error {
	exampleIdStr = ""
	exampleColumn = ""
	exampleColumnStr = ""
	exampleOrderStr = ""

	tableNaming := uppercaseFirst(table)
	dir := GOPATH + "/src/models/" + uppercaseFirst(table) + "/"

	var usedColumns []UsedColumn
	initialString := `//Package ` + uppercaseFirst(table) + ` serves as the base structure for the ` + table + ` table
//and contains base methods and CRUD functionality to
//interact with the ` + table + ` table in the ` + gs.Database + ` database
package ` + uppercaseFirst(table)
	importString := `

import (
	"database/sql"
	"strings"
	"connection"
	"reflect"
	"utils"`

	string1 := `

//` + uppercaseFirst(table) + `Obj is the structure of the home table
//
//This contains all columns that exist in the database
type ` + uppercaseFirst(table) + "Obj struct {"
	string2 := ""
	contents := ""

	primaryKeys := []string{}
	primaryKeyTypes := []string{}

	importTime := false
	importDate := false

	questionMarks := make([]string, 0)

	Loop:
	for i := 0; i < len(objects); i++ {
		object := objects[i]
		for c := 0; c < len(usedColumns); c++ {
			if usedColumns[c].Name == object.Name {
				continue Loop
			}
		}
		usedColumns = append(usedColumns, UsedColumn{Name: object.Name})
		questionMarks = append(questionMarks, "?")

		isBool := true
		var dataType string
		switch object.DataType {
		case "int":
			isBool = false
			if object.IsNullable == "NO" {
				dataType = "int"
			} else {
				dataType = "sql.NullInt64"
			}
		case "tinyint":
			rows, err := con.Query("SELECT DISTINCT(`" + object.Name + "`) FROM " + gs.Database + "." + table)
			if err != nil {
				return err
			} else {
				for rows.Next() {
					var uObj UniqueValues
					rows.Scan(&uObj.Value)
					if uObj.Value.String != "0" && uObj.Value.String != "1" {
						isBool = false
					}
				}
			}
			if isBool {
				if object.IsNullable == "NO" {
					dataType = "bool"
				} else {
					dataType = "sql.NullBool"
				}
			} else {
				if object.IsNullable == "NO" {
					dataType = "int"
				} else {
					dataType = "sql.NullInt64"
				}
			}
		case "bool", "boolean":
			if object.IsNullable == "NO" {
				dataType = "bool"
			} else {
				dataType = "sql.NullBool"
			}
		case "float", "decimal":
			isBool = false
			if object.IsNullable == "NO" {
				dataType = "float64"
			} else {
				dataType = "sql.NullFloat64"
			}
		case "date", "datetime", "timestamp":
			isBool = false
			if object.IsNullable == "NO" {
				importTime = true
				dataType = "time.Time"
			} else {
				importDate = true
				dataType = "date.NullTime"
			}
		default:
			isBool = false
			if object.IsNullable == "NO" {
				dataType = "string"
				if i > 1 && exampleColumn == "" {
					exampleColumn = object.Name
					exampleColumnStr = uppercaseFirst(object.Name) + ` = "some string"`
				}
			} else {
				dataType = "sql.NullString"
				if i > 1 && exampleColumn == "" {
					exampleColumn = object.Name
					exampleColumnStr = uppercaseFirst(object.Name) + `.String = "some string"`
				}
			}
		}

		if object.Key == "PRI" {
			if isBool {
				if object.IsNullable == "NO" {
					dataType = "int"
				} else {
					dataType = "sql.NullInt64"
				}
			}
			if dataType == "string" || dataType == "sql.NullString" {
				exampleIdStr = `"12345"`
			} else if dataType == "int" || dataType == "sql.NullInt64" {
				exampleIdStr = `12345`
			}
			if exampleOrderStr == "" {
				exampleOrderStr = object.Name + ` DESC`
			}
			primaryKeys = append(primaryKeys, object.Name)
			primaryKeyTypes = append(primaryKeyTypes, dataType)
		}

		if i > 0 {
			string2 += ", &" + strings.ToLower(table) + "." + uppercaseFirst(object.Name)
		}
		defaultVal := ""
		if strings.ToLower(object.Default.String) != "null" {
			if object.Default.String == "0" && object.IsNullable == "YES" {
				defaultVal = ""
			} else {
				defaultVal = object.Default.String
			}
		}
		string1 += "\n\t" + uppercaseFirst(object.Name) + "\t\t" + dataType + "\t\t`column:\"" + object.Name + "\" default:\"" + defaultVal + "\" type:\"" + object.ColumnType + "\" key:\"" + object.Key + "\" extra:\"" + object.Extra.String + "\"`"
	}
	string1 += "\n}"

	if importTime {
		importString += `
		"time"`
	}
	if importDate {
		importString += `
		"date"`
	}

	bs := "`"

	if len(primaryKeys) > 0 {
		string1 += `

//Save accepts a ` + uppercaseFirst(table) + `Obj pointer
//
//Turns each value into it's string representation
//so we can save it to the database
func (` + strings.ToLower(table) + ` *` + uppercaseFirst(table) + `Obj) Save() (sql.Result, error) {
	v := reflect.ValueOf(` + strings.ToLower(table) + `).Elem()
	objType := v.Type()

	columnArr := make([]string, 0)
	args := make([]interface{}, 0)
	q := make([]string, 0)

	updateStr := ""
	query := "INSERT INTO ` + table + `"
	for i := 0; i < v.NumField(); i++ {
		val, err := utils.ValidateField(v.Field(i), objType.Field(i))
		if err != nil {
			return nil, err
		}
		args = append(args, val)
		column := string(objType.Field(i).Tag.Get("column"))
		columnArr = append(columnArr, "` + bs + `"+column+"` + bs + `")
		q = append(q, "?")
		if i > 0 && updateStr != "" {
			updateStr += ", "
		}
		updateStr += "` + bs + `" + column + "` + bs + ` = ?"
	}

	query += " (" + strings.Join(columnArr, ", ") + ") VALUES (" + strings.Join(q, ", ") + ") ON DUPLICATE KEY UPDATE " + updateStr
	newArgs := append(args, args...)`

		if len(primaryKeys) > 1 {
			string1 += `

	return Exec(query, newArgs...)`
		} else {
			var insertIdStr string
			switch primaryKeyTypes[0] {
			case "string":
				insertIdStr = "strconv.FormatInt(id, 10)"
				importString += `
				"strconv"`
			default:
				insertIdStr = `int(id)`
			}

			string1 += `
	newRecord := false
	if utils.Empty(` + strings.ToLower(table) + `.` + uppercaseFirst(primaryKeys[0]) + `) {
		newRecord = true
	}

	res, err := Exec(query, newArgs...)
	if err == nil && newRecord {
		id, _ := res.LastInsertId()
		` + strings.ToLower(table) + `.` + uppercaseFirst(primaryKeys[0]) + ` = ` + insertIdStr + `
	}
	return res, err`
		}

		string1 += `
}`

		whereStrQuery, whereStrQueryValues := "", ""
		for k := range primaryKeys {
			if k > 0 {
				whereStrQuery += " AND"
				whereStrQueryValues += ","
			}
			whereStrQuery += ` ` + primaryKeys[k] + ` = ?`
			whereStrQueryValues += ` ` + strings.ToLower(table) + `.` + uppercaseFirst(primaryKeys[k])
		}

		string1 += `

//Deletes record from database
func (` + strings.ToLower(table) + ` *` + uppercaseFirst(table) + `Obj) Delete() (sql.Result, error) {
	return Exec("DELETE FROM ` + table + ` WHERE` + whereStrQuery + `", ` + whereStrQueryValues + `)
}
`
		paramStr, whereStr, whereStrValues := "", "", ""
		for k := range primaryKeys {
			var param string
			if primaryKeys[k] == "type" {
				param = "objType"
			} else if primaryKeys[k] == "typeId" {
				param = "objTypeId"
			} else {
				param = primaryKeys[k]
			}

			var dataType string
			var paramTypeStr string
			switch primaryKeyTypes[k] {
			case "int":
				dataType = "int"
				paramTypeStr = "strconv.Itoa(" + param + ")"
			case "float64":
				dataType = "float64"
				paramTypeStr = "strconv.FormatFloat(" + param + ", 'f', -1, 64)"
			default:
				dataType = "string"
				paramTypeStr = param
			}

			paramStr += param + " " + dataType
			if k > 0 {
				whereStr += " AND"
				whereStrValues += ","
			}
			if k == len(primaryKeys) - 1 {
				whereStr += ` ` + param + ` = '" + ` + paramTypeStr + ` + "'"`
			} else {
				paramStr += ", "
				whereStr += ` ` + param + ` = '" + ` + paramTypeStr + ` + "'`
			}
			whereStrValues += " " + param
		}

		string1 += `
//Returns a single object as pointer
func ReadById(` + paramStr + `) (*` + uppercaseFirst(table) + `Obj, error) {
	return ReadOneByQuery("SELECT * FROM ` + table + ` WHERE` + whereStrQuery + `", ` + whereStrValues + `)
}`
	}

	string1 += `

//Returns all records in the table
func ReadAll(order string) ([]*` + uppercaseFirst(table) + `Obj, error) {
	query := "SELECT * FROM ` + table + `"
	if order != "" {
		query += " ORDER BY " + order
	}
	return ReadByQuery(query)
}`

	string1 += `

//Returns a slice of ` + uppercaseFirst(table) + `Obj pointers
//
//Accepts a query string, and an order string
func ReadByQuery(query string, args ...interface{}) ([]*` + uppercaseFirst(table) + `Obj, error) {
	con := connection.Get()
	objects := make([]*` + uppercaseFirst(table) + `Obj, 0)
	query = strings.Replace(query, "'", "\"", -1)
	rows, err := con.Query(query, args...)
	if err != nil {
		return objects, err
	} else {
		for rows.Next() {
			var ` + strings.ToLower(table) + ` ` + uppercaseFirst(table) + `Obj
			err = rows.Scan(&` + strings.ToLower(table) + `.` + uppercaseFirst(objects[0].Name) + string2 + `)
			if err != nil {
				return objects, err
			}
			objects = append(objects, &` + strings.ToLower(table) + `)
		}
		err = rows.Err()
		if len(objects) == 0 {
			return objects, sql.ErrNoRows
		} else if err != nil && err != sql.ErrNoRows {
			return objects, err
		}
		rows.Close()
	}

	return objects, nil
}

//Returns a single object as pointer
//
//Serves as the LIMIT 1
func ReadOneByQuery(query string, args ...interface{}) (*` + uppercaseFirst(table) + `Obj, error) {
	var ` + strings.ToLower(table) + ` ` + uppercaseFirst(table) + `Obj

	con := connection.Get()
	query = strings.Replace(query, "'", "\"", -1)
	err := con.QueryRow(query, args...).Scan(&` + strings.ToLower(table) + `.` + uppercaseFirst(objects[0].Name) + string2 + `)

	return &` + strings.ToLower(table) + `, err
}

//Method for executing UPDATE queries
func Exec(query string, args ...interface{}) (sql.Result, error) {
	con := connection.Get()
	return con.Exec(query, args...)
}`

	importString += "\n)"
	contents = initialString + importString + string1

	cruxFilePath := dir + "CRUX_" + tableNaming + ".go"
	err = writeFile(cruxFilePath, contents, true)
	if err != nil {
		return err
	}

	_, err = runCommand("go fmt " + cruxFilePath)
	if err != nil {
		return err
	}

	return nil
}

//Builds DAO_{table}.go file for custom Data Access Object methods
func (gs *Gostruct) buildDaoFile(table string) error {
	tableNaming := uppercaseFirst(table)
	dir := GOPATH + "/src/models/" + uppercaseFirst(table) + "/"
	daoFilePath := dir + "DAO_" + tableNaming + ".go"

	if !exists(daoFilePath) {
		contents := "package " + uppercaseFirst(table) + "\n\n//Methods Here"
		err = writeFile(daoFilePath, contents, false)
		if err != nil {
			return err
		}
	}

	_, err := runCommand("go fmt " + daoFilePath)
	if err != nil {
		return err
	}

	return nil
}

//Builds BO_{table}.go file for custom Business Object methods
func (gs *Gostruct) buildBoFile(table string) error {
	tableNaming := uppercaseFirst(table)
	dir := GOPATH + "/src/models/" + uppercaseFirst(table) + "/"
	boFilePath := dir + "BO_" + tableNaming + ".go"

	if !exists(boFilePath) {
		contents := "package " + uppercaseFirst(table) + "\n\n//Methods Here"
		err = writeFile(boFilePath, contents, false)
		if err != nil {
			return err
		}
	}

	_, err := runCommand("go fmt " + boFilePath)
	if err != nil {
		return err
	}

	return nil
}

//Builds {table}_test.go file
func (gs *Gostruct) buildTestFile(table string) error {
	tableNaming := uppercaseFirst(table)
	dir := GOPATH + "/src/models/" + uppercaseFirst(table) + "/"
	testFilePath := dir + tableNaming + "_test.go"

	if !exists(testFilePath) {
		contents := `package ` + tableNaming + `_test

		import (
			"testing"
		)

		func TestSomething(t *testing.T) {
			//test stuff here..
		}`
		err = writeFile(testFilePath, contents, false)
		if err != nil {
			return err
		}
	}

	_, err := runCommand("go fmt " + testFilePath)
	if err != nil {
		return err
	}

	return nil
}

//Builds {table}_test.go file
func (gs *Gostruct) buildExamplesFile(table string) error {
	tableNaming := uppercaseFirst(table)
	dir := GOPATH + "/src/models/" + uppercaseFirst(table) + "/"
	examplesFilePath := dir + "examples_test.go"

	if !exists(examplesFilePath) {
		contents := `package ` + tableNaming + `_test

import (
	"fmt"
	"models/` + tableNaming + `"
)

func Example` + tableNaming + `Obj_Save() {
	` + strings.ToLower(table) + `, err := ` + tableNaming + `.ReadById(` + exampleIdStr + `)
	if err == nil {
		` + strings.ToLower(table) + `.` + exampleColumnStr + `
		_, err = ` + strings.ToLower(table) + `.Save()
		if err != nil {
			//Save failed
		}
	}
}

func Example` + tableNaming + `Obj_Delete() {
	` + strings.ToLower(table) + `, err := ` + tableNaming + `.ReadById(` + exampleIdStr + `)
	if err == nil {
		_, err = ` + strings.ToLower(table) + `.Delete()
		if err != nil {
			//Delete failed
		}
	}
}

func ExampleReadAll() {
	` + strings.ToLower(table) + `s, err := ` + tableNaming + `.ReadAll("` + exampleOrderStr + `")
	if err == nil {
		for i := range ` + strings.ToLower(table) + `s {
			` + strings.ToLower(table) + ` := ` + strings.ToLower(table) + `s[i]
			fmt.Println(` + strings.ToLower(table) + `)
		}
	}
}

func ExampleReadById() {
	` + strings.ToLower(table) + `, err := ` + tableNaming + `.ReadById(` + exampleIdStr + `)
	if err == nil {
		//handle ` + strings.ToLower(table) + ` object
		fmt.Println(` + strings.ToLower(table) + `)
	}
}

func ExampleReadByQuery() {
	` + strings.ToLower(table) + `s, err := ` + tableNaming + `.ReadByQuery("SELECT * FROM ` + table + ` WHERE ` + exampleColumn + ` = ? ORDER BY ` + exampleOrderStr + `", "some string")
	if err == nil {
		for i := range ` + strings.ToLower(table) + `s {
			` + strings.ToLower(table) + ` := ` + strings.ToLower(table) + `s[i]
			fmt.Println(` + strings.ToLower(table) + `)
		}
	}
}

func ExampleReadOneByQuery() {
	` + strings.ToLower(table) + `, err := ` + tableNaming + `.ReadOneByQuery("SELECT * FROM ` + table + ` WHERE ` + exampleColumn + ` = ? ORDER BY ` + exampleOrderStr + `", "some string")
	if err == nil {
		//handle ` + strings.ToLower(table) + ` object
		fmt.Println(` + strings.ToLower(table) + `)
	}
}

func ExampleExec() {
	_, err := ` + tableNaming + `.Exec("UPDATE ` + table + ` SET ` + exampleColumn + ` = ? WHERE id = ?", "some string", ` + exampleIdStr + `)
	if err != nil {
		//Exec failed
	}
}`
		err = writeFile(examplesFilePath, contents, false)
		if err != nil {
			return err
		}
	}

	_, err := runCommand("go fmt " + examplesFilePath)
	if err != nil {
		return err
	}

	return nil
}

//Builds utils file
func (gs *Gostruct) buildUtilsPackage() error {
	filePath := GOPATH + "/src/utils/utils.go"
	if !exists(GOPATH + "/src/utils") {
		err = gs.CreateDirectory(GOPATH + "/src/utils")
		if err != nil {
			return err
		}
	}

	if !exists(filePath) {
		contents := `package utils

import (
	"database/sql"
	"date"
	"errors"
	"reflect"
	"strings"
	"time"
)

func NewNullString(s string) sql.NullString {
	if Empty(s) {
		return sql.NullString{}
	}
	return sql.NullString{
		String: s,
		Valid:  true,
	}
}

func NewNullInt(i int64) sql.NullInt64 {
	if Empty(i) {
		return sql.NullInt64{}
	}
	return sql.NullInt64{
		Int64: i,
		Valid: true,
	}
}

func NewNullFloat(f float64) sql.NullFloat64 {
	if Empty(f) {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{
		Float64: f,
		Valid:   true,
	}
}

func NewNullBool(b bool) sql.NullBool {
	if Empty(b) {
		return sql.NullBool{}
	}
	return sql.NullBool{
		Bool:  b,
		Valid: true,
	}
}

func NewNullTime(t time.Time) date.NullTime {
	if Empty(t) {
		return date.NullTime{}
	}
	return date.NullTime{
		Time:  t,
		Valid: true,
	}
}

//Determine whether or not an object is empty
func Empty(val interface{}) bool {
	empty := true
	switch val.(type) {
	case string, int, int64, float64, bool, time.Time:
		empty = isEmpty(val)
	default:
		v := reflect.ValueOf(val).Elem()
		if v.String() == "<invalid Value>" {
			return true
		}
		for i := 0; i < v.NumField(); i++ {
			var value interface{}
			field := reflect.Value(v.Field(i))

			switch field.Interface().(type) {
			case string:
				value = field.String()
			case int, int64:
				value = field.Int()
			case float64:
				value = field.Float()
			case bool:
				value = field.Bool()
			case time.Time:
				value = field.Interface()
			case sql.NullString:
				value = field.Field(0).String()
			case sql.NullInt64:
				value = field.Field(0).Int()
			case sql.NullFloat64:
				value = field.Field(0).Float()
			case sql.NullBool:
				value = field.Field(0).Bool()
			case date.NullTime:
				value = field.Field(0).Interface()
			default:
				value = field.Interface()
			}

			if !isEmpty(value) {
				empty = false
				break
			}
		}
	}
	return empty
}

func isEmpty(val interface{}) bool {
	empty := false
	switch v := val.(type) {
	case string:
		if v == "" {
			empty = true
		}
	case int:
		if v == 0 {
			empty = true
		}
	case int64:
		if int(int64(v)) == 0 {
			empty = true
		}
	case float64:
		if int(float64(v)) == 0 {
			empty = true
		}
	case bool:
		if v == false {
			empty = true
		}
	case time.Time:
		if v.String() == "0001-01-01 00:00:00 +0000 UTC" {
			empty = true
		}
	}
	return empty
}

//Validate field value
func ValidateField(val reflect.Value, field reflect.StructField) (interface{}, error) {
	if strings.Contains(string(field.Tag.Get("type")), "enum") {
		var s string
		switch t := val.Interface().(type) {
		case string:
			s = t
		case sql.NullString:
			s = t.String
		}
		vals := Between(string(field.Tag.Get("type")), "enum('", "')")
		arr := strings.Split(vals, "','")
		if !InArray(s, arr) {
			return nil, errors.New("Invalid value: '" + s + "' for column: " + string(field.Tag.Get("column")) + ". Possible values are: " + strings.Join(arr, ", "))
		}
	}

	return GetFieldValue(val, field.Tag.Get("default")), nil
}

//Returns string between two specified characters/strings
func Between(initial string, beginning string, end string) string {
	return strings.TrimLeft(strings.TrimRight(initial, end), beginning)
}

//Determine whether or not a string is in array
func InArray(char string, strings []string) bool {
	for _, a := range strings {
		if a == char {
			return true
		}
	}
	return false
}

//Returns the value from the struct field value as an interface
func GetFieldValue(field reflect.Value, defaultVal string) interface{} {
	var val interface{}

	switch t := field.Interface().(type) {
	case string:
		if !Empty(t) {
			val = t
		} else {
			val = defaultVal
		}
	case int:
		if !Empty(t) {
			val = t
		} else {
			val = defaultVal
		}
	case int64:
		if !Empty(t) {
			val = t
		} else {
			val = defaultVal
		}
	case float64:
		if !Empty(t) {
			val = t
		} else {
			val = defaultVal
		}
	case bool:
		if !Empty(t) {
			val = t
		} else {
			val = defaultVal
		}
	case time.Time:
		if !Empty(t) {
			val = t
		} else {
			val = defaultVal
		}
	case sql.NullString:
		val = NewNullString(t.String)
	case sql.NullInt64:
		val = NewNullInt(t.Int64)
	case sql.NullFloat64:
		val = NewNullFloat(t.Float64)
	case sql.NullBool:
		val = NewNullBool(t.Bool)
	case date.NullTime:
		val = NewNullTime(t.Time)
	}

	return val
}
`

		err = writeFile(filePath, contents, false)
		if err != nil {
			return err
		}
	}

	_, err := runCommand("go fmt " + filePath)
	if err != nil {
		return err
	}

	return nil
}

//Builds main connection package for serving up all database connections
//with a shared connection pool
func (gs *Gostruct) buildConnectionPackage() error {
	if !exists(GOPATH + "/src/connection") {
		err = gs.CreateDirectory(GOPATH + "/src/connection")
		if err != nil {
			return err
		}
	}

	conFilePath := GOPATH + "/src/connection/connection.go"
	contents := `package connection
import (
	"logger"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
)

var connection *sql.DB
var err error

func Get() *sql.DB {
	if connection != nil {
		//determine whether connection is still alive
		err = connection.Ping()
		if err == nil {
			return connection
		}
	}

	connection, err = sql.Open("mysql", "` + gs.Username + `:` + gs.Password + `@tcp(` + gs.Host + `:3306)/` + gs.Database + `?parseTime=true")
	if err != nil {
		logger.HandleError(err)
	}

	return connection
}`
	err = writeFile(conFilePath, contents, false)
	if err != nil {
		return err
	}

	_, err := runCommand("go fmt " + conFilePath)
	if err != nil {
		return err
	}

	return nil
}

func (gs *Gostruct) buildDatePackage() error {
	if !exists(GOPATH + "/src/date") {
		err = gs.CreateDirectory(GOPATH + "/src/date")
		if err != nil {
			return err
		}
	}

	dateFilePath := GOPATH + "/src/date/date.go"
	contents := `package date
import (
	"time"
	"database/sql/driver"
)

const DEFAULT_FORMAT = "2006-01-02 15:04:05"

// In place of a sql.NullTime struct
type NullTime struct {
	Time  time.Time
	Valid bool // Valid is true if Time is not NULL
}

// Scan implements the Scanner interface.
func (nt *NullTime) Scan(value interface{}) error {
	nt.Time, nt.Valid = value.(time.Time)
	return nil
}

// Value implements the driver Valuer interface.
func (nt NullTime) Value() (driver.Value, error) {
	if !nt.Valid {
		return nil, nil
	}
	return nt.Time, nil
}`
	err = writeFile(dateFilePath, contents, false)
	if err != nil {
		return err
	}

	_, err := runCommand("go fmt " + dateFilePath)
	if err != nil {
		return err
	}

	return nil
}