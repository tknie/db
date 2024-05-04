/*
* Copyright 2022-2024 Thorsten A. Knieling
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*    http://www.apache.org/licenses/LICENSE-2.0
*
 */

package common

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/tknie/errorrepo"
	"github.com/tknie/log"
	"gopkg.in/yaml.v3"
)

type SetType byte

// TagName name to be used for tagging structure field
const TagName = "flynn"
const SubTypeTag = "sub"

const (
	EmptySet SetType = iota
	AllSet
	GivenSet
)

type void struct{}

var member void

type typeInterface struct {
	DataType   interface{}
	RowNames   map[string][]string
	RowFields  []string
	SetType    SetType
	FieldSet   map[string]void
	ValueRefTo []any
	ScanValues []any
}

type SubInterface interface {
	Data() []byte
	ParseData(sub []byte) error
}

func CreateInterface(i interface{}, createFields []string) *typeInterface {
	fields := createFields
	if fields == nil {
		fields = []string{"*"}
	}
	ri := reflect.TypeOf(i)
	if ri.Kind() == reflect.Ptr {
		ri = ri.Elem()
	}
	log.Log.Debugf("Create dynamic interface with fields %#v", fields)
	set := make(map[string]void) // New empty set
	dynamic := &typeInterface{DataType: i, RowNames: make(map[string][]string),
		RowFields: make([]string, 0), FieldSet: set}
	for _, f := range fields {
		switch f {
		case "*":
			dynamic.SetType = AllSet
		case "":
			dynamic.SetType = EmptySet
			return dynamic
		default:
			dynamic.SetType = GivenSet
			dynamic.FieldSet[strings.ToLower(f)] = member
		}
	}
	log.Log.Debugf("FieldSet defined: %#v", dynamic.FieldSet)
	dynamic.generateFieldNames(ri)
	log.Log.Debugf("Final created field list generated %#v", dynamic.RowFields)
	return dynamic
}

func (dynamic *typeInterface) CreateQueryFields() string {
	if dynamic.SetType == EmptySet {
		return ""
	}
	var buffer bytes.Buffer
	for _, fieldName := range dynamic.RowFields {
		if buffer.Len() > 0 {
			buffer.WriteRune(',')
		}
		buffer.WriteString(fieldName)
	}
	return buffer.String()
}

// CreateQueryValues create query value copy of struct
func (dynamic *typeInterface) CreateQueryValues() (*ValueDefinition, error) {
	if dynamic.SetType == EmptySet {
		log.Log.Debugf("Empty set defined")
		return nil, nil
	}
	log.Log.Debugf("Create query values")
	value := reflect.ValueOf(dynamic.DataType)
	if value.Type().Kind() == reflect.Pointer {
		value = value.Elem()
	}
	copyValue := reflect.New(value.Type())
	if log.IsDebugLevel() {
		log.Log.Debugf("Value %s %T", value.Type().Name(), value.Interface())
		log.Log.Debugf("Main1: %T", copyValue.Interface())
	}
	elemValue := copyValue
	rt := elemValue.Type()
	if rt.Kind() == reflect.Pointer {
		elemValue = elemValue.Elem()
		log.Log.Debugf("Pointer type: %T", elemValue.Interface())
	}
	log.Log.Debugf("Final type: %T", elemValue.Interface())
	err := dynamic.generateField(elemValue, true)
	if err != nil {
		return nil, err
	}
	vd := &ValueDefinition{dynamic, copyValue.Interface(), dynamic.ValueRefTo, dynamic.ScanValues}
	return vd, nil
}

// CreateValues create query value copy of struct
// deprecated: should not be used anymore
func (dynamic *typeInterface) CreateValues(value interface{}) ([]any, error) {
	dynamic.ValueRefTo = make([]any, 0)
	if dynamic.SetType == EmptySet {
		log.Log.Debugf("Empty set defined")
		return nil, nil
	}
	log.Log.Debugf("Create insert values")
	valueOf := reflect.ValueOf(value)
	if valueOf.Type().Kind() == reflect.Pointer {
		valueOf = valueOf.Elem()
	}
	err := dynamic.generateField(valueOf, false)
	if err != nil {
		return nil, err
	}
	log.Log.Debugf("Create insert values done")
	return dynamic.ValueRefTo, nil
}

// generateField generate field values for dynamic query.
// 'scan' is used to consider case for read (field creation out of database) or
// write (no creation, data is used by application)
func (dynamic *typeInterface) generateField(elemValue reflect.Value, scan bool) error {
	log.Log.Debugf("Generate field of Struct: %T %s -> scan=%v",
		elemValue.Interface(), elemValue.Type().Name(), scan)
	defer log.Log.Debugf("generated field of struct")
	for fi := 0; fi < elemValue.NumField(); fi++ {
		fieldType := elemValue.Type().Field(fi)
		tag := fieldType.Tag
		cv := elemValue.Field(fi)
		d := tag.Get(TagName)
		tags := strings.Split(d, ":")
		fieldName := fieldType.Name
		log.Log.Debugf("%s: kind %v tags = %#v", fieldName, cv.Kind(), tags)
		if len(tags) > 1 {
			log.Log.Debugf("Tag for %s = %s", fieldType.Name, tag)
			if tags[1] == "ignore" {
				continue
			}
			log.Log.Debugf("Tags[1]=%v / %s", tags[1], cv.Type().Name())
			if tags[1] == SubTypeTag {
				log.Log.Debugf("is nil = %v scan = %v", cv.IsNil(), scan)
				checkField := dynamic.checkFieldSet(fieldType.Name)
				if checkField {
					di := cv.Interface()
					log.Log.Debugf("Sub interface = %v/%T", di, di)
					if cd, ok := di.(SubInterface); ok {
						if scan {
							x := reflect.Indirect(reflect.New(cv.Type().Elem()))
							log.Log.Debugf("X = %T", x.Interface())
							cv.Set(x.Addr())
							di = cv.Interface()
							log.Log.Debugf("V = %v/%T", di, di)
							dynamic.ValueRefTo = append(dynamic.ValueRefTo, di)
						} else {
							data := cd.Data()
							dynamic.ValueRefTo = append(dynamic.ValueRefTo, data)
						}
						dynamic.ScanValues = append(dynamic.ScanValues, &sql.NullString{})
						continue
					}
					log.Log.Debugf("No sub interface = %v/%T", di, di)
					return errorrepo.NewError("DB000011", fieldType.Name)
				} else {
					continue
				}
			}
		}
		if len(tags) > 0 {
			if tags[0] != "" {
				fieldName = tags[0]
			}
		}
		if cv.Kind() == reflect.Pointer {
			if !scan && cv.IsNil() {
				log.Log.Debugf("IsNil pointer = %v -> %s", cv.IsNil(), cv.Type().String())
				if len(tags) > 1 {
					switch tags[1] {
					case "YAML", "XML", "JSON":
						dynamic.ValueRefTo = append(dynamic.ValueRefTo, "")
						continue
					}
				}

				// x := reflect.New(cv.Type().Elem())
				x := reflect.Indirect(reflect.New(cv.Type().Elem()))

				err := dynamic.generateField(x, scan)
				if err != nil {
					return err
				}
				// dynamic.ValueRefTo = append(dynamic.ValueRefTo, nil)
				continue
			}
			if scan {
				x := reflect.New(cv.Type().Elem())
				log.Log.Debugf("Work on pointer %v %s", x, cv.Type().String())
				cv.Set(x)
				cv = x.Elem()
			} else {
				cv = cv.Elem()
				log.Log.Debugf("Go on pointer %s: kind %v", fieldName, cv.Kind())
			}
		}
		if cv.Kind() == reflect.Struct {
			log.Log.Debugf("Work on struct %s", fieldType.Name)
			switch cv.Interface().(type) {
			case time.Time:
				checkField := dynamic.checkFieldSet(fieldType.Name)
				if checkField {
					ptr := cv.Addr()
					t := reflect.TypeOf(cv)
					log.Log.Debugf("Add Time %T %s %s", ptr.Interface(), cv.Type().Name(), t.Name())
					dynamic.ValueRefTo = append(dynamic.ValueRefTo, ptr.Interface())
					dynamic.ScanValues = append(dynamic.ScanValues, &sql.NullTime{})
				}
				continue
			default:
				if len(tags) > 1 {
					log.Log.Debugf("Tags[1]=%v / %s", tags[1], cv.Type().Name())
					if tags[1] == SubTypeTag {
						di := cv.Interface()
						log.Log.Debugf("Check sub interface = %v/%T", di, di)
						if cd, ok := di.(SubInterface); ok {
							data := cd.Data()
							dynamic.ValueRefTo = append(dynamic.ValueRefTo, data)
							dynamic.ScanValues = append(dynamic.ScanValues, &sql.NullString{})
							continue
						}
						log.Log.Debugf("No sub interface = %v/%T", di, di)
						return errorrepo.NewError("DB000011", fieldType.Name)
					}
				}
				if len(tags) > 1 {
					switch tags[1] {
					case "YAML":
						out, err := yaml.Marshal(cv.Interface())
						if err != nil {
							return err
						}
						dynamic.ValueRefTo = append(dynamic.ValueRefTo, string(out))
						continue
					case "XML":
						out, err := xml.Marshal(cv.Interface())
						if err != nil {
							return err
						}
						dynamic.ValueRefTo = append(dynamic.ValueRefTo, string(out))
						continue
					case "JSON":
						out, err := json.Marshal(cv.Interface())
						if err != nil {
							return err
						}
						dynamic.ValueRefTo = append(dynamic.ValueRefTo, string(out))
						continue
					default:
						dynamic.ValueRefTo = append(dynamic.ValueRefTo, "")
						continue
					}
				}
				dynamic.generateField(cv, scan)
			}
		} else {
			log.Log.Debugf("Work on field %s -> scan=%v", fieldName, scan)
			checkField := dynamic.checkFieldSet(fieldName)
			if checkField {
				if scan {
					var ptr reflect.Value
					if cv.CanAddr() {
						log.Log.Debugf("Use Addr")
						ptr = cv.Addr()
					} else {
						ptr = reflect.New(cv.Type())
						log.Log.Debugf("Got Addr pointer %#v", ptr)
						ptr.Elem().Set(cv)
					}
					ptrInt := ptr.Interface()
					log.Log.Debugf("Add value %T pointer=%p %s %s", ptrInt, ptrInt, fieldName, elemValue.Type().Name())
					dynamic.ValueRefTo = append(dynamic.ValueRefTo, ptrInt)
					switch cv.Kind() {
					case reflect.String:
						dynamic.ScanValues = append(dynamic.ScanValues, &sql.NullString{})
					case reflect.Bool:
						dynamic.ScanValues = append(dynamic.ScanValues, &sql.NullBool{})
					case reflect.Int8:
						dynamic.ScanValues = append(dynamic.ScanValues, &sql.NullByte{})
					case reflect.Int16:
						dynamic.ScanValues = append(dynamic.ScanValues, &sql.NullInt16{})
					case reflect.Int32, reflect.Int:
						dynamic.ScanValues = append(dynamic.ScanValues, &sql.NullInt32{})
					case reflect.Int64:
						dynamic.ScanValues = append(dynamic.ScanValues, &sql.NullInt64{})
					case reflect.Uint64:
						dynamic.ScanValues = append(dynamic.ScanValues, &sql.NullString{})
					case reflect.Float32, reflect.Float64:
						dynamic.ScanValues = append(dynamic.ScanValues, &sql.NullFloat64{})
					default:
						log.Log.Debugf("'%s' dynamic Kind not defined for SQL %s", fieldType.Name, cv.Kind().String())
						dynamic.ScanValues = append(dynamic.ScanValues, ptrInt)
					}
				} else {
					switch cv.Kind() {
					case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer,
						reflect.UnsafePointer, reflect.Interface, reflect.Slice:
						if cv.IsNil() {
							dynamic.ValueRefTo = append(dynamic.ValueRefTo, nil)
						} else {
							dynamic.ValueRefTo = append(dynamic.ValueRefTo, cv.Interface())
						}
					default:
						if cv.IsValid() {
							log.Log.Debugf("Add no-scan value type=%T field=%s elemValueName=%s: value=%#v",
								cv.Interface(), fieldName, elemValue.Type().Name(), cv.Interface())
							dynamic.ValueRefTo = append(dynamic.ValueRefTo, cv.Interface())
						} else {
							log.Log.Debugf("Invalid no-scan field %s", fieldName)
							dynamic.ValueRefTo = append(dynamic.ValueRefTo, nil)
						}
					}
				}
			} else {
				log.Log.Debugf("Skip field not in field set")
			}
		}
		log.Log.Debugf("Row values len=%d", len(dynamic.ValueRefTo))
	}
	return nil
}

func (dynamic *typeInterface) checkFieldSet(fieldName string) bool {
	ok := true
	log.Log.Debugf("Check %s in %#v", strings.ToLower(fieldName), dynamic.FieldSet)
	if dynamic.SetType == GivenSet {
		_, ok = dynamic.FieldSet[strings.ToLower(fieldName)]
	}
	log.Log.Debugf("Restrict to %v", ok)

	return ok
}

// generateFieldNames examine all structure-tags in the given structure and build up
// field names map pointing to corresponding path with names of structures
func (dynamic *typeInterface) generateFieldNames(ri reflect.Type) {
	if log.IsDebugLevel() {
		log.Log.Debugf("Generate field names...")
	}
	if ri.Kind() != reflect.Struct {
		return
	}
	for fi := 0; fi < ri.NumField(); fi++ {
		ct := ri.Field(fi)
		fieldName := ct.Name
		log.Log.Debugf("Work on fieldname %s", fieldName)
		tag := ct.Tag.Get(TagName)

		// If tag is given
		if tag != "" {
			log.Log.Debugf("Field tag %s", tag)
			s := strings.Split(tag, ":")
			if len(s) > 0 && s[0] != "" {
				fieldName = s[0]
			}
			if len(s) > 1 {
				log.Log.Debugf("Field tag option %s", s[1])
				switch s[1] {
				case "key":
					dynamic.RowNames["#key"] = []string{fieldName}
				case "isn":
					dynamic.RowNames["#index"] = []string{fieldName}
					continue
				case "ignore":
					continue
				case SubTypeTag:
					log.Log.Debugf("Found sub")
					ok := dynamic.checkFieldSet(fieldName)
					if ok {
						dynamic.RowFields = append(dynamic.RowFields, fieldName)
						log.Log.Debugf("RowFields: Add field name %s", fieldName)
					}
					continue
				default:
				}
			}
			if len(s) > 1 {
				log.Log.Debugf("Field tag option %s", s[1])
				switch s[1] {
				case "YAML", "XML", "JSON":
					dynamic.RowFields = append(dynamic.RowFields, fieldName)
					continue
				}
			}
		}
		log.Log.Debugf("Work on final fieldname %s", fieldName)
		log.Log.Debugf("Add field %s", ct.Name)
		st := ct.Type
		if st.Kind() == reflect.Pointer {
			log.Log.Debugf("Pointer-Kind of %s", st.Name())
			st = st.Elem()
			log.Log.Debugf("Pointer-Struct-Kind of %s -> %s", st.Name(), st.Kind())
		}
		if st.Kind() == reflect.Struct {
			log.Log.Debugf("Struct-Kind of %s", st.Name())
			//continue generate field names
			if st.Name() != "Time" {
				dynamic.generateFieldNames(st)
			} else {
				ok := dynamic.checkFieldSet(fieldName)
				if ok {
					dynamic.RowFields = append(dynamic.RowFields, fieldName)
					log.Log.Debugf("RowFields: Add field name %s", fieldName)
				}
			}
		} else {
			log.Log.Debugf("Kind of %s: %s", fieldName, ct.Type.Kind())
			// copy of subfields
			// copy(subFields, fields)
			ok := dynamic.checkFieldSet(fieldName)
			if ok {
				dynamic.RowFields = append(dynamic.RowFields, fieldName)
				log.Log.Debugf("RowFields: Add field name %s", fieldName)
			}
		}
		// Handle special case for pointer and slices
		switch ct.Type.Kind() {
		case reflect.Ptr:
			// dynamic.generateFieldNames(ct.Type.Elem())
		case reflect.Slice:
			sliceT := ct.Type.Elem()
			if sliceT.Kind() == reflect.Ptr {
				sliceT = sliceT.Elem()
			}
			dynamic.generateFieldNames(sliceT)
		}
	}
	log.Log.Debugf("Field list generated %#v", dynamic.RowFields)
}

func (vd *ValueDefinition) ShiftValues() error {
	for d, v := range vd.ScanValues {
		if di, ok := vd.Values[d].(SubInterface); ok {

			log.Log.Debugf("%d. entry is sub interface %v", d, vd.Values[d])
			ns := v.(*sql.NullString)
			if ns.Valid {
				if di == nil || vd.Values[d] == nil {
					return errorrepo.NewError("DB000032")
				}
				log.Log.Debugf("Found sub data: %s(%v)/%v", ns.String, di, v)
				return di.ParseData([]byte(ns.String))
			}
			return nil
		}
		if _, ok := v.(sqlInterface); ok {
			vv, err := v.(sqlInterface).Value()
			if err != nil {
				return err
			}
			if vv != nil {
				log.Log.Debugf("(%d) Found value %T pointer=%p", d, vd.Values[d], vd.Values[d])
				log.Log.Debugf("Shift values %v", vv)
				switch vt := vd.Values[d].(type) {
				case *int:
					switch vvv := vv.(type) {
					case int:
						*vt = int(vvv)
					case int32:
						*vt = int(vvv)
					case int64:
						*vt = int(vvv)
					default:
						log.Log.Debugf("Unknown type %T", vv)
					}
				case *float32:
					*vt = vv.(float32)
				case *float64:
					*vt = vv.(float64)
				case *int64:
					*vt = vv.(int64)
				case *uint64:
					v := vv.(string)
					*vt, err = strconv.ParseUint(v, 0, 64)
					if err != nil {
						return err
					}
				case *string:
					*vt = vv.(string)
				case *time.Time:
					*vt = vv.(time.Time)
				default:
					log.Log.Fatalf("Unknown type for shifting at index %d value %T <- %T", d, vd.Values[d], vv)
				}
			} else {
				log.Log.Debugf("SQL interface value nil")
			}
		}
	}
	return nil
}
