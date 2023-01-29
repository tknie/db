/*
* Copyright 2022-2023 Thorsten A. Knieling
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*    http://www.apache.org/licenses/LICENSE-2.0
*
 */

package mysql

import (
	"database/sql"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/tknie/errorrepo"
	"github.com/tknie/flynn/common"
	def "github.com/tknie/flynn/common"
	"github.com/tknie/flynn/dbsql"
	"github.com/tknie/log"
)

const layer = "mysql"

// Mysql instance for MySQL
type Mysql struct {
	def.CommonDatabase
	openDB       any
	dbURL        string
	dbTableNames []string
	user         string
	password     string
}

// New create new postgres reference instance
func New(id def.RegDbID, url string) (def.Database, error) {
	mysql := &Mysql{def.CommonDatabase{RegDbID: id},
		nil, url, nil, "", ""}
	return mysql, nil
}

// SetCredentials set credentials to connect to database
func (mysql *Mysql) SetCredentials(user, password string) error {
	mysql.user = user
	mysql.password = password
	return nil
}

func (mysql *Mysql) generateURL() string {
	url := mysql.dbURL
	if mysql.user != "" {
		url = strings.Replace(url, "<user>", mysql.user, -1)
	}
	if mysql.password != "" {
		url = strings.Replace(url, "<password>", mysql.password, -1)
	}
	return url
}

// Open open the database connection
func (mysql *Mysql) Open() (dbOpen any, err error) {
	var db *sql.DB
	if !mysql.IsTransaction() || mysql.openDB == nil {
		db, err = sql.Open(layer, mysql.generateURL()+"?parseTime=true")
		if err != nil {
			return
		}
		mysql.openDB = db
	} else {
		db = mysql.openDB.(*sql.DB)
	}
	return db, nil
}

// Close close the database connection
func (mysql *Mysql) Close() {
	if mysql.openDB != nil {
		mysql.openDB.(*sql.DB).Close()
		mysql.openDB = nil
	}
}

// IndexNeeded index needed for the SELECT statement value reference
func (mysql *Mysql) IndexNeeded() bool {
	return false
}

// ByteArrayAvailable byte array available in SQL database
func (mysql *Mysql) ByteArrayAvailable() bool {
	return false
}

// Reference reference to postgres URL
func (mysql *Mysql) Reference() (string, string) {
	return "mysql", mysql.dbURL
}

// ID current id used
func (mysql *Mysql) ID() def.RegDbID {
	return mysql.RegDbID
}

// URL current URL used
func (mysql *Mysql) URL() string {
	return mysql.dbURL
}

// Maps database maps, tables or views
func (mysql *Mysql) Maps() ([]string, error) {
	if mysql.dbTableNames == nil {
		err := mysql.Ping()
		if err != nil {
			return nil, err
		}
	}
	return mysql.dbTableNames, nil
}

// Ping create short test database connection
func (mysql *Mysql) Ping() error {
	dbOpen, err := mysql.Open()
	if err != nil {
		return err
	}
	defer mysql.Close()

	db := dbOpen.(*sql.DB)

	mysql.dbTableNames = make([]string, 0)

	rows, err := db.Query("SHOW TABLES")
	if err != nil {
		return err
	}
	tableName := ""
	for rows.Next() {
		err = rows.Scan(&tableName)
		if err != nil {
			return err
		}
		mysql.dbTableNames = append(mysql.dbTableNames, tableName)
	}

	return nil
}

// Delete Delete database records
func (mysql *Mysql) Delete(name string, remove *def.Entries) (int64, error) {
	return dbsql.Delete(mysql, name, remove)
}

// GetTableColumn get table columne names
func (mysql *Mysql) GetTableColumn(tableName string) ([]string, error) {
	return nil, errorrepo.NewError("DB065535")
}

// Query query database records with search or SELECT
func (mysql *Mysql) Query(search *def.Query, f def.ResultFunction) (*common.Result, error) {
	dbOpen, err := mysql.Open()
	if err != nil {
		return nil, err
	}
	defer mysql.Close()

	db := dbOpen.(*sql.DB)
	selectCmd := search.Select()

	log.Log.Debugf("Query: %s", selectCmd)
	rows, err := db.Query(selectCmd)
	if err != nil {
		return nil, err
	}
	if search.DataStruct == nil {
		return search.ParseRows(rows, f)
	}
	return search.ParseStruct(rows, f)
}

// CreateTable create a new table
func (mysql *Mysql) CreateTable(name string, columns any) error {
	return dbsql.CreateTable(mysql, name, columns)
}

// DeleteTable delete a table
func (mysql *Mysql) DeleteTable(name string) error {
	return dbsql.DeleteTable(mysql, name)
}

// Insert insert record into table
func (mysql *Mysql) Insert(name string, insert *def.Entries) error {
	return dbsql.Insert(mysql, name, insert)
}

// Update update record in table
func (mysql *Mysql) Update(name string, insert *def.Entries) error {
	return dbsql.Update(mysql, name, insert)
}

// BatchSQL batch SQL query in table
func (mysql *Mysql) BatchSQL(batch string) error {
	return dbsql.BatchSQL(mysql, batch)
}
